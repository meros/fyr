// V0.admission-memory: the wait tells the memory budget (ResMem) from the host's real headroom (ResHeadroom), the
// running jobs' growth comes off the headroom, and the AIMD controller grows the memory budget only for ResMem.
package slots

import (
	"strings"
	"testing"
	"time"
)

func TestAdmissionMemoryHeadroomIsNotTheBudget(t *testing.T) {
	held := []Need{{CPU: 1, Mem: 2 * gb}}
	// host6: 13 GB, 60 % free: 6.5 GB above the reserve, 11 GB of budget. 4 GB fits...
	if ok, why, _ := Decide(DefaultPolicy, host6, Full, 2, held, Need{CPU: 1, Mem: 4 * gb}); !ok {
		t.Fatalf("%q", why)
	}
	// ...but not beside a holder that may still grow 3 GB: the headroom binds, and the wait says why.
	grow := host6
	grow.Growth = 3 * gb
	ok, why, res := Decide(DefaultPolicy, grow, Full, 2, held, Need{CPU: 1, Mem: 4 * gb})
	if ok || res != ResHeadroom || !strings.Contains(why, "the running jobs may still grow 3.2 GB") {
		t.Fatalf("%v %q %q", ok, why, res)
	}
	// A cut budget binds before the headroom: ResMem.
	if ok, _, res := Decide(DefaultPolicy, host6, Factors{CPU: 1, Mem: 0.5}, 2, held, Need{CPU: 1, Mem: 5 * gb}); ok || res != ResMem {
		t.Fatalf("budget: %v %q", ok, res)
	}
}

func TestAdmissionMemoryAIMDGrowsOnlyABindingBudget(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	calm := Signals{Provider: "cgroups", Slowdown: f(1.1), Headroom: f(0.3), Stall: StallOK}
	a := &AIMD{N: 2}
	a.Step(t0, calm, true, time.Time{}, ResHeadroom)
	for i := 1; i <= 8; i++ {
		if b, _ := a.Step(t0.Add(time.Duration(30*i)*time.Second), calm, true, time.Time{}, ResHeadroom); b != Full {
			t.Fatalf("the headroom bound and a budget moved: %+v", b)
		}
	}
	if b, _ := a.Step(t0.Add(300*time.Second), calm, true, time.Time{}, ResMem); b.Mem != 1+AIMDUp || b.CPU != 1 {
		t.Fatalf("the budget bound and did not grow: %+v", b)
	}
}
