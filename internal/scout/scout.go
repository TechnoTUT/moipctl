// Package scout splits discovery from the controller.
//
// mDNS does not cross the router, so something has to sit on each VLAN that
// has nodes. Instead of attaching the controller itself (which is reachable
// from the DJ and AV networks) to those VLANs, one small scout per VLAN does
// nothing but search for nodes and report the addresses it found to the
// controller over the cluster network. The controller then talks to the nodes
// through the router.
//
//	scout (one per VLAN) --POST /api/scout/report--> controller (Hub)
package scout

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/TechnoTUT/moipctl/internal/discovery"
)

// ReportPath is where the controller accepts reports.
const ReportPath = "/api/scout/report"

// Report is what a scout sends after every browse round, also when it found
// nothing: it is how the controller learns that the nodes are still there.
type Report struct {
	// Source names the scout, e.g. "av" or "ent".
	Source string            `json:"source"`
	Nodes  []discovery.Entry `json:"nodes"`
}

const (
	maxSource = 64
	maxField  = 128
	maxNodes  = 256
)

// Validate rejects a report the controller must not act on. The addresses end
// up as targets of HTTP requests from the controller, so they have to be plain
// unicast IPv4 host:port pairs.
func (r Report) Validate() error {
	if r.Source == "" || len(r.Source) > maxSource {
		return fmt.Errorf("source must be 1 to %d characters", maxSource)
	}
	if len(r.Nodes) > maxNodes {
		return fmt.Errorf("at most %d nodes per report", maxNodes)
	}
	for i, n := range r.Nodes {
		if n.ID == "" || len(n.ID) > maxField || len(n.Hostname) > maxField {
			return fmt.Errorf("nodes[%d]: id is required and id/hostname are at most %d characters", i, maxField)
		}
		if err := validAddr(n.Addr); err != nil {
			return fmt.Errorf("nodes[%d]: %w", i, err)
		}
	}
	return nil
}

func validAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("addr %q: want host:port", addr)
	}
	if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
		return fmt.Errorf("addr %q: bad port", addr)
	}
	ip := net.ParseIP(host).To4()
	switch {
	case ip == nil:
		return fmt.Errorf("addr %q: want an IPv4 address", addr)
	case ip.IsLoopback(), ip.IsLinkLocalUnicast(), ip.IsUnspecified(), ip.IsMulticast(), ip.Equal(net.IPv4bcast):
		return errors.New("addr " + strconv.Quote(addr) + ": not a unicast address")
	}
	return nil
}
