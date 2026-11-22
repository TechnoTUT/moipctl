package scout

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/TechnoTUT/moipctl/internal/discovery"
)

// Agent is the scout: it browses on the VLAN it is attached to and reports to
// the controller.
type Agent struct {
	Browser    discovery.Browser
	Controller string // base URL, e.g. http://moipctl:8081
	Source     string
	Interval   time.Duration // between rounds
	Window     time.Duration // how long a round listens
	Client     *http.Client
}

func (a *Agent) withDefaults() {
	if a.Interval <= 0 {
		a.Interval = 15 * time.Second
	}
	if a.Window <= 0 {
		a.Window = 4 * time.Second
	}
	if a.Client == nil {
		a.Client = &http.Client{Timeout: 10 * time.Second}
	}
}

// Run reports until ctx is done: once at once, then every Interval.
func (a *Agent) Run(ctx context.Context) {
	a.withDefaults()
	t := time.NewTicker(a.Interval)
	defer t.Stop()
	var lastErr string
	for {
		if err := a.Round(ctx); err != nil && ctx.Err() == nil {
			// log a repeated failure once, not every round
			if msg := err.Error(); msg != lastErr {
				log.Printf("scout %s: %v", a.Source, err)
				lastErr = msg
			}
		} else if err == nil {
			lastErr = ""
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Round browses once and reports the result. A failed browse sends nothing,
// so a broken scout cannot make the controller think the nodes are gone
// faster than its TTL; a round that finds nothing is reported as empty.
func (a *Agent) Round(ctx context.Context) error {
	a.withDefaults()
	entries, err := a.Browser.Browse(ctx, a.Window)
	if err != nil {
		return fmt.Errorf("browse: %w", err)
	}
	if entries == nil {
		entries = []discovery.Entry{}
	}
	body, err := json.Marshal(Report{Source: a.Source, Nodes: entries})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.Controller+ReportPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.Client.Do(req)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("report: %s: %s", resp.Status, bytes.TrimSpace(msg))
	}
	return nil
}
