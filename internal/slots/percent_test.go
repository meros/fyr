// V0.163: the owner and the agents see heavy jobs as percentages of what the host gives its agents, CPU and memory,
// not slots.
package slots

import (
	"strings"
	"testing"
)

func TestV163SnapshotSpeaksPercent(t *testing.T) {
	p := New(2)
	p.Host = func() Signals { return Signals{Provider: "cgroups", CPU: 6, Memory: 13 * gb} }
	p.Cost = func(h Holder) Need {
		switch h.Owner {
		case "cpu":
			return Need{CPU: 3, Mem: 2 * gb}
		case "mem":
			return Need{CPU: 0.6, Mem: 6 * gb}
		}
		return Need{CPU: 2.7, Mem: gb}
	}
	p.Admit = func(head Holder, held []Holder) (bool, string, string) {
		if len(held) < 2 {
			return true, "", ""
		}
		return false, "needs ≈ 45 % CPU, 40 % free", ResCPU
	}
	p.Acquire("cpu", Test)
	p.Acquire("mem", Build)
	p.Acquire("next", Test)
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
	p.SetBudget(Factors{CPU: 1.25, Mem: 1}, "the host is calm and a job waits for CPU: the CPU budget grows")
	if s := p.Snapshot(); *s.CPUBudgetPct != 125 || *s.CPUFreePct != 65 || s.Budget == nil {
		t.Fatalf("after AIMD %+v", s)
	}
}

func TestV163ACountedHostSpeaksPercentToo(t *testing.T) {
	p := New(4)
	p.Acquire("a", Test)
	s := p.Snapshot()
	if s.UsedPct == nil || *s.UsedPct != 25 || s.Held[0].Pct != 25 || s.CPUPct != nil {
		t.Fatalf("%+v", s)
	}
}

func TestV163WhyNamesTheResource(t *testing.T) {
	_, why, res := Decide(DefaultPolicy, host6, Full, 2, []Need{{CPU: 4, Mem: gb}}, Need{CPU: 2.7, Mem: gb})
	if why != "needs ≈ 45 % CPU, 33 % free" || res != ResCPU {
		t.Fatalf("%q %q", why, res)
	}
	_, why, _ = Decide(DefaultPolicy, Signals{}, Full, 2, []Need{{}, {}}, Need{})
	if !strings.Contains(why, "2 of 2 heavy jobs run") {
		t.Fatalf("counted %q", why)
	}
}
