// Decision 0029 S4 (#188), V0.163 per resource: memory pressure cuts the memory budget, a CPU stall the CPU budget,
// and the resource that holds the head back grows on a calm host; bounded 1/N–2; every 30 s at most.
package slots

import (
	"strings"
	"testing"
	"time"
)

func TestS4BudgetAdaptsPerResource(t *testing.T) {
	t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	a := &AIMD{N: 2}
	calm := Signals{Provider: "cgroups", Slowdown: f(1.1), Headroom: f(0.5), Stall: StallOK}
	if b, _ := a.Step(t0, calm, true, time.Time{}, ResCPU); b != Full {
		t.Fatalf("starts full: %+v", b)
	}
	b, why := a.Step(t0.Add(30*time.Second), calm, true, time.Time{}, ResCPU)
	if b != (Factors{CPU: 1.125, Mem: 1}) || !strings.Contains(why, "CPU budget grows") {
		t.Fatalf("CPU up: %+v %q", b, why)
	}
	b, why = a.Step(t0.Add(60*time.Second), calm, true, time.Time{}, ResMem)
	if b != (Factors{CPU: 1.125, Mem: 1.125}) || !strings.Contains(why, "memory budget grows") {
		t.Fatalf("memory up: %+v %q", b, why)
	}
	if b, _ := a.Step(t0.Add(70*time.Second), calm, true, time.Time{}, ResCPU); b.CPU != 1.125 {
		t.Fatalf("stepped within 30 s: %+v", b)
	}
	if b, _ := a.Step(t0.Add(90*time.Second), calm, true, t0.Add(80*time.Second), ResCPU); b.CPU != 1.125 {
		t.Fatalf("up although a job was admitted within 60 s: %+v", b)
	}
	if b, _ := a.Step(t0.Add(120*time.Second), calm, false, time.Time{}, ResCPU); b.CPU != 1.125 {
		t.Fatalf("up with nobody waiting: %+v", b)
	}
	for i := 0; i < 20; i++ {
		b, _ = a.Step(t0.Add(time.Duration(150+30*i)*time.Second), calm, true, time.Time{}, ResCPU)
	}
	if b.CPU != 2 || b.Mem != 1.125 {
		t.Fatalf("not capped at 2: %+v", b)
	}
	// A CPU stall for 60 s cuts the CPU budget only.
	slow := Signals{Provider: "cgroups", Slowdown: f(3.5), Headroom: f(0.5), Stall: StallOK}
	t1 := t0.Add(time.Hour)
	a.Step(t1, slow, true, time.Time{}, ResCPU)
	if b, _ := a.Step(t1.Add(30*time.Second), slow, true, time.Time{}, ResCPU); b.CPU != 2 {
		t.Fatalf("cut before 60 s: %+v", b)
	}
	b, why = a.Step(t1.Add(60*time.Second), slow, true, time.Time{}, ResCPU)
	if b != (Factors{CPU: 1.5, Mem: 1.125}) || !strings.Contains(why, "3.5×") {
		t.Fatalf("CPU cut: %+v %q", b, why)
	}
	// Memory short cuts the memory budget only, never under 1/N.
	short := Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.05), Stall: StallOK}
	for i := 0; i < 10; i++ {
		b, why = a.Step(t1.Add(time.Duration(120+30*i)*time.Second), short, true, time.Time{}, ResCPU)
	}
	if b.Mem != 0.5 || b.CPU != 1.5 || !strings.Contains(why, "memory budget shrinks") {
		t.Fatalf("memory cut: %+v %q", b, why)
	}
	if b, _ := (&AIMD{N: 2}).Step(t0, Signals{}, true, time.Time{}, ""); b != Full {
		t.Fatalf("no provider: %+v", b)
	}
}

func TestS4PoolBudgetIsSettableAndShown(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	p := New(2)
	p.Now = func() time.Time { return now }
	if p.Budget() != Full {
		t.Fatal("the budget starts full")
	}
	p.SetBudget(Factors{CPU: 1.25, Mem: 0.75}, "why")
	p.Acquire("a", Test)
	s := p.Snapshot()
	if p.Budget() != (Factors{CPU: 1.25, Mem: 0.75}) || s.Budget == nil || s.BudgetWhy != "why" {
		t.Fatalf("%+v", s)
	}
	if !p.LastGrant().Equal(now) {
		t.Fatalf("last grant %v", p.LastGrant())
	}
}

// V0.load-tuning: the host stalling while the agents do not holds the budget
// where it is; the agents' own stall cuts it; without that reading the host's counts.
func TestMemoryBudgetFollowsTheAgentsOwnStall(t *testing.T) {
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	step := func(s Signals) Factors {
		a := &AIMD{N: 2}
		a.Step(t0, s, true, time.Time{}, ResMem)
		b, _ := a.Step(t0.Add(30*time.Second), s, true, time.Time{}, ResMem)
		return b
	}
	base := Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.8), Stall: StallCritical}
	host := base
	host.AgentsStall = StallOK
	if b := step(host); b != Full {
		t.Fatalf("the host's stall moved the budget: %+v", b)
	}
	own := base
	own.AgentsStall = StallCritical
	if b := step(own); b.Mem != 0.75 {
		t.Fatalf("the agents' stall did not cut: %+v", b)
	}
	if b := step(base); b.Mem != 0.75 {
		t.Fatalf("without the agents' reading the host's stall counts: %+v", b)
	}
}
