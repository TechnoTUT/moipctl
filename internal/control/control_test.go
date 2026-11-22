package control

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/TechnoTUT/moipctl/internal/registry"
)

func node(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

func TestCallForwardsBodyAndReturnsNodeResponse(t *testing.T) {
	var gotPath, gotBody string
	addr := node(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath, gotBody = r.Method+" "+r.URL.Path, string(b)
		w.Write([]byte(`{"running":true}`))
	})
	reg := registry.New()
	reg.Seen("a", "utone-a", addr)

	res := New(reg).Call(context.Background(), "a", "rx", "switch", []byte(`{"sender_name":"CAM1"}`))
	if !res.OK || res.Code != 200 || string(res.Response) != `{"running":true}` {
		t.Fatalf("got %+v", res)
	}
	if gotPath != "POST /api/rx/switch" || gotBody != `{"sender_name":"CAM1"}` {
		t.Fatalf("node got %q %q", gotPath, gotBody)
	}
}

func TestCallRelaysNodeErrorDetail(t *testing.T) {
	addr := node(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"detail":"sender_name is required"}`))
	})
	reg := registry.New()
	reg.Seen("a", "utone-a", addr)

	res := New(reg).Call(context.Background(), "a", "rx", "switch", nil)
	if res.OK || res.Code != 400 || res.Error != "sender_name is required" {
		t.Fatalf("got %+v", res)
	}
}

func TestCallRejectsBeforeReachingTheNode(t *testing.T) {
	reg := registry.New()
	reg.Seen("up", "h", "127.0.0.1:1")
	reg.Seen("down", "h", "127.0.0.1:1")
	reg.Gone("down")
	c := New(reg)
	ctx := context.Background()

	for _, tc := range []struct {
		id, role, action string
		code             int
	}{
		{"nope", "rx", "stop", 404},
		{"down", "rx", "stop", 409},
		{"up", "rx", "reboot", 404}, // not on the allowlist
		{"up", "settings", "stop", 404},
		{"up", "rx", "stop", 502}, // reachable in the registry but nothing listens
	} {
		if res := c.Call(ctx, tc.id, tc.role, tc.action, nil); res.OK || res.Code != tc.code {
			t.Errorf("%s %s/%s: got %+v, want code %d", tc.id, tc.role, tc.action, res, tc.code)
		}
	}
}

func TestCallManyKeepsOrderAndIsolatesFailures(t *testing.T) {
	ok := node(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	reg := registry.New()
	reg.Seen("a", "a", ok)
	reg.Seen("c", "c", ok)

	res := New(reg).CallMany(context.Background(), []string{"a", "missing", "c"}, "rx", "stop", nil)
	if len(res) != 3 || res[0].ID != "a" || !res[0].OK || res[1].ID != "missing" || res[1].OK || res[2].ID != "c" || !res[2].OK {
		t.Fatalf("got %+v", res)
	}
}
