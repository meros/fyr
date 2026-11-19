// V0.project-priority: a new waiter goes ahead of the waiters of a lower
// project priority, never ahead of an equal or higher one, a priority wait,
// or a waiter that has waited RankAfter; the queued keep their order.
package slots

import (
	"context"
	"slices"
	"testing"
	"time"
)

func waiting(p *Pool) []string {
	var out []string
	for _, h := range p.Snapshot().Waiting {
		out = append(out, h.Owner)
	}
	return out
}

func TestVProjectPriorityPlacesNewWaiters(t *testing.T) {
	now := time.Unix(1000, 0)
	p := New(1)
	p.Now = func() time.Time { return now }
	ranks := map[string]int{"hi1": 1, "hi2": 1, "lo1": -1, "lo2": -1}
	p.Rank = func(o string) int { return ranks[o] }
	p.Acquire("holder", Test)
	for _, o := range []string{"lo1", "n1", "hi1", "lo2", "n2", "hi2"} {
		p.Acquire(o, Test)
	}
	if got, want := waiting(p), []string{"hi1", "hi2", "n1", "n2", "lo1", "lo2"}; !slices.Equal(got, want) {
		t.Fatalf("queue %v, want %v", got, want)
	}
	// The ahead count names the real place.
	if _, ahead := p.Acquire("n2", Test); ahead != 3 {
		t.Fatalf("n2 ahead %d", ahead)
	}
}

func TestVProjectPriorityKeepsTheOwnersOrderAndPriorityWaits(t *testing.T) {
	now := time.Unix(1000, 0)
	p := New(1)
	p.Now = func() time.Time { return now }
	ranks := map[string]int{"hi": 1, "lo": -1}
	p.Rank = func(o string) int { return ranks[o] }
	p.Acquire("holder", Test)
	p.Acquire("lo", Test)
	p.Acquire("n", Test)
	// The owner puts lo first (V0.137); it keeps its place ahead of n. A
	// later high one goes ahead of both, behind the deploy's priority wait;
	// a later normal one goes after n, the last waiter of its rank.
	p.Reorder([]string{"lo", "n"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go p.Wait(ctx, "self-deploy", Test)
	for len(waiting(p)) < 3 {
		time.Sleep(time.Millisecond)
	}
	p.Acquire("hi", Test)
	p.Acquire("n2", Test)
	if got, want := waiting(p), []string{"self-deploy", "hi", "lo", "n", "n2"}; !slices.Equal(got, want) {
		t.Fatalf("queue %v, want %v", got, want)
	}
}

func TestVProjectPriorityStarvesNoOne(t *testing.T) {
	now := time.Unix(1000, 0)
	p := New(1)
	p.Now = func() time.Time { return now }
	ranks := map[string]int{"hi1": 1, "hi2": 1, "lo": -1}
	p.Rank = func(o string) int { return ranks[o] }
	p.Acquire("holder", Test)
	p.Acquire("lo", Test)
	p.Acquire("hi1", Test)
	now = now.Add(RankAfter)
	p.Acquire("hi2", Test)
	if got, want := waiting(p), []string{"hi1", "lo", "hi2"}; !slices.Equal(got, want) {
		t.Fatalf("queue %v, want %v", got, want)
	}
}

func TestVProjectPriorityWithoutRankIsFirstComeFirstServed(t *testing.T) {
	p := New(1)
	for _, o := range []string{"holder", "a", "b", "c"} {
		p.Acquire(o, Test)
	}
	if got := waiting(p); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("queue %v", got)
	}
}
