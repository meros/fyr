package core

import "testing"

// V0.gate-hold-visible: a thread whose heavy command the gate holds back is
// Waiting in Moving without you, with the reason, not Finished; ending the
// hold puts it back.
func TestGateHoldWaits(t *testing.T) {
	th := &Thread{ID: "t", Title: "x"}
	v := &View{SeenAt: at(9)}
	if a := Compute(th, v, at(10)); a.Section != SeenSec {
		t.Fatalf("before: %+v", a)
	}
	th.GateHold = &GateHold{Since: at(10), Why: "memory is short on host-a; the build command runs when it recovers"}
	a := Compute(th, v, at(10))
	if a.State != Waiting || a.Section != WorkingSec || a.Reason != "Held: memory is short on host-a; the build command runs when it recovers" {
		t.Fatalf("held: %+v", a)
	}
	th.GateHold = nil
	if a := Compute(th, v, at(10)); a.Section != SeenSec {
		t.Fatalf("ended: %+v", a)
	}
}
