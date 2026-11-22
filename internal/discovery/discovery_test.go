package discovery

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/libp2p/zeroconf/v2"

	"github.com/TechnoTUT/moipctl/internal/registry"
)

type fakeBrowser struct {
	rounds [][]Entry
	err    error
	i      int
}

func (f *fakeBrowser) Browse(context.Context, time.Duration) ([]Entry, error) {
	if f.err != nil {
		return nil, f.err
	}
	r := f.rounds[f.i]
	if f.i < len(f.rounds)-1 {
		f.i++
	}
	return r, nil
}

var nodeA = Entry{ID: "a", Hostname: "utone-a", Addr: "10.0.0.1:8000"}

func TestRoundMarksOfflineOnlyAfterMissRounds(t *testing.T) {
	reg := registry.New()
	fb := &fakeBrowser{rounds: [][]Entry{{nodeA}, {}}}
	d := New(reg, fb, Options{MissRounds: 3})
	ctx := context.Background()

	d.Round(ctx)
	if n, _ := reg.Get("a"); !n.Online {
		t.Fatal("node should be online after it answered")
	}
	d.Round(ctx)
	d.Round(ctx)
	if n, _ := reg.Get("a"); !n.Online {
		t.Fatal("node must survive fewer than MissRounds silent rounds")
	}
	d.Round(ctx)
	if n, _ := reg.Get("a"); n.Online {
		t.Fatal("node should be offline after MissRounds silent rounds")
	}
}

func TestAnswerResetsMissCounterAndFollowsNewAddress(t *testing.T) {
	reg := registry.New()
	moved := Entry{ID: "a", Hostname: "utone-a", Addr: "10.0.0.9:8000"}
	fb := &fakeBrowser{rounds: [][]Entry{{nodeA}, {}, {}, {moved}, {}, {}}}
	d := New(reg, fb, Options{MissRounds: 3})
	for range 6 {
		d.Round(context.Background())
	}
	n, _ := reg.Get("a")
	if !n.Online || n.Addr != "10.0.0.9:8000" {
		t.Fatalf("got %+v", n)
	}
}

func TestFailedRoundDoesNotCountAsMiss(t *testing.T) {
	reg := registry.New()
	fb := &fakeBrowser{rounds: [][]Entry{{nodeA}}}
	d := New(reg, fb, Options{MissRounds: 1})
	d.Round(context.Background())
	fb.err = errors.New("no multicast interface")
	d.Round(context.Background())
	if n, _ := reg.Get("a"); !n.Online {
		t.Fatal("a failed browse must not mark nodes offline")
	}
}

func svc(instance string, port int, text []string, ips ...string) *zeroconf.ServiceEntry {
	se := &zeroconf.ServiceEntry{ServiceRecord: zeroconf.ServiceRecord{Instance: instance}}
	se.Port, se.Text = port, text
	for _, s := range ips {
		se.AddrIPv4 = append(se.AddrIPv4, net.ParseIP(s))
	}
	return se
}

func TestEntryFromService(t *testing.T) {
	e, ok := entryFromService(svc("inst", 8000, []string{"id=aa:bb", "hostname=utone-a", "version=1"}, "169.254.1.1", "10.0.0.5"))
	if !ok || e.ID != "aa:bb" || e.Hostname != "utone-a" || e.Addr != "10.0.0.5:8000" {
		t.Fatalf("got %+v ok=%v", e, ok)
	}
	e, ok = entryFromService(svc("utone-b", 8000, nil, "10.0.0.6"))
	if !ok || e.ID != "utone-b" || e.Hostname != "utone-b" {
		t.Fatalf("instance name should be the fallback, got %+v ok=%v", e, ok)
	}
	if _, ok := entryFromService(svc("x", 8000, nil, "169.254.1.1")); ok {
		t.Fatal("link-local only must be rejected")
	}
	if _, ok := entryFromService(svc("x", 0, nil, "10.0.0.6")); ok {
		t.Fatal("missing port must be rejected")
	}
}

func TestNodesNotFoundByDiscoveryNeverExpire(t *testing.T) {
	reg := registry.New()
	reg.Seen("static", "static", "10.0.0.7:8000")
	d := New(reg, &fakeBrowser{rounds: [][]Entry{{}}}, Options{MissRounds: 1})
	for range 3 {
		d.Round(context.Background())
	}
	if n, _ := reg.Get("static"); !n.Online {
		t.Fatal("discovery must not expire a node it did not find")
	}
}

func TestPicksTheAddressOnADirectlyAttachedSubnet(t *testing.T) {
	_, av, _ := net.ParseCIDR("10.10.16.0/20")
	defer func(old func() []*net.IPNet) { localNets = old }(localNets)
	localNets = func() []*net.IPNet { return []*net.IPNet{av} }

	// What a pod on VLAN 20 and 42 answered in the cluster: the pod network
	// address of the other side is listed first and is not reachable.
	e, ok := entryFromService(svc("n", 8000, nil, "10.15.1.226", "192.168.12.34", "10.10.20.3"))
	if !ok || e.Addr != "10.10.20.3:8000" {
		t.Fatalf("want the address on the attached subnet, got %+v ok=%v", e, ok)
	}

	// Attached to nothing that matches: the first usable address, as before.
	localNets = func() []*net.IPNet { return nil }
	if e, _ := entryFromService(svc("n", 8000, nil, "169.254.1.1", "10.15.1.226", "10.10.20.3")); e.Addr != "10.15.1.226:8000" {
		t.Fatalf("got %+v", e)
	}
}

func TestBrowseMergesPassesAndKeepsTheLatestAnswer(t *testing.T) {
	moved := Entry{ID: "a", Hostname: "utone-a", Addr: "10.10.20.9:8000"}
	other := Entry{ID: "b", Hostname: "utone-b", Addr: "10.10.20.4:8000"}
	var calls []time.Duration
	answers := [][]Entry{{nodeAForTest}, {}, {moved, other}} // a pass may come back empty
	b := ZeroconfBrowser{pass: func(_ context.Context, d time.Duration) ([]Entry, error) {
		calls = append(calls, d)
		r := answers[len(calls)-1]
		return r, nil
	}}
	got, err := b.Browse(context.Background(), 6*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || calls[0] != 2*time.Second {
		t.Fatalf("want 3 passes of 2s, got %v", calls)
	}
	if len(got) != 2 || got[0] != moved || got[1] != other {
		t.Fatalf("got %+v", got)
	}
}

func TestBrowseFindsANodeThatOnlyOnePassSees(t *testing.T) {
	n := 0
	b := ZeroconfBrowser{pass: func(context.Context, time.Duration) ([]Entry, error) {
		n++
		if n == 3 { // the first two queries went unanswered
			return []Entry{nodeAForTest}, nil
		}
		return nil, nil
	}}
	got, _ := b.Browse(context.Background(), 6*time.Second)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestBrowseReturnsAPassError(t *testing.T) {
	b := ZeroconfBrowser{pass: func(context.Context, time.Duration) ([]Entry, error) {
		return nil, errors.New("no multicast interface")
	}}
	if _, err := b.Browse(context.Background(), time.Second); err == nil {
		t.Fatal("want the error")
	}
}

func TestBrowseStopsWhenTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	b := ZeroconfBrowser{pass: func(context.Context, time.Duration) ([]Entry, error) {
		n++
		cancel()
		return nil, nil
	}}
	b.Browse(ctx, 6*time.Second)
	if n != 1 {
		t.Fatalf("want to stop after the cancelled pass, ran %d", n)
	}
}

var nodeAForTest = Entry{ID: "a", Hostname: "utone-a", Addr: "10.10.20.3:8000"}
