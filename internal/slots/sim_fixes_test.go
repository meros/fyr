// V0.sim-fixes (2): while memory is short a cheap job backfills unless the
// stall is critical (decision 0029 section 4: a job of CheapShare or less
// barely touches the head's room).
package slots

import (
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

// The huge-job row of V0.load-sim: a 0.5 GB, 1-core job waited 239 min
// behind a job larger than the agents' budget, because a short headroom held
// every job.
func TestSimFixesACheapJobBackfillsWhileMemoryIsShort(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	s := resource.Signals{Provider: "cgroups", CPU: 8, Memory: 12e9, Slowdown: f(1.0), Headroom: f(0.03), Stall: resource.StallOK, AgentsStall: resource.StallElevated}
	held := []resource.Need{{CPU: 4, Mem: 11e9}}
	cheap, dear := resource.Need{CPU: 1, Mem: 0.5e9}, resource.Need{CPU: 2, Mem: 2e9}
	if ok, why, _ := Decide(resource.DefaultPolicy, s, resource.Full, 2, held, cheap); !ok {
		t.Fatalf("the cheap job waits while memory is short and does not stall critically: %s", why)
	}
	if ok, _, res := Decide(resource.DefaultPolicy, s, resource.Full, 2, held, dear); ok || res != ResHeadroom {
		t.Fatalf("a job of 2 cores and 2 GB backfilled: ok=%t res=%s", ok, res)
	}
	s.AgentsStall = resource.StallCritical
	if ok, _, _ := Decide(resource.DefaultPolicy, s, resource.Full, 2, held, cheap); ok {
		t.Fatal("the cheap job started into a critical stall")
	}
	s.AgentsStall, s.SwapFull = resource.StallOK, true
	if ok, _, _ := Decide(resource.DefaultPolicy, s, resource.Full, 2, held, cheap); ok {
		t.Fatal("the cheap job started with the agents' swap full")
	}
	s.SwapFull, s.Slowdown = false, f(1.1)
	if ok, _, _ := Decide(resource.DefaultPolicy, s, resource.Full, 2, held, resource.Need{CPU: 2, Mem: 0.5e9}); ok {
		t.Fatal("2 of 8 cores is not cheap")
	}
}
