// Decision 0029 S4 (#188): the engine's tick steps the host's budget controller and sets the pool's budget, which
// admits what now fits.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/slots"
)

func TestS4TheTickRaisesTheBudgetOnACalmHost(t *testing.T) {
	e, _, now := bare(t)
	p := slots.New(1)
	p.Now = e.Now
	calm := 1.0
	free := 0.6
	// V0.163: a 4-core host; a holder needs 2 cores, b 4: 6 > 4 until the CPU budget grows past 150 %.
	sig := resource.Signals{Provider: "cgroups", Slowdown: &calm, Headroom: &free, Stall: resource.StallOK, CPU: 4, Memory: 8 << 30}
	p.Admit = func(head resource.Holder, held []resource.Holder) (bool, string, string) {
		c := make([]resource.Need, len(held))
		for i := range c {
			c[i] = resource.Need{CPU: 2, Mem: 1 << 30}
		}
		return guard.Decide(resource.DefaultPolicy, sig, p.Budget(), 1, c, resource.Need{CPU: 4, Mem: 1 << 30})
	}
	e.UseSlots(p)
	e.SlotSignals = func() resource.Signals { return sig }
	guardDeps(t, e)
	e.SlotAIMD = &guard.AIMD{N: 1}
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	p.Acquire(a, resource.Test) // 0.5 held; b needs 1.0 more: 1.5 > 1
	p.Acquire(b, resource.Test)
	e.Tick()
	for i := 0; i < 8; i++ {
		*now = now.Add(30 * time.Second)
		e.Tick()
	}
	if p.Budget().CPU < 1.5 || p.Budget().Mem != 1 {
		t.Fatalf("budget %v", p.Budget())
	}
	if _, ok := p.Holding(b); !ok {
		t.Fatal("b was not admitted once the budget grew")
	}
	if s := p.Snapshot(); s.Budget == nil || s.BudgetWhy != "the host is calm and a job waits for CPU: the CPU budget grows" {
		t.Fatalf("%+v", s)
	}
}
