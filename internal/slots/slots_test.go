// SPEC 0.5 V0.86: a pool of N slots for heavy jobs; first come, first
// served, a priority waiter ahead of the others, nobody loses a held slot.
package slots

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"
)

type grants struct {
	mu  sync.Mutex
	got []string
}

func (g *grants) on(owner, kind string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.got = append(g.got, owner+" "+kind)
}

func (g *grants) list() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.got...)
}

func TestV086QueueOrder(t *testing.T) {
	p := New(2)
	g := &grants{}
	p.OnGrant = g.on
	for _, x := range []struct {
		owner string
		held  bool
		ahead int
	}{{"a", true, 0}, {"b", true, 0}, {"c", false, 0}, {"d", false, 1}, {"e", false, 2}} {
		held, ahead := p.Acquire(x.owner, Test)
		if held != x.held || ahead != x.ahead {
			t.Fatalf("%s: held %v ahead %d, want %v %d", x.owner, held, ahead, x.held, x.ahead)
		}
	}
	// Asking again does not queue twice.
	if held, ahead := p.Acquire("d", Test); held || ahead != 1 {
		t.Fatalf("d again: %v %d", held, ahead)
	}
	if held, _ := p.Acquire("a", Build); !held {
		t.Fatal("a lost its slot by asking again")
	}
	// a releases: c (first in the queue) gets it and is told.
	if !p.Release("a") {
		t.Fatal("release a")
	}
	if got := g.list(); !slices.Equal(got, []string{"c test"}) {
		t.Fatalf("grants %v", got)
	}
	// d leaves the queue; e moves up.
	p.Release("d")
	if _, ahead := p.Acquire("e", Test); ahead != 0 {
		t.Fatalf("e ahead %d", ahead)
	}
	s := p.Snapshot()
	if s.N != 2 || len(s.Held) != 2 || s.Held[0].Owner != "b" || s.Held[1].Owner != "c" || len(s.Waiting) != 1 || s.Waiting[0].Owner != "e" {
		t.Fatalf("snapshot %+v", s)
	}
	if p.Release("nobody") {
		t.Fatal("released a slot nobody held")
	}
}

func TestV086DefaultIsTwo(t *testing.T) {
	if New(0).Snapshot().N != 2 || New(-1).Snapshot().N != 2 || New(3).Snapshot().N != 3 {
		t.Fatal("N")
	}
}

func TestV086PriorityGoesFirstWithoutTakingAHeldSlot(t *testing.T) {
	p := New(1)
	g := &grants{}
	p.OnGrant = g.on
	p.Acquire("a", Test)
	p.Acquire("b", Test) // queued
	got := make(chan func(), 1)
	go func() {
		release, err := p.Wait(context.Background(), "self-deploy", Test)
		if err != nil {
			t.Error(err)
		}
		got <- release
	}()
	waitFor(t, func() bool { return len(p.Snapshot().Waiting) == 2 })
	if s := p.Snapshot(); s.Waiting[0].Owner != "self-deploy" || !s.Waiting[0].Priority || s.Held[0].Owner != "a" {
		t.Fatalf("priority not first: %+v", s)
	}
	select {
	case <-got:
		t.Fatal("the deploy took a's slot")
	case <-time.After(50 * time.Millisecond):
	}
	// b, behind the deploy, is now 1 ahead.
	if _, ahead := p.Acquire("b", Test); ahead != 1 {
		t.Fatalf("b ahead %d", ahead)
	}
	p.Release("a")
	var release func()
	select {
	case release = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("the deploy never got the slot")
	}
	if len(g.list()) != 0 {
		t.Fatalf("an agent was told before the deploy: %v", g.list())
	}
	release()
	if got := g.list(); !slices.Equal(got, []string{"b test"}) {
		t.Fatalf("after the deploy: %v", got)
	}
}

func TestV086WaitEndsWithItsContext(t *testing.T) {
	p := New(1)
	p.Acquire("a", Test)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := p.Wait(ctx, "self-deploy", Test); done <- err }()
	waitFor(t, func() bool { return len(p.Snapshot().Waiting) == 1 })
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("no error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait did not end")
	}
	if s := p.Snapshot(); len(s.Waiting) != 0 {
		t.Fatalf("still queued: %+v", s)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatal("timed out")
}
