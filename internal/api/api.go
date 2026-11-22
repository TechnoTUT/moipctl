// Package api serves moipctl's own REST API.
//
//	GET /api/nodes          all known nodes
//	GET /api/nodes/{id}     one node
//	GET /api/events         Server-Sent Events: the node list, sent when it changes
//	PUT /api/nodes/{id}/alias
//	                        {"alias": "..."}: set the display name ("" clears it)
//	POST /api/nodes/{id}/{role}/{action}
//	                        forward an operation to one node; the body goes to
//	                        the node as is and the node's reply comes back
//	POST /api/bulk/{role}/{action}
//	                        {"nodes": [ids], "body": {...}}: run it on several
//	                        nodes; {"results": [...]} has one entry per node
//
// Errors are returned as {"detail": "..."} like moip-toolkit's API.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/TechnoTUT/moipctl/internal/control"
	"github.com/TechnoTUT/moipctl/internal/registry"
)

const (
	maxBody      = 1 << 20
	maxBulkNodes = 64
)

type Server struct {
	Reg *registry.Registry
	Ctl *control.Controller
	// Persist, if set, is called with all aliases after each change.
	Persist func(aliases map[string]string) error
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
	mux.HandleFunc("PUT /api/nodes/{id}/alias", s.setAlias)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("POST /api/nodes/{id}/{role}/{action}", s.call)
	mux.HandleFunc("POST /api/bulk/{role}/{action}", s.bulk)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpError(w, http.StatusNotFound, "not found")
	})
	return mux
}

const maxAliasLen = 64

func (s *Server) setAlias(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alias string `json:"alias"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	alias := strings.TrimSpace(req.Alias)
	if utf8.RuneCountInString(alias) > maxAliasLen {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("alias is longer than %d characters", maxAliasLen))
		return
	}
	id := r.PathValue("id")
	if !s.Reg.SetAlias(id, alias) {
		httpError(w, http.StatusNotFound, "unknown node")
		return
	}
	if s.Persist != nil {
		if err := s.Persist(s.Reg.Aliases()); err != nil {
			httpError(w, http.StatusInternalServerError, "alias is set but could not be saved: "+err.Error())
			return
		}
	}
	n, _ := s.Reg.Get(id)
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		httpError(w, http.StatusRequestEntityTooLarge, "body too large")
		return
	}
	res := s.Ctl.Call(r.Context(), r.PathValue("id"), r.PathValue("role"), r.PathValue("action"), body)
	if !res.OK {
		httpError(w, res.Code, res.Error)
		return
	}
	if len(res.Response) == 0 {
		res.Response = json.RawMessage("{}")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(res.Code)
	w.Write(res.Response)
}

func (s *Server) bulk(w http.ResponseWriter, r *http.Request) {
	role, action := r.PathValue("role"), r.PathValue("action")
	if !control.ValidOp(role, action) {
		httpError(w, http.StatusNotFound, "unsupported operation")
		return
	}
	var req struct {
		Nodes []string        `json:"nodes"`
		Body  json.RawMessage `json:"body"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	// Ask for ids explicitly: "everything" is too easy to hit by accident
	// when a node does not have the role.
	if len(req.Nodes) == 0 || len(req.Nodes) > maxBulkNodes {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("nodes must list 1 to %d ids", maxBulkNodes))
		return
	}
	seen := map[string]bool{}
	ids := req.Nodes[:0:0]
	for _, id := range req.Nodes {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": s.Ctl.CallMany(r.Context(), ids, role, action, req.Body)})
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
