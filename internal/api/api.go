// Package api serves moipctl's own REST API.
//
//	GET /api/nodes          all known nodes
//	GET /api/nodes/{id}     one node
//	GET /api/events         Server-Sent Events: the node list, sent when it changes
//
// Errors are returned as {"detail": "..."} like moip-toolkit's API.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/TechnoTUT/moipctl/internal/registry"
)

type Server struct {
	Reg *registry.Registry
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/nodes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, s.Reg.List())
	})
	mux.HandleFunc("GET /api/nodes/{id}", func(w http.ResponseWriter, r *http.Request) {
		n, ok := s.Reg.Get(r.PathValue("id"))
		if !ok {
			httpError(w, http.StatusNotFound, "unknown node")
			return
		}
		writeJSON(w, http.StatusOK, n)
	})
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpError(w, http.StatusNotFound, "not found")
	})
	return mux
}

// events streams the node list: once on connect, then after every change.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		httpError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	changes, cancel := s.Reg.Subscribe()
	defer cancel()
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	send := func() error {
		b, err := json.Marshal(s.Reg.List())
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "event: nodes\ndata: %s\n\n", b)
		fl.Flush()
		return err
	}
	if send() != nil {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-changes:
			if send() != nil {
				return
			}
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			fl.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, status int, detail string) {
	writeJSON(w, status, map[string]string{"detail": detail})
}
