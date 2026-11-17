// Decision 0029 S4 (#188): the engine's tick steps the host's budget controller and sets the pool's budget, which
// admits what now fits.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

func TestS4TheTickRaisesTheBudgetOnACalmHost(t *testing.T) {
	e, _, now := bare(t)
	p := slots.New(1)
	p.Now = e.Now
	calm := 1.0
	free := 0.6
	sig := slots.Signals{Provider: "cgroups", Slowdown: &calm, Headroom: &free, Stall: slots.StallOK}
	p.Admit = func(head slots.Holder, held []slots.Holder) (bool, string) {
		c := make([]float64, len(held))
		for i := range c {
			c[i] = 0.5
		}
		return slots.Decide(slots.DefaultPolicy, sig, p.Budget(), c, 1)
	}
	e.UseSlots(p)
	e.SlotSignals = func() slots.Signals { return sig }
	e.SlotAIMD = &slots.AIMD{N: 1}
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	p.Acquire(a, slots.Test) // 0.5 held; b needs 1.0 more: 1.5 > 1
	p.Acquire(b, slots.Test)
	e.Tick()
	for i := 0; i < 6; i++ {
		*now = now.Add(30 * time.Second)
		e.Tick()
	}
	if p.Budget() < 1.5 {
		t.Fatalf("budget %v", p.Budget())
	}
	if _, ok := p.Holding(b); !ok {
		t.Fatal("b was not admitted once the budget grew")
	}
	if s := p.Snapshot(); s.Budget == nil || s.BudgetWhy != "the host is calm and jobs wait" {
		t.Fatalf("%+v", s)
	}
}
