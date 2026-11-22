package scout

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/TechnoTUT/moipctl/internal/registry"
)

// Hub is the controller's side: it turns reports into registry updates and
// expires nodes that no scout has reported for a while.
type Hub struct {
	Reg *registry.Registry
	// TTL is how long a node stays online without being reported again. Scouts
	// report every round, so this is a few rounds.
	TTL time.Duration

	mu   sync.Mutex
	last map[string]time.Time // node id -> time of the last report that listed it
	now  func() time.Time
}

func NewHub(reg *registry.Registry, ttl time.Duration) *Hub {
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	return &Hub{Reg: reg, TTL: ttl, last: map[string]time.Time{}, now: time.Now}
}

// Apply records a validated report.
func (h *Hub) Apply(r Report) {
	now := h.now()
	h.mu.Lock()
	for _, n := range r.Nodes {
		h.last[n.ID] = now
	}
	h.mu.Unlock()
	for _, n := range r.Nodes {
		if h.Reg.SeenFrom(r.Source, n.ID, n.Hostname, n.Addr) {
			log.Printf("scout %s: node %s (%s) at %s", r.Source, n.ID, n.Hostname, n.Addr)
		}
	}
}

// Sweep marks nodes offline that have not been reported within TTL. Only nodes
// that scouts reported can expire here; static nodes never do.
func (h *Hub) Sweep() {
	now := h.now()
	var gone []string
	h.mu.Lock()
	for id, t := range h.last {
		if now.Sub(t) > h.TTL {
			gone = append(gone, id)
			delete(h.last, id)
		}
	}
	h.mu.Unlock()
	for _, id := range gone {
		log.Printf("scout: node %s is gone", id)
		h.Reg.Gone(id)
	}
}

// Run sweeps until ctx is done.
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(max(h.TTL/3, time.Second))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Sweep()
		}
	}
}

// Handler serves POST /api/scout/report and /healthz. It must only be
// reachable from the scouts (a separate listener and a network policy), never
// from the networks that use the controller's API.
func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("POST "+ReportPath, func(w http.ResponseWriter, r *http.Request) {
		var rep Report
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&rep); err != nil {
			detail(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
		if err := rep.Validate(); err != nil {
			detail(w, http.StatusBadRequest, err.Error())
			return
		}
		h.Apply(rep)
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func detail(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, "{\"detail\":%q}\n", msg)
}
