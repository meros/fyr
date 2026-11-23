// V0.163: the owner and the agents see heavy jobs as percentages of what the host gives its agents, CPU and memory,
// not slots.
package slots

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

func TestV163SnapshotSpeaksPercent(t *testing.T) {
	p := New(2)
	p.Host = func() resource.Signals { return resource.Signals{Provider: "cgroups", CPU: 6, Memory: 13 * gb} }
	p.Cost = func(h resource.Holder) resource.Need {
		switch h.Owner {
		case "cpu":
			return resource.Need{CPU: 3, Mem: 2 * gb}
		case "mem":
			return resource.Need{CPU: 0.6, Mem: 6 * gb}
		}
		return resource.Need{CPU: 2.7, Mem: gb}
	}
	p.Admit = func(head resource.Holder, held []resource.Holder) (bool, string, string) {
		if len(held) < 2 {
			return true, "", ""
		}
		return false, "needs ≈ 45 % CPU, 40 % free", ResCPU
	}
	p.Acquire("cpu", resource.Test)
	p.Acquire("mem", resource.Build)
	p.Acquire("next", resource.Test)
	s := p.Snapshot()
	if *s.CPUPct != 60 || *s.MemPct != 62 || *s.CPUBudgetPct != 100 || *s.CPUFreePct != 40 || *s.MemFreePct != 38 {
		t.Fatalf("host %+v", s)
	}
	if s.UsedPct == nil || *s.UsedPct != 62 || s.Tight != ResMem || s.Binding != ResCPU || *s.NeedPct != 45 {
		t.Fatalf("tight %+v", s)
	}
	if s.Held[0].CPUPct != 50 || s.Held[0].MemPct != 15 || s.Held[0].Pct != 50 || s.Waiting[0].Pct != 45 {
		t.Fatalf("jobs %+v %+v", s.Held, s.Waiting)
	}
	p.SetBudget(resource.Factors{CPU: 1.25, Mem: 1}, "the host is calm and a job waits for CPU: the CPU budget grows")
	if s := p.Snapshot(); *s.CPUBudgetPct != 125 || *s.CPUFreePct != 65 || s.Budget == nil {
		t.Fatalf("after AIMD %+v", s)
	}
}

func TestV163ACountedHostSpeaksPercentToo(t *testing.T) {
	p := New(4)
	p.Acquire("a", resource.Test)
	s := p.Snapshot()
	if s.UsedPct == nil || *s.UsedPct != 25 || s.Held[0].Pct != 25 || s.CPUPct != nil {
		t.Fatalf("%+v", s)
	}
}

func TestV163WhyNamesTheResource(t *testing.T) {
	_, why, res := Decide(resource.DefaultPolicy, host6, resource.Full, 2, []resource.Need{{CPU: 4, Mem: gb}}, resource.Need{CPU: 2.7, Mem: gb})
	if why != "needs ≈ 45 % CPU, 33 % free" || res != ResCPU {
		t.Fatalf("%q %q", why, res)
	}
	_, why, _ = Decide(resource.DefaultPolicy, resource.Signals{}, resource.Full, 2, []resource.Need{{}, {}}, resource.Need{})
	if !strings.Contains(why, "2 of 2 heavy jobs run") {
		t.Fatalf("counted %q", why)
	}
}
