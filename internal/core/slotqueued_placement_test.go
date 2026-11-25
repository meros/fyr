package core

import (
	"testing"
	"time"
)

// A thread whose heavy job waits in the host's slot queue is Waiting in
// Moving without you (V0.slot-queued-waits); the placement invariant must
// accept that, not call the thread idle.
func TestSlotQueuedPlacementIsNotViolation(t *testing.T) {
	now := t0.Add(10 * time.Minute)
	th := &Thread{ID: "t1", ActivityAt: t0}
	th.SlotQueued = true
	a := ComputeWith(th, &View{}, now, nil)
	if a.State != Waiting || a.Section != WorkingSec {
		t.Fatalf("attention = %s/%s, want waiting/working", a.State, a.Section)
	}
	if vs := CheckThread(th, &View{}, now, nil); hasViolation(vs, InvThreadAttentionPlacement) {
		t.Fatalf("slot-queued thread flagged: %+v", vs)
	}
}
