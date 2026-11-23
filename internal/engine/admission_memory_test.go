// V0.admission-memory: the memory budget grows on a calm host only while the budget holds the head back, not
// while memory outside it (the owner's browser) does: a larger budget would not admit the head, only admit up to
// twice the host once that memory is freed.
package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/slots"
)

func memoryWait(t *testing.T, headroom float64, held, head uint64) (*slots.Pool, resource.Snapshot) {
	t.Helper()
	e, _, now := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	calm := 1.0
	sig := resource.Signals{Provider: "cgroups", Slowdown: &calm, Headroom: &headroom, Stall: resource.StallOK, CPU: 8, Memory: 8 << 30}
	p.Admit = func(h resource.Holder, hs []resource.Holder) (bool, string, string) {
		c := make([]resource.Need, len(hs))
		for i := range c {
			c[i] = resource.Need{CPU: 1, Mem: held}
		}
		return guard.Decide(resource.DefaultPolicy, sig, p.Budget(), 2, c, resource.Need{CPU: 1, Mem: head})
	}
	e.UseSlots(p)
	e.SlotSignals = func() resource.Signals { return sig }
	guardDeps(t, e)
	e.SlotAIMD = &guard.AIMD{N: 2}
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	p.Acquire(a, resource.Test)
	p.Acquire(b, resource.Test)
	e.Tick()
	for i := 0; i < 8; i++ {
		*now = now.Add(30 * time.Second)
		e.Tick()
	}
	return p, p.Snapshot()
}

func TestAdmissionMemoryHeadroomBoundHeadKeepsTheMemoryBudget(t *testing.T) {
	// 8 GB for the agents, 30 % of it free (the rest is the owner's browser): the budget has 7 GB free, the
	// host 1.6 GB above the reserve; the head needs 3 GB.
	p, s := memoryWait(t, 0.3, 1<<30, 3<<30)
	if p.Budget().Mem != 1 {
		t.Fatalf("the memory budget moved while the host's headroom held the head: %+v", p.Budget())
	}
	if len(s.Waiting) != 1 || s.Binding != resource.ResMem {
		t.Fatalf("the front reads memory: binding %q, waiting %d", s.Binding, len(s.Waiting))
	}
}

func TestAdmissionMemoryBudgetBoundHeadGrowsTheMemoryBudget(t *testing.T) {
	// 90 % free: the host has 6.4 GB above the reserve, the budget 2 GB beside a 6 GB holder; the head needs 3.
	p, _ := memoryWait(t, 0.9, 6<<30, 3<<30)
	if p.Budget().Mem <= 1 {
		t.Fatalf("the memory budget did not grow while it held the head: %+v", p.Budget())
	}
}
