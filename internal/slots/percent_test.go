// V0.163: the owner and the agents see heavy jobs as a percentage of what the host gives its agents, not slots. A
// cost in units over N is a job's dominant share of the host (the tighter of CPU and memory), so 1 unit of N 2 is
// 50 %; the budget is a percentage of the same capacity (100 % = N, more or less after AIMD).
package slots

import (
	"strings"
	"testing"
)

func TestV163SnapshotSpeaksPercent(t *testing.T) {
	p := New(2)
	p.Cost = func(h Holder) float64 {
		switch h.Owner {
		case "busy":
			return 0.9
		case "next":
			return 0.9
		}
		return 0.1
	}
	p.Admit = func(head Holder, held []Holder) (bool, string) { return len(held) < 2, "x" }
	p.Acquire("busy", Test)
	p.Acquire("idle", E2E)
	p.Acquire("next", Test)
	s := p.Snapshot()
	if s.UsedPct == nil || *s.UsedPct != 50 || s.BudgetPct == nil || *s.BudgetPct != 100 || s.FreePct == nil || *s.FreePct != 50 {
		t.Fatalf("host %+v", s)
	}
	if s.Held[0].Pct != 45 || s.Held[1].Pct != 5 || s.Waiting[0].Pct != 45 || s.NeedPct == nil || *s.NeedPct != 45 {
		t.Fatalf("jobs %+v %+v %v", s.Held, s.Waiting, s.NeedPct)
	}
	p.SetBudget(2.5, "the host is calm and jobs wait")
	if s := p.Snapshot(); *s.BudgetPct != 125 || *s.FreePct != 75 {
		t.Fatalf("after AIMD %+v", s)
	}
}

func TestV163ACountedHostSpeaksPercentToo(t *testing.T) {
	p := New(4)
	p.Acquire("a", Test)
	s := p.Snapshot()
	if s.UsedPct == nil || *s.UsedPct != 25 || *s.BudgetPct != 100 || s.Held[0].Pct != 25 {
		t.Fatalf("%+v", s)
	}
}

func TestV163WhyInPercent(t *testing.T) {
	calm := Signals{Provider: "cgroups", Slowdown: f(1.1), Headroom: f(0.4), Stall: StallOK}
	_, why := Decide(DefaultPolicy, calm, 2, 2, []float64{1.4}, 0.9)
	if why != "needs ≈ 45 %, 30 % free" {
		t.Fatalf("%q", why)
	}
	_, why = Decide(DefaultPolicy, Signals{}, 2, 2, []float64{1, 1}, 1)
	if !strings.Contains(why, "2 of 2 heavy jobs run") {
		t.Fatalf("counted %q", why)
	}
}
