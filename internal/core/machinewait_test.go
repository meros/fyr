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

// V0.machine-wait-restart: the wait belongs to a start in the host's memory.
// A host that dies during the start leaves an idle thread with the wait
// saved, and no one is left to end it.
func TestMachineWaitEndsWithTheHost(t *testing.T) {
	for _, kept := range []bool{false, true} {
		th := &Thread{ID: "t"}
		th.MachineWait = &MachineWait{Since: time.Now(), Why: "memory pressure"}
		res, err := reduceRestarted(Rec{T: th}, Input{Args: RestartArgs{Kept: kept}}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if th.MachineWait != nil {
			t.Fatalf("kept=%v: the wait outlived the host", kept)
		}
		if len(res.Events) != 1 || res.Events[0].Type != "agent.machineWait" {
			t.Fatalf("kept=%v: events %+v", kept, res.Events)
		}
	}
}
