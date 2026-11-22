package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TechnoTUT/moipctl/internal/registry"
)

func newTestServer(t *testing.T) (*registry.Registry, *httptest.Server) {
	t.Helper()
	reg := registry.New()
	srv := httptest.NewServer((&Server{Reg: reg}).Handler())
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
