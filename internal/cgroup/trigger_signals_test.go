// V0.freeze-fast-class-name: a fresh PSI trigger is a critical agents' stall
// for everything that reads the sampler's signals (admission's hold, the
// guard), whatever avg10 says; the trigger is set at the critical level.
package cgroup

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

func TestTriggerMakesTheAgentsStallCriticalForTriggerFresh(t *testing.T) {
	if PressureTrigger != "full 200000 2000000" {
		t.Fatalf("the trigger %q is not the critical level (10 %% of 2 s)", PressureTrigger)
	}
	now := time.Date(2026, 11, 20, 7, 32, 12, 0, time.UTC)
	h := 0.64
	s := &Sampler{Now: func() time.Time { return now }}
	s.pub.Store(&Load{Provider: "cgroups", Headroom: &h, MemoryStall: "elevated", AgentsMemoryStall: "elevated"})
	if sig := s.Signals(); sig.AgentsStall != slots.StallElevated || sig.Triggered {
		t.Fatalf("no trigger yet: %+v", sig)
	}
	s.Triggered()
	sig := s.Signals()
	if sig.AgentsStall != slots.StallCritical || !sig.Triggered {
		t.Fatalf("a fresh trigger: %+v", sig)
	}
	if crit, why := sig.Critical(); !crit || why == "" {
		t.Fatalf("admission does not hold on a fresh trigger: %v %q", crit, why)
	}
	now = now.Add(slots.TriggerFresh)
	if sig := s.Signals(); sig.AgentsStall != slots.StallElevated || sig.Triggered {
		t.Fatalf("past TriggerFresh: %+v", sig)
	}
}
