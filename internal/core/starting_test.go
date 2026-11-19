package core

import (
	"testing"
	"time"
)

// V0.starting-moves: a thread whose agent is starting moves without the
// owner; it is not finished until the agent answers.
func TestStartingMoves(t *testing.T) {
	now := time.Now()
	th := &Thread{ID: "t"}
	if _, err := Registry["thread.create"].Apply(th, &View{}, Args{"text": "hi", "cwd": "/tmp"}, now); err != nil {
		t.Fatal(err)
	}
	if a := Compute(th, nil, now); a.State != Working || a.Section != WorkingSec || a.Reason != "Starting" {
		t.Fatalf("starting: %+v", a)
	}
	RunStarted(th, now)
	if th.Starting != nil {
		t.Fatal("the run started and the thread still starts")
	}
	RunEnded(th, nil, now)
	if a := Compute(th, nil, now); a.State != Done {
		t.Fatalf("after the run: %+v", a)
	}

	// A start that fails ends the start.
	if _, err := Registry["thread.send"].Apply(th, &View{}, Args{"text": "again"}, now); err != nil {
		t.Fatal(err)
	}
	if th.Starting == nil {
		t.Fatal("a message to an idle thread does not start it")
	}
	RunEnded(th, &Failure{Class: "process", Message: "no", At: now}, now)
	if th.Starting != nil {
		t.Fatal("a failed start still starts")
	}

	// A process lost during the start fails the thread.
	th = &Thread{ID: "t"}
	th.Starting = &now
	ProcessLost(th, "process", "gone", now)
	if th.Starting != nil || th.Failure == nil {
		t.Fatalf("lost during the start: starting=%v failure=%+v", th.Starting, th.Failure)
	}

	// A host that dies during the start leaves no start behind; without the
	// process the message is lost, so the thread fails.
	for _, kept := range []bool{false, true} {
		th := &Thread{ID: "t"}
		th.Starting = &now
		if _, err := reduceRestarted(Rec{T: th}, Input{Args: RestartArgs{Kept: kept}}, now); err != nil {
			t.Fatal(err)
		}
		if th.Starting != nil {
			t.Fatalf("kept=%v: the start outlived the host", kept)
		}
		if !kept && (th.Failure == nil || th.Failure.Class != "restart") {
			t.Fatalf("a start lost with the host is no failure: %+v", th.Failure)
		}
	}
}
