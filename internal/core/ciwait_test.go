package core

import (
	"testing"
	"time"
)

// V0.ci-wait-moves: a thread whose run ended while CI runs is in Moving
// without you, with why, not in Finished; when CI is over it leaves Moving.
func TestCIWaitMoves(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	th, _ := heldAsk(t, now)
	if a := Compute(th, &View{}, now); a.State != Waiting || a.Section != WorkingSec || a.Reason != "Waiting for CI on #846: 2 of 5 checks done" {
		t.Fatalf("a held merge ask: %+v", a)
	}

	// an open PR with checks running and no ask
	th, _, _, err := Reduce(&Thread{ID: "t1"}, &View{}, askPR(now, nil), now)
	if err != nil {
		t.Fatal(err)
	}
	if a := Compute(th, &View{}, now); a.State != Waiting || a.Section != WorkingSec || a.Reason != "Waiting for CI on #846" {
		t.Fatalf("an open PR with pending checks: %+v", a)
	}

	// green: no longer moving (the PR is ready, so it is the owner's move)
	th, _, _, err = Reduce(th, &View{}, askPR(now, func(p *PR) { p.Checks = ChecksPass; p.MergeState = "CLEAN" }), now)
	if err != nil {
		t.Fatal(err)
	}
	if a := Compute(th, &View{}, now); a.Section == WorkingSec {
		t.Fatalf("CI over: %+v", a)
	}
}

// V0.ci-failed-still-running: a red check while others still run (a fix job,
// a slower check) is Waiting for CI in Moving, not Stopped on a failure; once
// every check is done the failure is the owner's.
func TestCIFailedStillRunning(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	red := func(done int) func(*PR) {
		return func(p *PR) {
			p.Checks, p.ChecksDone, p.ChecksTotal, p.FailedChecks = ChecksFail, done, 5, []string{"test"}
		}
	}
	th, _, _, err := Reduce(&Thread{ID: "t1"}, &View{}, askPR(now, red(4)), now)
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(StopAfter + time.Minute)
	th.Stopped = true
	if a := Compute(th, &View{}, later); a.State != Waiting || a.Section != WorkingSec || a.Reason != "Waiting for CI on #846: 4 of 5 checks done, 1 failed" {
		t.Fatalf("red with a check running: %+v", a)
	}
	if stopCheck(&Thread{Changes: th.Changes}) != nil {
		t.Fatal("a moving failure arms the stop clock")
	}
	th, _, _, err = Reduce(th, &View{}, askPR(later, red(5)), later)
	if err != nil {
		t.Fatal(err)
	}
	th.Stopped = true
	if a := Compute(th, &View{}, later); a.Section != NeedsYou || a.Group != GroupUnblock {
		t.Fatalf("red and all done: %+v", a)
	}
}
