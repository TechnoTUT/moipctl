// Package stream follows every online node's GET /api/events (Server-Sent
// Events) and stores the state it carries in the registry.
package stream

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/TechnoTUT/moipctl/internal/moip"
	"github.com/TechnoTUT/moipctl/internal/registry"
)

// Manager keeps exactly one stream open per online node, at the node's
// current address.
type Manager struct {
	reg    *registry.Registry
	client *http.Client

	// Idle is how long a stream may stay silent before it is dropped. A node
	// sends a state or a keepalive comment every 500 ms, so silence means a
	// dead link that TCP has not noticed yet.
	Idle       time.Duration
	BackoffMin time.Duration
	BackoffMax time.Duration
}

func New(reg *registry.Registry) *Manager {
	return &Manager{
		reg: reg,
		client: &http.Client{Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
			ResponseHeaderTimeout: 5 * time.Second,
			// no overall timeout: the response body is the stream
		}},
		Idle:       5 * time.Second,
		BackoffMin: time.Second,
		BackoffMax: 10 * time.Second,
	}
}

type running struct {
	addr   string
	cancel context.CancelFunc
}

// Run reconciles streams with the registry until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	changes, unsubscribe := m.reg.Subscribe()
	defer unsubscribe()

	var wg sync.WaitGroup
	streams := map[string]running{}
	defer func() {
		for _, s := range streams {
			s.cancel()
		}
		wg.Wait()
	}()

	for {
		want := map[string]string{}
		for _, n := range m.reg.List() {
			if n.Online {
				want[n.ID] = n.Addr
			}
		}
		for id, s := range streams {
			if addr, ok := want[id]; !ok || addr != s.addr {
				s.cancel()
				delete(streams, id)
			}
		}
		for id, addr := range want {
			if _, ok := streams[id]; ok {
				continue
			}
			sctx, cancel := context.WithCancel(ctx)
			streams[id] = running{addr: addr, cancel: cancel}
			wg.Add(1)
			go func() {
				defer wg.Done()
				m.follow(sctx, id, addr)
			}()
		}

		select {
		case <-ctx.Done():
			return
		case <-changes:
		}
	}
}

// follow keeps one node's stream open, reconnecting with backoff, until ctx
// is cancelled.
func (m *Manager) follow(ctx context.Context, id, addr string) {
	backoff := m.BackoffMin
	var lastErr string
	for ctx.Err() == nil {
		gotState, err := m.once(ctx, id, addr)
		if ctx.Err() != nil {
			return
		}
		m.reg.SetConnected(id, false)
		if gotState {
			backoff = m.BackoffMin
		}
		// Log a failure once, not on every retry.
		if msg := fmt.Sprint(err); msg != lastErr {
			log.Printf("stream: node %s (%s): %v", id, addr, err)
			lastErr = msg
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, m.BackoffMax)
	}
}

// once opens the stream and reads it until it ends. gotState reports whether
// at least one state arrived, which resets the reconnect backoff.
func (m *Manager) once(ctx context.Context, id, addr string) (gotState bool, err error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	idle := time.AfterFunc(m.Idle, cancel)
	defer idle.Stop()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/events", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := m.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("GET /api/events: %s", resp.Status)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var event string
	var data strings.Builder
	for sc.Scan() {
		idle.Reset(m.Idle)
		line := sc.Text()
		switch {
		case line == "": // end of one event
			if event == "state" && data.Len() > 0 {
				var st moip.State
				if err := json.Unmarshal([]byte(data.String()), &st); err != nil {
					log.Printf("stream: node %s: bad state: %v", id, err)
				} else {
					m.reg.SetState(id, &st)
					gotState = true
				}
			}
			event = ""
			data.Reset()
		case strings.HasPrefix(line, ":"): // comment, e.g. keepalive
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil { // cancelled by the idle timer (follow handles a cancelled parent)
			return gotState, fmt.Errorf("no data for %s", m.Idle)
		}
		return gotState, err
	}
	return gotState, fmt.Errorf("stream closed by node")
}
