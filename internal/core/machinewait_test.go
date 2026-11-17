package core

import (
	"testing"
	"time"
)

// SPEC 0.5 V0.153: an idle thread whose start waits for the machine is
// Working as waiting, with the reason; a live process gone ends the wait.
func TestMachineWaitAttention(t *testing.T) {
	th := &Thread{}
	th.MachineWait = &MachineWait{Since: time.Now(), Why: "memory pressure"}
	a := Compute(th, nil, time.Now())
	if a.State != Waiting || a.Section != WorkingSec || a.Reason != "Waiting for the machine (memory pressure)" {
		t.Fatalf("attention %+v", a)
	}
	th.Running = true
	ProcessLost(th, "process", "gone", time.Now())
	if th.MachineWait != nil {
		t.Fatal("the wait outlived the process")
	}
}
