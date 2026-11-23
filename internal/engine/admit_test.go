// Decision 0029 S2 (#186): a queued thread is told why it waits; the tick grants when the conditions change; the
// memory guard tells the holder whose job grew most, once per episode, and stops nothing.
package engine

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/slots"
)

func TestS2QueuedWithAReasonAndGrantedAtTheTick(t *testing.T) {
	e, _, _ := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	slow := 2.4
	sig := resource.Signals{Provider: "cgroups", Slowdown: &slow, CPU: 4, Memory: 8 << 30}
	p.Admit = func(head resource.Holder, held []resource.Holder) (bool, string, string) {
		c := make([]resource.Need, len(held))
		for i := range c {
			c[i] = resource.Need{CPU: 1, Mem: 1 << 30}
		}
		return guard.Decide(resource.DefaultPolicy, sig, p.Budget(), 2, c, resource.Need{CPU: 1, Mem: 1 << 30})
	}
	e.UseSlots(p)
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	if out := acquire(e, a, "test"); out != "admitted" {
		t.Fatalf("a: one job always runs: %q", out)
	}
	out := acquire(e, b, "test")
	if !strings.Contains(out, "(0 ahead). Waiting: CPU: new jobs run 2.4× slower.") {
		t.Fatalf("b: %q", out)
	}
	slow = 1.1
	e.Tick()
	if _, ok := e.Slots.Holding(b); !ok || !told(e, b, "job is admitted now") {
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
	p.Acquire(a, resource.Test)
	p.Acquire(b, resource.E2E)
	free := 0.06
	e.SlotPolicy = resource.DefaultPolicy
	e.SlotSignals = func() resource.Signals { return resource.Signals{Provider: "cgroups", Headroom: &free} }
	e.SlotGrowth = func(o string) uint64 { return map[string]uint64{a: 1e9, b: 4.1e9}[o] }
	guardDeps(t, e)
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
