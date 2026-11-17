// Decision 0029 S2 (#186): a queued thread is told why it waits; the tick grants when the conditions change; the
// memory guard tells the holder whose job grew most, once per episode, and stops nothing.
package engine

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/slots"
)

func TestS2QueuedWithAReasonAndGrantedAtTheTick(t *testing.T) {
	e, _, _ := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	slow := 2.4
	sig := slots.Signals{Provider: "cgroups", Slowdown: &slow}
	p.Admit = func(head slots.Holder, held []slots.Holder) (bool, string) {
		c := make([]float64, len(held))
		for i := range c {
			c[i] = 1
		}
		return slots.Decide(slots.DefaultPolicy, sig, p.Budget(), c, 1)
	}
	e.UseSlots(p)
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	if out, _ := callAs(t, e, a, "slot_acquire", map[string]any{"kind": "test"}); !strings.Contains(out, "You hold") {
		t.Fatalf("a: one job always runs: %q", out)
	}
	out, _ := callAs(t, e, b, "slot_acquire", map[string]any{"kind": "test"})
	if !strings.Contains(out, "Queued for a test slot: 0 ahead. Waiting: CPU: new jobs run 2.4× slower.") {
		t.Fatalf("b: %q", out)
	}
	slow = 1.1
	e.Tick()
	if _, ok := e.Slots.Holding(b); !ok || !told(e, b, "slot is yours now") {
		t.Fatal("b was not granted at the tick once the host was calm")
	}
}

func TestS2MemoryGuardTellsTheBiggestJobOnce(t *testing.T) {
	e, _, _ := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	e.UseSlots(p)
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	p.Acquire(a, slots.Test)
	p.Acquire(b, slots.E2E)
	free := 0.06
	e.SlotPolicy = slots.DefaultPolicy
	e.SlotSignals = func() slots.Signals { return slots.Signals{Provider: "cgroups", Headroom: &free} }
	e.SlotGrowth = func(o string) uint64 { return map[string]uint64{a: 1e9, b: 4.1e9}[o] }
	e.Tick()
	if !told(e, b, "Memory is short on this host (memory: 6 % free); your job grew 4.1 GB") || told(e, a, "Memory is short") {
		t.Fatal("the biggest job was not the one told")
	}
	th, _ := e.Thread(b)
	n := len(th.Feed.Queue)
	e.Tick()
	if th, _ := e.Thread(b); len(th.Feed.Queue) != n {
		t.Fatal("told twice in one episode")
	}
	if _, ok := p.Holding(b); !ok {
		t.Fatal("the guard took a slot")
	}
}
