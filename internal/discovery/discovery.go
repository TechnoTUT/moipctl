// Package discovery finds moip-toolkit nodes over mDNS and keeps the registry
// up to date.
//
// A node advertises the service _moip._tcp with its HTTP API port and these
// TXT records:
//
//	id=<stable node id>   falls back to the instance name
//	hostname=<hostname>   falls back to the instance name
//
// The controller can be attached to several VLANs; every multicast-capable
// interface is queried, so nodes on all of them are found.
package discovery

import (
	"context"
	"errors"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/libp2p/zeroconf/v2"

	"github.com/TechnoTUT/moipctl/internal/registry"
)

// Service is the mDNS service type nodes advertise.
const Service = "_moip._tcp"

// Entry is a node found by one browse round.
type Entry struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	Addr     string `json:"addr"` // host:port
}

// Browser runs one browse round and returns what answered within window.
type Browser interface {
	Browse(ctx context.Context, window time.Duration) ([]Entry, error)
}

// Options tune the discovery loop.
type Options struct {
	Interval   time.Duration // time between rounds
	Window     time.Duration // how long one round listens for answers
	MissRounds int           // consecutive silent rounds before a node is marked offline
}

func (o Options) withDefaults() Options {
	if o.Interval <= 0 {
		o.Interval = 15 * time.Second
	}
	if o.Window <= 0 {
		o.Window = 4 * time.Second
	}
	if o.MissRounds <= 0 {
		o.MissRounds = 3
	}
	return o
}

// Discoverer turns browse rounds into registry updates.
type Discoverer struct {
	reg    *registry.Registry
	b      Browser
	opts   Options
	missed map[string]int
}

func New(reg *registry.Registry, b Browser, opts Options) *Discoverer {
	return &Discoverer{reg: reg, b: b, opts: opts.withDefaults(), missed: map[string]int{}}
}

// Run browses until ctx is done: once at once, then every Interval.
func (d *Discoverer) Run(ctx context.Context) {
	t := time.NewTicker(d.opts.Interval)
	defer t.Stop()
	for {
		d.Round(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Round runs one browse and applies the result. A node is only marked offline
// after MissRounds rounds without an answer, because one lost multicast
// packet must not flap a node. A failed round counts for nothing.
func (d *Discoverer) Round(ctx context.Context) {
	entries, err := d.b.Browse(ctx, d.opts.Window)
	if err != nil {
		if ctx.Err() == nil {
			log.Printf("discovery: browse failed: %v", err)
		}
		return
	}
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		seen[e.ID] = true
		d.missed[e.ID] = 0
		if d.reg.Seen(e.ID, e.Hostname, e.Addr) {
			log.Printf("discovery: node %s (%s) at %s", e.ID, e.Hostname, e.Addr)
		}
	}
	// Only nodes found here can expire here; nodes added by other means
	// (static addresses) are never marked offline by discovery.
	for id := range d.missed {
		if seen[id] {
			continue
		}
		d.missed[id]++
		if d.missed[id] >= d.opts.MissRounds {
			log.Printf("discovery: node %s is gone", id)
			d.reg.Gone(id)
			delete(d.missed, id)
		}
	}
}

// ZeroconfBrowser browses _moip._tcp on all multicast interfaces.
type ZeroconfBrowser struct{}

func (ZeroconfBrowser) Browse(ctx context.Context, window time.Duration) ([]Entry, error) {
	ctx, cancel := context.WithTimeout(ctx, window)
	defer cancel()

	ch := make(chan *zeroconf.ServiceEntry, 32)
	errc := make(chan error, 1)
	go func() { errc <- zeroconf.Browse(ctx, Service, "local.", ch) }()

	var out []Entry
	add := func(se *zeroconf.ServiceEntry) {
		if e, ok := entryFromService(se); ok {
			out = append(out, e)
		}
	}
	for {
		select {
		case se, ok := <-ch:
			if !ok {
				ch = nil // a nil channel blocks forever; wait for errc
				continue
			}
			add(se)
		case err := <-errc:
			for ch != nil { // collect what is still buffered
				select {
				case se, ok := <-ch:
					if !ok {
						ch = nil
						break
					}
					add(se)
				default:
					ch = nil
				}
			}
			if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				return nil, err
			}
			return out, nil
		}
	}
}

// localNets returns the subnets this host is directly attached to, one per
// IPv4 address of an up interface. It is a variable so tests can replace it.
var localNets = func() []*net.IPNet {
	var nets []*net.IPNet
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
			nets = append(nets, n)
		}
	}
	return nets
}

// pickIPv4 chooses the address to talk to out of everything a node answered
// with. A node answers with the addresses of all its interfaces, and the one
// that works is on a subnet this host is attached to (the VLAN the answer came
// in on), so those win; otherwise the first usable address is returned.
func pickIPv4(ips []net.IP) (net.IP, bool) {
	var first net.IP
	local := localNets()
	for _, ip := range ips {
		ip4 := ip.To4()
		if ip4 == nil || ip4.IsLinkLocalUnicast() || ip4.IsLoopback() {
			continue
		}
		for _, n := range local {
			if n.Contains(ip4) {
				return ip4, true
			}
		}
		if first == nil {
			first = ip4
		}
	}
	return first, first != nil
}

// entryFromService converts an mDNS answer. It returns false if the answer
// has no usable IPv4 address or port.
func entryFromService(se *zeroconf.ServiceEntry) (Entry, bool) {
	id, host := se.Instance, se.Instance
	for _, t := range se.Text {
		switch k, v, _ := strings.Cut(t, "="); k {
		case "id":
			if v != "" {
				id = v
			}
		case "hostname":
			if v != "" {
				host = v
			}
		}
	}
	if id == "" || se.Port == 0 {
		return Entry{}, false
	}
	ip, ok := pickIPv4(se.AddrIPv4)
	if !ok {
		return Entry{}, false
	}
	return Entry{ID: id, Hostname: host, Addr: net.JoinHostPort(ip.String(), strconv.Itoa(se.Port))}, true
}
