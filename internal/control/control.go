// Package control sends operations to nodes.
package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/TechnoTUT/moipctl/internal/registry"
)

// ops lists the node operations moipctl forwards: role -> allowed actions.
// Anything else is rejected before it reaches a node.
var ops = map[string]map[string]bool{
	"rx":        {"start": true, "switch": true, "stop": true},
	"tx":        {"start": true, "stop": true},
	"multiview": {"start": true, "stop": true},
	"signage":   {"start": true, "stop": true},
}

// ValidOp reports whether role/action may be forwarded.
func ValidOp(role, action string) bool { return ops[role][action] }

// Result is the outcome of one operation on one node.
type Result struct {
	ID    string `json:"id"`
	OK    bool   `json:"ok"`
	Code  int    `json:"code"`            // HTTP status: the node's, or 404/409/502 when it was not reached
	Error string `json:"error,omitempty"` // reason when !OK
	// Response is the node's JSON body, when it sent one.
	Response json.RawMessage `json:"response,omitempty"`
}

type Controller struct {
	Reg     *registry.Registry
	Client  *http.Client
	Timeout time.Duration // per node
}

func New(reg *registry.Registry) *Controller {
	return &Controller{Reg: reg, Client: &http.Client{}, Timeout: 10 * time.Second}
}

// Call runs POST /api/<role>/<action> on one node with body (may be empty).
func (c *Controller) Call(ctx context.Context, id, role, action string, body []byte) Result {
	n, ok := c.Reg.Get(id)
	switch {
	case !ValidOp(role, action):
		return Result{ID: id, Code: http.StatusNotFound, Error: fmt.Sprintf("unsupported operation %s/%s", role, action)}
	case !ok:
		return Result{ID: id, Code: http.StatusNotFound, Error: "unknown node"}
	case !n.Online:
		return Result{ID: id, Code: http.StatusConflict, Error: "node is offline"}
	}

	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("http://%s/api/%s/%s", n.Addr, role, action), bytes.NewReader(body))
	if err != nil {
		return Result{ID: id, Code: http.StatusBadGateway, Error: err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Client.Do(req)
	if err != nil {
		return Result{ID: id, Code: http.StatusBadGateway, Error: err.Error()}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	res := Result{ID: id, Code: resp.StatusCode, OK: resp.StatusCode/100 == 2}
	if json.Valid(data) {
		res.Response = data
	}
	if !res.OK {
		res.Error = http.StatusText(resp.StatusCode)
		var e struct {
			Detail string `json:"detail"`
		}
		if json.Unmarshal(data, &e) == nil && e.Detail != "" {
			res.Error = e.Detail
		}
	}
	return res
}

// CallMany runs the same operation on several nodes in parallel. Results are
// in the order of ids; one node failing does not stop the others.
func (c *Controller) CallMany(ctx context.Context, ids []string, role, action string, body []byte) []Result {
	out := make([]Result, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = c.Call(ctx, id, role, action, body)
		}()
	}
	wg.Wait()
	return out
}
