package scout

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TechnoTUT/moipctl/internal/discovery"
	"github.com/TechnoTUT/moipctl/internal/registry"
)

var nodeA = discovery.Entry{ID: "utone-a", Hostname: "utone-a", Addr: "10.10.20.3:8000"}

func TestValidate(t *testing.T) {
	ok := Report{Source: "av", Nodes: []discovery.Entry{nodeA}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Report{Source: "av"}).Validate(); err != nil {
		t.Fatalf("an empty report is a heartbeat and must be valid: %v", err)
	}
	bad := map[string]Report{
		"no source":       {Nodes: []discovery.Entry{nodeA}},
		"long source":     {Source: strings.Repeat("x", 65)},
		"no id":           {Source: "av", Nodes: []discovery.Entry{{Addr: "10.0.0.1:8000"}}},
		"no port":         {Source: "av", Nodes: []discovery.Entry{{ID: "a", Addr: "10.0.0.1"}}},
		"port zero":       {Source: "av", Nodes: []discovery.Entry{{ID: "a", Addr: "10.0.0.1:0"}}},
		"hostname not ip": {Source: "av", Nodes: []discovery.Entry{{ID: "a", Addr: "example.com:8000"}}},
		"ipv6":            {Source: "av", Nodes: []discovery.Entry{{ID: "a", Addr: "[fd00::1]:8000"}}},
		"loopback":        {Source: "av", Nodes: []discovery.Entry{{ID: "a", Addr: "127.0.0.1:8000"}}},
		"link local":      {Source: "av", Nodes: []discovery.Entry{{ID: "a", Addr: "169.254.1.1:8000"}}},
		"multicast":       {Source: "av", Nodes: []discovery.Entry{{ID: "a", Addr: "224.0.0.251:8000"}}},
		"unspecified":     {Source: "av", Nodes: []discovery.Entry{{ID: "a", Addr: "0.0.0.0:8000"}}},
	}
	for name, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newHub(t *testing.T) (*Hub, *registry.Registry, *clock) {
	t.Helper()
	reg := registry.New()
	h := NewHub(reg, 45*time.Second)
	c := &clock{t: time.Unix(1_000_000, 0)}
	h.now = c.now
	return h, reg, c
}

func TestHubExpiresNodesThatAreNoLongerReported(t *testing.T) {
	h, reg, c := newHub(t)
	reg.Seen("static", "static", "10.0.0.9:8000") // never reported by a scout

	h.Apply(Report{Source: "av", Nodes: []discovery.Entry{nodeA}})
	if n, _ := reg.Get("utone-a"); !n.Online || n.Source != "av" || n.Addr != "10.10.20.3:8000" {
		t.Fatalf("got %+v", n)
	}

	c.t = c.t.Add(30 * time.Second)
	h.Sweep()
	if n, _ := reg.Get("utone-a"); !n.Online {
		t.Fatal("within the TTL the node stays online")
	}
	h.Apply(Report{Source: "av", Nodes: []discovery.Entry{nodeA}}) // reported again
	c.t = c.t.Add(30 * time.Second)
	h.Sweep()
	if n, _ := reg.Get("utone-a"); !n.Online {
		t.Fatal("a fresh report restarts the TTL")
	}

	c.t = c.t.Add(46 * time.Second)
	h.Sweep()
	if n, _ := reg.Get("utone-a"); n.Online {
		t.Fatal("not reported within the TTL: offline")
	}
	if n, _ := reg.Get("static"); !n.Online {
		t.Fatal("a node no scout reported must never expire")
	}
	h.Apply(Report{Source: "av", Nodes: []discovery.Entry{nodeA}})
	if n, _ := reg.Get("utone-a"); !n.Online {
		t.Fatal("reported again: back online")
	}
}

func TestHubFollowsANodeThatMovesToAnotherVLAN(t *testing.T) {
	h, reg, _ := newHub(t)
	h.Apply(Report{Source: "av", Nodes: []discovery.Entry{nodeA}})
	moved := nodeA
	moved.Addr = "192.168.12.40:8000"
	h.Apply(Report{Source: "ent", Nodes: []discovery.Entry{moved}})
	if n, _ := reg.Get("utone-a"); n.Source != "ent" || n.Addr != "192.168.12.40:8000" {
		t.Fatalf("got %+v", n)
	}
}

func post(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, ReportPath, strings.NewReader(body)))
	return rec
}

