// V0.budget-restart: the budget outlives a restart of fyrd when it is young.
package slots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard/resource"
)

func TestBudgetKeptAcrossARestart(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 9, 50, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "slot-budget.json")
	short := resource.Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.05), Stall: resource.StallOK}
	a := &AIMD{N: 2, Path: path}
	var b resource.Factors
	for i := 0; i < 5; i++ {
		b, _ = a.Step(t0.Add(time.Duration(30*i)*time.Second), short, true, time.Time{}, ResMem)
	}
	if b.Mem != 0.5 {
		t.Fatalf("memory not cut: %+v", b)
	}
	// The deploy restarts fyrd 10 s later: the new controller starts where
	// the old one was, not at the full budget.
	b, why := (&AIMD{N: 2, Path: path}).Step(t0.Add(130*time.Second), short, true, time.Time{}, ResMem)
	if b != (resource.Factors{CPU: 1, Mem: 0.5}) || !strings.Contains(why, "kept across a restart") {
		t.Fatalf("restart forgot the budget: %+v %q", b, why)
	}
	// A budget older than AIMDKeep says nothing of the host now.
	if b, _ := (&AIMD{N: 2, Path: path}).Step(t0.Add(time.Hour), short, true, time.Time{}, ResMem); b != resource.Full {
		t.Fatalf("a stale budget was taken: %+v", b)
	}
	// A budget the host's N no longer allows is bounded; junk is ignored.
	if b, _ := (&AIMD{N: 4, Path: path}).Step(t0.Add(130*time.Second), short, true, time.Time{}, ResMem); b.Mem != 0.5 {
		t.Fatalf("bounded wrong: %+v", b)
	}
	if b, _ := (&AIMD{N: 1, Path: path}).Step(t0.Add(130*time.Second), short, true, time.Time{}, ResMem); b.Mem != 1 {
		t.Fatalf("not raised to 1/N: %+v", b)
	}
	os.WriteFile(path, []byte("{"), 0o600)
	if b, _ := (&AIMD{N: 2, Path: path}).Step(t0, short, true, time.Time{}, ResMem); b != resource.Full {
		t.Fatalf("junk taken: %+v", b)
	}
}

func TestBudgetKeptIsRewrittenWhileItHolds(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "slot-budget.json")
	short := resource.Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.05), Stall: resource.StallOK}
	still := resource.Signals{Provider: "cgroups", Slowdown: f(2), Headroom: f(0.5), Stall: resource.StallOK}
	a := &AIMD{N: 2, Path: path}
	a.Step(t0, short, true, time.Time{}, ResMem)
	a.Step(t0.Add(30*time.Second), short, true, time.Time{}, ResMem)
	// An hour on a budget that does not move: still young when fyrd restarts.
	for i := 2; i < 120; i++ {
		a.Step(t0.Add(time.Duration(30*i)*time.Second), still, false, time.Time{}, "")
	}
	b, _ := (&AIMD{N: 2, Path: path}).Step(t0.Add(time.Hour), short, true, time.Time{}, ResMem)
	if b.Mem != 0.75 {
		t.Fatalf("an unmoving budget aged out: %+v", b)
	}
}

// V0.restart-why: a budget kept across two restarts in a row says so once.
func TestBudgetKeptTwiceSaysItOnce(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 9, 50, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "slot-budget.json")
	short := resource.Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.05), Stall: resource.StallOK}
	a := &AIMD{N: 2, Path: path}
	for i := 0; i < 5; i++ {
		a.Step(t0.Add(time.Duration(30*i)*time.Second), short, true, time.Time{}, ResMem)
	}
	// The next fyrd holds the kept budget and writes it again while it holds;
	// the one after reads what it wrote.
	calm := resource.Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.5), Stall: resource.StallOK}
	b := &AIMD{N: 2, Path: path}
	b.Step(t0.Add(130*time.Second), calm, false, time.Time{}, "")
	b.Step(t0.Add(130*time.Second+AIMDWrite+time.Second), calm, false, time.Time{}, "")
	_, why := (&AIMD{N: 2, Path: path}).Step(t0.Add(140*time.Second+AIMDWrite), calm, false, time.Time{}, "")
	if n := strings.Count(why, "kept across a restart"); n != 1 {
		t.Fatalf("said %d times: %q", n, why)
	}
}
