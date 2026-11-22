package stream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TechnoTUT/moipctl/internal/registry"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func newManager(reg *registry.Registry) *Manager {
	m := New(reg)
	m.Idle = 150 * time.Millisecond
	m.BackoffMin = 10 * time.Millisecond
	m.BackoffMax = 40 * time.Millisecond
	return m
}

func start(t *testing.T, m *Manager) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func sendState(w http.ResponseWriter, running bool) {
	fmt.Fprintf(w, "event: state\ndata: {\"rx\":{\"running\":%v},\"unknown_field\":1}\n\n", running)
	w.(http.Flusher).Flush()
}

func rxRunning(reg *registry.Registry, id string) (running, ok bool) {
	n, found := reg.Get(id)
	if !found || n.State == nil {
		return false, false
	}
	return n.State.RX.Running, true
}

func TestStoresStateFromEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": keepalive\n\n")
		sendState(w, true)
		<-r.Context().Done()
	}))
	defer srv.Close()

	reg := registry.New()
	start(t, newManager(reg))
	reg.Seen("a", "utone-a", srv.Listener.Addr().String())

	waitFor(t, "state", func() bool { r, ok := rxRunning(reg, "a"); return ok && r })
	if n, _ := reg.Get("a"); !n.Connected {
		t.Fatal("node should be connected while the stream is open")
	}
}

func TestReconnectsAfterStreamEnds(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if conns.Add(1) == 1 {
			sendState(w, false)
			return // node closes the stream
		}
		sendState(w, true)
		<-r.Context().Done()
	}))
	defer srv.Close()

	reg := registry.New()
	start(t, newManager(reg))
	reg.Seen("a", "utone-a", srv.Listener.Addr().String())

	waitFor(t, "state after reconnect", func() bool { r, ok := rxRunning(reg, "a"); return ok && r })
}

func TestSilentStreamIsDropped(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		conns.Add(1)
		sendState(w, true)
		<-r.Context().Done() // then nothing, not even keepalives
	}))
	defer srv.Close()

	reg := registry.New()
	start(t, newManager(reg))
	reg.Seen("a", "utone-a", srv.Listener.Addr().String())

	waitFor(t, "reconnect after idle", func() bool { return conns.Load() >= 2 })
}

func TestStopsWhenNodeIsGoneAndFollowsNewAddress(t *testing.T) {
	var closed atomic.Bool
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sendState(w, false)
		<-r.Context().Done()
		closed.Store(true)
	}))
	defer old.Close()
	moved := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		sendState(w, true)
		<-r.Context().Done()
	}))
	defer moved.Close()

	reg := registry.New()
	start(t, newManager(reg))
	reg.Seen("a", "utone-a", old.Listener.Addr().String())
	waitFor(t, "first state", func() bool { _, ok := rxRunning(reg, "a"); return ok })

	reg.Seen("a", "utone-a", moved.Listener.Addr().String()) // DHCP gave a new address
	waitFor(t, "state from new address", func() bool { r, ok := rxRunning(reg, "a"); return ok && r })
	waitFor(t, "old stream closed", closed.Load)
}

func TestHTTPErrorKeepsRetryingWithoutConnected(t *testing.T) {
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conns.Add(1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	reg := registry.New()
	start(t, newManager(reg))
	reg.Seen("a", "utone-a", srv.Listener.Addr().String())

	waitFor(t, "retries", func() bool { return conns.Load() >= 3 })
	if n, _ := reg.Get("a"); n.Connected {
		t.Fatal("must not be connected")
	}
}
