package registry

import (
	"testing"

	"github.com/TechnoTUT/moipctl/internal/moip"
)

func TestSeenReportsNewAndMovedNodes(t *testing.T) {
	r := New()
	if !r.Seen("a", "utone-a", "10.0.0.1:8000") {
		t.Fatal("new node should report changed")
	}
	if r.Seen("a", "utone-a", "10.0.0.1:8000") {
		t.Fatal("same address should not report changed")
	}
	if !r.Seen("a", "utone-a", "10.0.0.2:8000") {
		t.Fatal("new address (DHCP change) should report changed")
	}
	n, _ := r.Get("a")
	if n.Addr != "10.0.0.2:8000" || !n.Online {
		t.Fatalf("got %+v", n)
	}
}

func TestGoneKeepsEntryAndStateThenSeenRevives(t *testing.T) {
	r := New()
	r.Seen("a", "utone-a", "10.0.0.1:8000")
	r.SetState("a", &moip.State{})
	r.Gone("a")
	n, ok := r.Get("a")
	if !ok || n.Online || n.State == nil {
		t.Fatalf("got %+v ok=%v", n, ok)
	}
	if !r.Seen("a", "utone-a", "10.0.0.1:8000") {
		t.Fatal("coming back online should report changed")
	}
}

func TestListOrderUsesAliasThenID(t *testing.T) {
	r := New()
	r.Seen("2", "utone-b", "x:1")
	r.Seen("1", "utone-c", "x:2")
	r.Seen("3", "utone-a", "x:3")
	r.SetAlias("1", "entrance") // sorts before the hostnames
	var got []string
	for _, n := range r.List() {
		got = append(got, n.ID)
	}
	want := []string{"1", "3", "2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestSetStateIgnoresUnknownID(t *testing.T) {
	r := New()
	r.SetState("nope", &moip.State{})
	if len(r.List()) != 0 {
		t.Fatal("unknown id must not create a node")
	}
	if r.SetAlias("nope", "x") {
		t.Fatal("SetAlias on unknown id must return false")
	}
}

func TestSubscribeCoalesces(t *testing.T) {
	r := New()
	ch, cancel := r.Subscribe()
	defer cancel()
	r.Seen("a", "h", "x:1")
	r.SetAlias("a", "one")
	r.SetAlias("a", "two")
	select {
	case <-ch:
	default:
		t.Fatal("expected a notification")
	}
	select {
	case <-ch:
		t.Fatal("notifications should be coalesced")
	default:
	}
}

func TestConnectedFollowsStreamAndAddress(t *testing.T) {
	r := New()
	r.Seen("a", "h", "10.0.0.1:8000")
	if n, _ := r.Get("a"); n.Connected {
		t.Fatal("a node is not connected before a state arrives")
	}
	r.SetState("a", &moip.State{})
	if n, _ := r.Get("a"); !n.Connected {
		t.Fatal("a state means the stream is alive")
	}
	r.SetConnected("a", false)
	if n, _ := r.Get("a"); n.Connected || n.State == nil {
		t.Fatalf("lost stream keeps the last state but clears Connected: %+v", n)
	}
	r.SetState("a", &moip.State{})
	r.Seen("a", "h", "10.0.0.2:8000")
	if n, _ := r.Get("a"); n.Connected {
		t.Fatal("moving to a new address invalidates the old stream")
	}
	r.SetState("a", &moip.State{})
	r.Gone("a")
	if n, _ := r.Get("a"); n.Connected || n.Online {
		t.Fatal("Gone clears Online and Connected")
	}
}

func TestAliasesSurviveBeforeAndAfterTheNodeIsSeen(t *testing.T) {
	r := New()
	r.LoadAliases(map[string]string{"a": "entrance", "b": ""})
	r.Seen("a", "utone-a", "x:1")
	if n, _ := r.Get("a"); n.Alias != "entrance" || n.Name() != "entrance" {
		t.Fatalf("remembered alias should apply when the node appears: %+v", n)
	}
	if got := r.Aliases(); len(got) != 1 || got["a"] != "entrance" {
		t.Fatalf("got %v", got)
	}
	r.SetAlias("a", "")
	if n, _ := r.Get("a"); n.Alias != "" || len(r.Aliases()) != 0 {
		t.Fatal("empty alias should clear it")
	}
	r.Gone("a")
	r.SetAlias("a", "lobby")
	r.Seen("a", "utone-a", "x:2")
	if n, _ := r.Get("a"); n.Alias != "lobby" {
		t.Fatalf("got %+v", n)
	}
}
