package core

import "testing"

// V0.waiting-for-command: a run request whose command runs, with the agent's
// turn over, is Waiting in Moving without you, not Finished.
func TestRunningRequestWaits(t *testing.T) {
	th := &Thread{ID: "t", Title: "x"}
	th.Offers = []ActionSpec{{ID: "r1", Request: true, Why: "copy the database"}}
	v := &View{SeenAt: at(9)}
	if a := Compute(th, v, at(10)); a.Section != NeedsYou {
		t.Fatalf("pending: %+v", a)
	}
	th.Runs = []ActionRun{{Action: "r1", On: OnThread, Running: true}}
	a := Compute(th, v, at(10))
	if a.State != Waiting || a.Section != WorkingSec || a.Reason != "Waiting for command: copy the database" {
		t.Fatalf("running: %+v", a)
	}
	th.Runs[0].Running = false
	if a := Compute(th, v, at(10)); a.Section != SeenSec {
		t.Fatalf("ended: %+v", a)
	}
}

// V0.invariant-running-command-wait: the placement invariant knows the
// running-command wait, so a thread that waits for its command is no violation.
func TestPlacementInvariantRunningCommandWait(t *testing.T) {
	th := &Thread{ID: "t", Title: "x"}
	th.Offers = []ActionSpec{{ID: "r1", Request: true, Why: "copy the database"}}
	th.Runs = []ActionRun{{Action: "r1", On: OnThread, Running: true}}
	v := &View{SeenAt: at(9)}
	for _, vi := range CheckThread(th, v, at(10), nil) {
		if vi.ID == InvThreadAttentionPlacement {
			t.Fatalf("running command wait flagged: %+v", vi)
		}
	}
}
