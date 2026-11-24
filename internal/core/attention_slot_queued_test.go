package core

import "testing"

// V0.slot-queued-waits: a thread whose heavy job waits for room on the host
// moves without the owner, even when its PR is ready to merge.
func TestSlotQueuedWaitsAheadOfReadyMerge(t *testing.T) {
	th := needThread(t, ready(headA))
	v := &View{SeenAt: at(9)}
	if a := Compute(th, v, at(10)); a.State != Blocked || a.Section != NeedsYou {
		t.Fatalf("ready PR: %+v", a)
	}
	th.SlotQueued = true
	if a := Compute(th, v, at(10)); a.State != Waiting || a.Section != WorkingSec || a.Reason != QueuedText {
		t.Fatalf("queued: %+v", a)
	}
	th.SlotQueued = false
	if a := Compute(th, v, at(10)); a.State != Blocked {
		t.Fatalf("admitted: %+v", a)
	}
}
