package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/TechnoTUT/moipctl/internal/control"
	"github.com/TechnoTUT/moipctl/internal/registry"
)

func newTestServer(t *testing.T) (*registry.Registry, *httptest.Server) {
	t.Helper()
	reg := registry.New()
	srv := httptest.NewServer((&Server{Reg: reg, Ctl: control.New(reg)}).Handler())
	t.Cleanup(srv.Close)
	return reg, srv
}

func TestNodesAndNode(t *testing.T) {
	reg, srv := newTestServer(t)
	reg.Seen("a", "utone-a", "10.0.0.1:8000")

	resp, err := http.Get(srv.URL + "/api/nodes")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var nodes []registry.Node
	if err := json.NewDecoder(resp.Body).Decode(&nodes); err != nil || len(nodes) != 1 || nodes[0].ID != "a" {
		t.Fatalf("got %+v err=%v", nodes, err)
	}

	resp2, err := http.Get(srv.URL + "/api/nodes/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var e map[string]string
	json.NewDecoder(resp2.Body).Decode(&e)
	if resp2.StatusCode != http.StatusNotFound || e["detail"] == "" {
		t.Fatalf("status=%d body=%v", resp2.StatusCode, e)
	}
}

func TestEventsSendsListThenChanges(t *testing.T) {
	reg, srv := newTestServer(t)
	resp, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)

	next := func() []registry.Node {
		t.Helper()
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var nodes []registry.Node
				if err := json.Unmarshal([]byte(data), &nodes); err != nil {
					t.Fatal(err)
				}
				return nodes
			}
		}
		t.Fatal("stream ended")
		return nil
	}
	if got := next(); len(got) != 0 {
		t.Fatalf("initial list should be empty, got %+v", got)
	}
	reg.Seen("a", "utone-a", "10.0.0.1:8000")
	if got := next(); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("got %+v", got)
	}
}

// calls records the requests a fake node received. Bulk operations call nodes
// in parallel, so it is locked.
type calls struct {
	mu   sync.Mutex
	list []string
}

func (c *calls) add(s string) { c.mu.Lock(); c.list = append(c.list, s); c.mu.Unlock() }
func (c *calls) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.list...)
}

func fakeNode(t *testing.T, got *calls) string {
	t.Helper()
	n := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got.add(r.URL.Path + " " + string(b))
		w.Write([]byte(`{"running":true}`))
	}))
	t.Cleanup(n.Close)
	return n.Listener.Addr().String()
}

