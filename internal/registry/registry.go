// Package registry keeps the list of nodes moipctl knows about and the latest
// state each of them reported.
package registry

import (
	"sort"
	"sync"
	"time"

	"github.com/TechnoTUT/moipctl/internal/moip"
)

// Node is one moip-toolkit node. ID is stable across reboots and DHCP
// changes (the MAC-derived hostname); Addr is where the node is now.
//
// A Node returned by the registry is a snapshot. State is never modified
// after it is stored, so the pointer can be shared safely.
type Node struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Alias    string `json:"alias,omitempty"`
	Addr     string `json:"addr"`   // host:port of the node's HTTP API
	Online   bool   `json:"online"` // advertised on the network
	// Connected is true while the node's state stream is alive. An online node
	// that is not connected shows a State that may be stale.
	Connected bool        `json:"connected"`
	LastSeen  time.Time   `json:"last_seen"`
	State     *moip.State `json:"state,omitempty"`
}

// Name is what to show for the node: the alias if set, else the hostname.
func (n Node) Name() string {
	if n.Alias != "" {
		return n.Alias
	}
	return n.Hostname
}

type Registry struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	subs  map[chan struct{}]struct{}
	now   func() time.Time
}

func New() *Registry {
	return &Registry{
		nodes: map[string]*Node{},
		subs:  map[chan struct{}]struct{}{},
		now:   time.Now,
	}
}

// Seen records that the node id is advertised at addr and marks it online.
// It returns true if the node was new or its address changed, which is when a
// state stream has to be (re)opened.
func (r *Registry) Seen(id, hostname, addr string) (changed bool) {
	r.mu.Lock()
	n, ok := r.nodes[id]
	if !ok {
		n = &Node{ID: id}
		r.nodes[id] = n
	}
	changed = !ok || n.Addr != addr || !n.Online
	if n.Addr != addr {
		n.Connected = false // the old stream, if any, was to another address
	}
	n.Hostname, n.Addr, n.Online, n.LastSeen = hostname, addr, true, r.now()
	r.mu.Unlock()
	if changed {
		r.notify()
	}
	return changed
}

// Gone marks the node offline. The entry and its last state are kept so the
// UI can still show what the node was doing.
func (r *Registry) Gone(id string) {
	r.mu.Lock()
	n, ok := r.nodes[id]
	changed := ok && (n.Online || n.Connected)
	if changed {
		n.Online, n.Connected = false, false
	}
	r.mu.Unlock()
	if changed {
		r.notify()
	}
}

// SetState stores the latest state received from the node and marks its
// stream connected. Unknown ids are ignored.
func (r *Registry) SetState(id string, st *moip.State) {
	r.mu.Lock()
	n, ok := r.nodes[id]
	if ok {
		n.State, n.Connected, n.LastSeen = st, true, r.now()
	}
	r.mu.Unlock()
	if ok {
		r.notify()
	}
}

// SetConnected records whether the node's state stream is alive.
func (r *Registry) SetConnected(id string, connected bool) {
	r.mu.Lock()
	n, ok := r.nodes[id]
	changed := ok && n.Connected != connected
	if changed {
		n.Connected = connected
	}
	r.mu.Unlock()
	if changed {
		r.notify()
	}
}

// SetAlias sets the display name of a node. It returns false for an unknown id.
func (r *Registry) SetAlias(id, alias string) bool {
	r.mu.Lock()
	n, ok := r.nodes[id]
	if ok {
		n.Alias = alias
	}
	r.mu.Unlock()
	if ok {
		r.notify()
	}
	return ok
}

func (r *Registry) Get(id string) (Node, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	n, ok := r.nodes[id]
	if !ok {
		return Node{}, false
	}
	return *n, true
}

// List returns all nodes ordered by display name, then ID.
func (r *Registry) List() []Node {
	r.mu.RLock()
	out := make([]Node, 0, len(r.nodes))
	for _, n := range r.nodes {
		out = append(out, *n)
	}
	r.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if a, b := out[i].Name(), out[j].Name(); a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Subscribe returns a channel that receives a value after the registry
// changes. Notifications are coalesced: a slow reader sees one value for any
// number of changes, then reads the registry with List. Call cancel when done.
func (r *Registry) Subscribe() (ch <-chan struct{}, cancel func()) {
	c := make(chan struct{}, 1)
	r.mu.Lock()
	r.subs[c] = struct{}{}
	r.mu.Unlock()
	return c, func() {
		r.mu.Lock()
		delete(r.subs, c)
		r.mu.Unlock()
	}
}

func (r *Registry) notify() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for c := range r.subs {
		select {
		case c <- struct{}{}:
		default:
		}
	}
}
