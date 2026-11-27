package core

import (
	"strings"
	"testing"
)

// V0.freeze-visible: a job the memory guard froze is the thread's claim,
// above its running tasks, until the freeze ends.
func TestFrozenWorkWaits(t *testing.T) {
	now := at(10)
	v := &View{SeenAt: at(9)}
	const why = "memory is short on host-a; it resumes when memory recovers"
	const want = "Frozen by Fyr: " + why
	idle := func() *Thread {
		th := &Thread{ID: "t", Title: "x"}
		th.Tasks = []Task{{ID: "b1", Status: TaskRunning, Background: true, Type: "local_bash", Description: "Run the smoke check"}}
		return th
	}
	reduce := func(th *Thread, in Input) *Thread {
		t.Helper()
		th, _, _, err := Reduce(th, v, in, now)
		if err != nil {
			t.Fatalf("%s: %v", in.Kind, err)
		}
		return th
	}
	freeze := func(th *Thread, key string) *Thread {
		return reduce(th, Input{Source: Observer(ObsHost), Kind: KindFrozen, Thread: th.ID, Args: FrozenArgs{Key: key, Why: why}})
	}

	th := idle()
	if a := Compute(th, v, now); a.Reason != "Running in background: Run the smoke check" {
		t.Fatalf("before: %+v", a)
	}
	th = freeze(th, "work")
	a := Compute(th, v, now)
	if a.State != Waiting || a.Section != WorkingSec || a.Reason != want {
		t.Fatalf("frozen: %+v", a)
	}
	if !WorkGoingAt(th, now) || NeedsOwnerAt(th, now) {
		t.Fatalf("frozen: going %v needs %v", WorkGoingAt(th, now), NeedsOwnerAt(th, now))
	}

	// Two freezes (the slotted job and undeclared work) end one by one.
	th = freeze(th, "job")
	th = reduce(th, Input{Source: Observer(ObsHost), Kind: KindFrozen, Thread: th.ID, Args: FrozenArgs{Key: "work"}})
	if a := Compute(th, v, now); a.Reason != want {
		t.Fatalf("one of two thawed: %+v", a)
	}
	th = reduce(th, Input{Source: Observer(ObsHost), Kind: KindFrozen, Thread: th.ID, Args: FrozenArgs{Key: "job"}})
	if th.Frozen != nil || Compute(th, v, now).Reason != "Running in background: Run the smoke check" {
		t.Fatalf("thawed: %+v %+v", th.Frozen, Compute(th, v, now))
	}

	// A kill of the frozen work ends the freeze.
	th = freeze(idle(), "work")
	th = reduce(th, Input{Source: Observer(ObsHost), Kind: KindSlotPause, Thread: th.ID, Args: map[string]any{"killed": "yarn smoke", "work": true}})
	if th.Frozen != nil {
		t.Fatalf("killed: %+v", th.Frozen)
	}
	// V0.oomd-frozen-work: so does the guard's kill before oomd, and the
	// agent is told what it was and why.
	th = freeze(idle(), "work")
	th = reduce(th, Input{Source: Observer(ObsHost), Kind: KindSlotPause, Thread: th.ID, Args: map[string]any{"ended": "yarn check", "work": true}})
	if th.Frozen != nil {
		t.Fatalf("ended: %+v", th.Frozen)
	}
	if ns := slotNotices(Reduced{Events: []Event{{EventSlot, map[string]any{"ended": "yarn check", "work": true}}}}); len(ns) != 1 || !strings.Contains(ns[0].What, "killed your heavy work (`yarn check`)") || !strings.Contains(ns[0].What, "oomd") {
		t.Fatalf("notice %+v", ns)
	}

	// The last task's end ends it: nothing of the thread runs to be frozen.
	th = freeze(idle(), "work")
	th = reduce(th, Input{Source: Observer(ObsProcess), Kind: KindTask, Thread: th.ID, Args: TaskArgs{Task: Task{ID: "b1", Status: TaskStopped}}})
	if th.Frozen != nil {
		t.Fatalf("task ended: %+v", th.Frozen)
	}

	// A lost process, a restart and a settle end it.
	th = freeze(idle(), "work")
	ProcessLost(th, "process", "gone", now)
	if th.Frozen != nil {
		t.Fatalf("process lost: %+v", th.Frozen)
	}
	th = freeze(idle(), "work")
	settleNow(th, now)
	if th.Frozen != nil {
		t.Fatalf("settled: %+v", th.Frozen)
	}
	th = freeze(idle(), "work")
	th = reduce(th, Input{Source: Observer(ObsHost), Kind: KindHostRestarted, Thread: th.ID, Args: RestartArgs{}})
	if th.Frozen != nil {
		t.Fatalf("restart: %+v", th.Frozen)
	}

	// A settled thread takes no freeze.
	th = idle()
	th.Settle.At = &now
	th = freeze(th, "work")
	if th.Frozen != nil {
		t.Fatalf("asleep: %+v", th.Frozen)
	}
}
