// Decision 0029 slice S4 (#188): the budget adapts slowly to the host (AIMD): up by 0.25 while jobs wait on a calm
// host and nothing was admitted for 60 s, down by a quarter after 60 s of a 3× slowdown or with memory short; bounded
// 1.0–2·N; every 30 s at most.
package slots

import (
	"strings"
	"testing"
	"time"
)

func TestS4BudgetAdaptsSlowly(t *testing.T) {
	t0 := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	a := &AIMD{N: 2}
	calm := Signals{Provider: "cgroups", Slowdown: f(1.1), Headroom: f(0.5), Stall: StallOK}
	if b, _ := a.Step(t0, calm, true, time.Time{}); b != 2 {
		t.Fatalf("starts at N: %v", b)
	}
	b, why := a.Step(t0.Add(30*time.Second), calm, true, time.Time{})
	if b != 2.25 || !strings.Contains(why, "calm") {
		t.Fatalf("up: %v %q", b, why)
	}
	if b, _ := a.Step(t0.Add(40*time.Second), calm, true, time.Time{}); b != 2.25 {
		t.Fatalf("stepped within 30 s: %v", b)
	}
	if b, _ := a.Step(t0.Add(60*time.Second), calm, true, t0.Add(50*time.Second)); b != 2.25 {
		t.Fatalf("up although a job was admitted within 60 s: %v", b)
	}
	if b, _ := a.Step(t0.Add(90*time.Second), calm, false, time.Time{}); b != 2.25 {
		t.Fatalf("up with nobody waiting: %v", b)
	}
	for i := 0; i < 20; i++ {
		b, _ = a.Step(t0.Add(time.Duration(120+30*i)*time.Second), calm, true, time.Time{})
	}
	if b != 4 {
		t.Fatalf("not capped at 2·N: %v", b)
	}
	// A 3× slowdown for 60 s: down by a quarter (a short one does nothing).
	slow := Signals{Provider: "cgroups", Slowdown: f(3.5), Headroom: f(0.5), Stall: StallOK}
	t1 := t0.Add(time.Hour)
	a.Step(t1, slow, true, time.Time{})
	if b, _ := a.Step(t1.Add(30*time.Second), slow, true, time.Time{}); b != 4 {
		t.Fatalf("down before 60 s: %v", b)
	}
	b, why = a.Step(t1.Add(60*time.Second), slow, true, time.Time{})
	if b != 3 || !strings.Contains(why, "3.5×") {
		t.Fatalf("down: %v %q", b, why)
	}
	// Memory short: down at once, never under 1.0.
	short := Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.05), Stall: StallOK}
	for i := 0; i < 10; i++ {
		b, why = a.Step(t1.Add(time.Duration(120+30*i)*time.Second), short, true, time.Time{})
	}
	if b != 1 || !strings.Contains(why, "memory") {
		t.Fatalf("floor: %v %q", b, why)
	}
	// No provider: N, always.
	if b, _ := (&AIMD{N: 2}).Step(t0, Signals{}, true, time.Time{}); b != 2 {
		t.Fatalf("no provider: %v", b)
	}
}

func TestS4PoolBudgetIsSettableAndShown(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	p := New(2)
	p.Now = func() time.Time { return now }
	if p.Budget() != 2 {
		t.Fatal("budget starts at N")
	}
	p.SetBudget(2.5, "the host is calm and jobs wait")
	p.Acquire("a", Test)
	s := p.Snapshot()
	if p.Budget() != 2.5 || s.Budget == nil || *s.Budget != 2.5 || s.BudgetWhy != "the host is calm and jobs wait" {
		t.Fatalf("%+v", s)
	}
	if !p.LastGrant().Equal(now) {
		t.Fatalf("last grant %v", p.LastGrant())
	}
}
