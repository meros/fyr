package core

import "testing"

// V0.settled-self-turn: a settled thread whose CLI still lives on for its
// background work can start a turn by itself (a task notice wakes it). That
// run is real work, so it revives the thread; it must not leave it settled
// (thread-settled-run-started).
func TestSelfStartedTurnRevivesSettledThread(t *testing.T) {
	now := at(1)
	th := &Thread{ID: "t1", Settle: Settle{At: ptr(at(0))}}
	in := Input{Source: Observer(ObsProcess), Kind: KindRunStarted, Thread: "t1"}
	after, v, res, err := Reduce(th, &View{User: Owner, Entity: "t1"}, in, now)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Running || after.Settle.At != nil {
		t.Fatalf("running=%v settle=%+v", after.Running, after.Settle)
	}
	if vs := CheckTransition(th, v, after, v, in, res, now, nil); len(vs) > 0 {
		t.Fatalf("%+v", vs)
	}
}