func TestHandlerAcceptsReportsAndRejectsBadOnes(t *testing.T) {
	h, reg, _ := newHub(t)
	hh := h.Handler()

	if rec := post(t, hh, `{"source":"av","nodes":[{"id":"utone-a","hostname":"utone-a","addr":"10.10.20.3:8000"}]}`); rec.Code != 204 {
		t.Fatalf("status=%d %s", rec.Code, rec.Body)
	}
	if n, ok := reg.Get("utone-a"); !ok || !n.Online {
		t.Fatalf("got %+v", n)
	}
	for _, body := range []string{
		`not json`,
		`{"source":"","nodes":[]}`,
		`{"source":"av","nodes":[{"id":"x","addr":"127.0.0.1:8000"}]}`, // would make the controller call itself
		`{"source":"av","nodes":[{"id":"x","addr":"http://evil/"}]}`,
	} {
		rec := post(t, hh, body)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "detail") {
			t.Errorf("%q: status=%d body=%s", body, rec.Code, rec.Body)
		}
	}
	if _, ok := reg.Get("x"); ok {
		t.Fatal("a rejected report must not touch the registry")
	}
	// GET is not allowed on the report path
	rec := httptest.NewRecorder()
	hh.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ReportPath, bytes.NewReader(nil)))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: status=%d", rec.Code)
	}
}

type fakeBrowser struct {
	entries []discovery.Entry
	err     error
	calls   atomic.Int32
}

func (f *fakeBrowser) Browse(context.Context, time.Duration) ([]discovery.Entry, error) {
	f.calls.Add(1)
	return f.entries, f.err
}

func TestAgentReportsToTheHub(t *testing.T) {
	h, reg, c := newHub(t)
	srv := httptest.NewServer(h.Handler())
	defer srv.Close()
	fb := &fakeBrowser{entries: []discovery.Entry{nodeA}}
	a := &Agent{Browser: fb, Controller: srv.URL, Source: "av"}
	ctx := context.Background()

	if err := a.Round(ctx); err != nil {
		t.Fatal(err)
	}
	if n, _ := reg.Get("utone-a"); !n.Online || n.Source != "av" {
		t.Fatalf("got %+v", n)
	}

	// The node disappears from the VLAN: the scout keeps reporting, now empty,
	// and the controller expires the node by TTL.
	fb.entries = nil
	if err := a.Round(ctx); err != nil {
		t.Fatalf("an empty round is a heartbeat, not an error: %v", err)
	}
	c.t = c.t.Add(46 * time.Second)
	h.Sweep()
	if n, _ := reg.Get("utone-a"); n.Online {
		t.Fatal("node should be offline")
	}
}

func TestAgentSendsNothingWhenBrowsingFailsAndReportsControllerErrors(t *testing.T) {
	var got atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Add(1)
		http.Error(w, `{"detail":"boom"}`, http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := &Agent{Browser: &fakeBrowser{err: errors.New("no multicast interface")}, Controller: srv.URL, Source: "av"}
	if err := a.Round(context.Background()); err == nil || got.Load() != 0 {
		t.Fatalf("a failed browse must not report (err=%v, requests=%d)", err, got.Load())
	}

	a = &Agent{Browser: &fakeBrowser{}, Controller: srv.URL, Source: "av"}
	err := a.Round(context.Background())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("the controller's reason should be in the error, got %v", err)
	}
}
