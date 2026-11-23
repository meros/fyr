// V0.admission-memory: the wait tells the memory budget (resource.ResMem) from the host's real headroom (resource.ResHeadroom), the
// running jobs' growth comes off the headroom, and the AIMD controller grows the memory budget only for resource.ResMem.
package guard

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

func TestAdmissionMemoryHeadroomIsNotTheBudget(t *testing.T) {
	held := []resource.Need{{CPU: 1, Mem: 2 * gb}}
	// host6: 13 GB, 60 % free: 6.5 GB above the reserve, 11 GB of budget. 4 GB fits...
	if ok, why, _ := Decide(resource.DefaultPolicy, host6, resource.Full, 2, held, resource.Need{CPU: 1, Mem: 4 * gb}); !ok {
		t.Fatalf("%q", why)
	}
	// ...but not beside a holder that may still grow 3 GB: the headroom binds, and the wait says why.
	grow := host6
	grow.Growth = 3 * gb
	ok, why, res := Decide(resource.DefaultPolicy, grow, resource.Full, 2, held, resource.Need{CPU: 1, Mem: 4 * gb})
	if ok || res != resource.ResHeadroom || !strings.Contains(why, "the running jobs may still grow 3.2 GB") {
		t.Fatalf("%v %q %q", ok, why, res)
	}
	// A cut budget binds before the headroom: resource.ResMem.
	if ok, _, res := Decide(resource.DefaultPolicy, host6, resource.Factors{CPU: 1, Mem: 0.5}, 2, held, resource.Need{CPU: 1, Mem: 5 * gb}); ok || res != resource.ResMem {
		t.Fatalf("budget: %v %q", ok, res)
	}
}

func TestAdmissionMemoryAIMDGrowsOnlyABindingBudget(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	calm := resource.Signals{Provider: "cgroups", Slowdown: f(1.1), Headroom: f(0.3), Stall: resource.StallOK}
	a := &AIMD{N: 2}
	a.Step(t0, calm, true, time.Time{}, resource.ResHeadroom)
	for i := 1; i <= 8; i++ {
		if b, _ := a.Step(t0.Add(time.Duration(30*i)*time.Second), calm, true, time.Time{}, resource.ResHeadroom); b != resource.Full {
			t.Fatalf("the headroom bound and a budget moved: %+v", b)
		}
	}
	if b, _ := a.Step(t0.Add(300*time.Second), calm, true, time.Time{}, resource.ResMem); b.Mem != 1+AIMDUp || b.CPU != 1 {
		t.Fatalf("the budget bound and did not grow: %+v", b)
	}
}