func post(t *testing.T, url, body string) (*http.Response, []byte) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestCallForwardsToNode(t *testing.T) {
	var got calls
	reg, srv := newTestServer(t)
	reg.Seen("a", "utone-a", fakeNode(t, &got))

	resp, body := post(t, srv.URL+"/api/nodes/a/rx/switch", `{"sender_name":"CAM1"}`)
	if resp.StatusCode != 200 || string(body) != `{"running":true}` {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if g := got.all(); len(g) != 1 || g[0] != `/api/rx/switch {"sender_name":"CAM1"}` {
		t.Fatalf("node got %v", g)
	}

	if resp, _ := post(t, srv.URL+"/api/nodes/nope/rx/stop", ``); resp.StatusCode != 404 {
		t.Fatalf("unknown node: status=%d", resp.StatusCode)
	}
	if resp, _ := post(t, srv.URL+"/api/nodes/a/rx/reboot", ``); resp.StatusCode != 404 {
		t.Fatalf("unsupported op: status=%d", resp.StatusCode)
	}
}

func TestBulkReportsEachNode(t *testing.T) {
	var got calls
	reg, srv := newTestServer(t)
	addr := fakeNode(t, &got)
	reg.Seen("a", "utone-a", addr)
	reg.Seen("b", "utone-b", addr)

	resp, body := post(t, srv.URL+"/api/bulk/rx/switch", `{"nodes":["a","b","a","ghost"],"body":{"sender_name":"CAM1"}}`)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var out struct {
		Results []struct {
			ID   string `json:"id"`
			OK   bool   `json:"ok"`
			Code int    `json:"code"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 3 { // "a" listed twice runs once
		t.Fatalf("got %+v", out.Results)
	}
	if r := out.Results; !r[0].OK || !r[1].OK || r[2].ID != "ghost" || r[2].OK || r[2].Code != 404 {
		t.Fatalf("got %+v", r)
	}
	if g := got.all(); len(g) != 2 {
		t.Fatalf("nodes should have been called once each, got %v", g)
	}

	for _, body := range []string{`{"nodes":[]}`, `{`} {
		if resp, _ := post(t, srv.URL+"/api/bulk/rx/switch", body); resp.StatusCode != 400 {
			t.Fatalf("%q: status=%d", body, resp.StatusCode)
		}
	}
	if resp, _ := post(t, srv.URL+"/api/bulk/rx/reboot", `{"nodes":["a"]}`); resp.StatusCode != 404 {
		t.Fatalf("unsupported op: status=%d", resp.StatusCode)
	}
}

func put(t *testing.T, url, body string) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestSetAliasPersistsAndValidates(t *testing.T) {
	reg := registry.New()
	var saved map[string]string
	srv := httptest.NewServer((&Server{Reg: reg, Ctl: control.New(reg), Persist: func(a map[string]string) error {
		saved = a
		return nil
	}}).Handler())
	defer srv.Close()
	reg.Seen("a", "utone-a", "x:1")

	resp, body := put(t, srv.URL+"/api/nodes/a/alias", `{"alias":"  entrance  "}`)
	var n registry.Node
	json.Unmarshal(body, &n)
	if resp.StatusCode != 200 || n.Alias != "entrance" || saved["a"] != "entrance" {
		t.Fatalf("status=%d node=%+v saved=%v", resp.StatusCode, n, saved)
	}
	if resp, _ := put(t, srv.URL+"/api/nodes/nope/alias", `{"alias":"x"}`); resp.StatusCode != 404 {
		t.Fatalf("unknown node: status=%d", resp.StatusCode)
	}
	if resp, _ := put(t, srv.URL+"/api/nodes/a/alias", `{"alias":"`+strings.Repeat("あ", 65)+`"}`); resp.StatusCode != 400 {
		t.Fatalf("too long: status=%d", resp.StatusCode)
	}
	if resp, _ := put(t, srv.URL+"/api/nodes/a/alias", `nope`); resp.StatusCode != 400 {
		t.Fatalf("bad JSON: status=%d", resp.StatusCode)
	}
}

func TestWritesMustBeJSONAndSameOrigin(t *testing.T) {
	var got calls
	reg, srv := newTestServer(t)
	reg.Seen("a", "utone-a", fakeNode(t, &got))

	do := func(method, path, ctype, origin, body string) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	host := strings.TrimPrefix(srv.URL, "http://")

	// What a page on another site can send without a preflight: a form or text/plain.
	for _, ct := range []string{"", "text/plain", "application/x-www-form-urlencoded", "multipart/form-data"} {
		if code := do("POST", "/api/nodes/a/rx/stop", ct, "", "{}"); code != http.StatusUnsupportedMediaType {
			t.Errorf("POST with Content-Type %q: status=%d, want 415", ct, code)
		}
		if code := do("PUT", "/api/nodes/a/alias", ct, "", `{"alias":"x"}`); code != http.StatusUnsupportedMediaType {
			t.Errorf("PUT with Content-Type %q: status=%d, want 415", ct, code)
		}
	}
	if n := len(got.all()); n != 0 {
		t.Fatalf("a refused request must not reach the node, it saw %d", n)
	}

	if code := do("POST", "/api/nodes/a/rx/stop", "application/json; charset=utf-8", "", "{}"); code != 200 {
		t.Errorf("JSON with a charset: status=%d", code)
	}
	if code := do("POST", "/api/nodes/a/rx/stop", "application/json", "http://"+host, "{}"); code != 200 {
		t.Errorf("same origin: status=%d", code)
	}
	if code := do("POST", "/api/nodes/a/rx/stop", "application/json", "http://evil.example", "{}"); code != http.StatusForbidden {
		t.Errorf("other origin: status=%d, want 403", code)
	}
	// reads are not affected, and carry nosniff
	resp, err := http.Get(srv.URL + "/api/nodes")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("GET: status=%d nosniff=%q", resp.StatusCode, resp.Header.Get("X-Content-Type-Options"))
	}
}
