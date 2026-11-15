package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// #33: a settle condition that holds while a run is live waits for the run
// to end; a working thread is never hidden as settled.
func TestSettleWaitsForTheRun(t *testing.T) {
	th := &Thread{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "nix:server", Kind: KindHost, Title: "Rebuild"}, ByAgent, ct0)
	RunStarted(th, ct0)
	ArmSettle(th, SettleCond{Kind: SettleChange, Change: c.ID, Phase: PhaseLanded}, ct0)
	UpdateChange(th, c, ChangeUpdate{Phase: PhaseLanded}, ByAgent, ct0)
	if th.Settle.At != nil {
		t.Fatal("the change landing settled a running thread")
	}
	if a := Compute(th, &View{}, ct0); a.State != Working {
		t.Fatalf("attention while the run is live: %+v", a)
	}
	end := ct0.Add(time.Minute)
	RunEnded(th, nil, end)
	if th.Settle.At == nil || !th.Settle.At.Equal(end) || len(th.Settle.When) != 0 {
		t.Fatalf("the run's end did not settle: %+v", th.Settle)
	}
}

// #33: the owner's and the agent's conditions are armed together, and any
// one that holds settles the thread. Pressing m keeps the agent's
// condition; Unsettle clears them all.
func TestSettleAnyArmedCondition(t *testing.T) {
	th, v := &Thread{}, &View{}
	PRObserved(th, PR{URL: "https://github.com/o/r/pull/1", State: "OPEN", Checks: ChecksPass}, ct0)
	doc, _, _ := OpenChange(th, ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Design doc"}, ByAgent, ct0)
	ArmSettle(th, SettleCond{Kind: SettleChange, Change: doc.ID, Phase: PhaseReview}, ct0)
	Registry["thread.settleWhenMerged"].Apply(th, v, nil, ct0)
	if len(th.Settle.When) != 2 || !SettlesWhenMerged(th) {
		t.Fatalf("m replaced the agent's condition: %+v", th.Settle.When)
	}
	if w := SettleWhenChange(th); w == nil || w.Change != doc.ID {
		t.Fatalf("the client's settleWhen: %+v", w)
	}

	un := Registry["thread.unsettle"]
	if r := un.Available(th, v, ct0); r != "" {
		t.Fatalf("unsettle with armed conditions: %q", r)
	}
	un.Apply(th, v, nil, ct0)
	if th.Settle.At != nil || len(th.Settle.When) != 0 {
		t.Fatalf("unsettle left %+v", th.Settle)
	}

	ArmSettle(th, SettleCond{Kind: SettleChange, Change: doc.ID, Phase: PhaseReview}, ct0)
	SettleWhenMerged(th, ct0)
	UpdateChange(th, doc, ChangeUpdate{Phase: PhaseReview}, ByAgent, ct0)
	if th.Settle.At == nil || len(th.Settle.When) != 0 {
		t.Fatalf("one condition holding did not settle: %+v", th.Settle)
	}
}

// #33: a run that fails drops "when done", and a failure is not hidden by
// a change condition that holds.
func TestSettleNotOnAFailedRun(t *testing.T) {
	th := &Thread{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "nix:server", Kind: KindHost, Title: "Rebuild"}, ByAgent, ct0)
	RunStarted(th, ct0)
	ArmSettle(th, SettleCond{Kind: SettleDone}, ct0)
	ArmSettle(th, SettleCond{Kind: SettleChange, Change: c.ID, Phase: PhaseLanded}, ct0)
	UpdateChange(th, c, ChangeUpdate{Phase: PhaseLanded}, ByAgent, ct0)
	RunEnded(th, &Failure{Class: "api", Message: "overloaded", At: ct0}, ct0)
	if th.Settle.At != nil || th.Settle.Has(SettleDone) || len(th.Settle.When) != 1 {
		t.Fatalf("after a failed run: %+v", th.Settle)
	}
	RunStarted(th, ct0)
	RunEnded(th, nil, ct0)
	if th.Settle.At == nil {
		t.Fatal("the retried run's end did not settle")
	}
}

// #33: a thread stored before #33 loads: settledAt, settleWhenDone and
// settleWhen become its Settle, liveView its Live.Enabled, and the stored
// project name is dropped.
func TestDecodeThreadBefore33(t *testing.T) {
	old := `{"id":"t_1","project":"fyr","cwd":"/src/fyr","running":true,"summary":"hi",
		"settledAt":"2026-11-14T08:00:00Z","settleWhenDone":true,"settleWhen":{"change":"c_1","phase":"live"},
		"liveView":true,"live":{"pageId":"p1","headline":"h"},
		"changes":[{"id":"c_1","key":"k","kind":"host","title":"Rebuild","phase":"review"}]}`
	th, err := DecodeThread([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 14, 8, 0, 0, 0, time.UTC)
	if th.Settle.At == nil || !th.Settle.At.Equal(at) {
		t.Fatalf("settledAt: %+v", th.Settle.At)
	}
	want := []SettleCond{{Kind: SettleDone}, {Kind: SettleChange, Change: "c_1", Phase: PhaseLive}}
	if len(th.Settle.When) != 2 || th.Settle.When[0] != want[0] || th.Settle.When[1] != want[1] {
		t.Fatalf("settle conditions: %+v", th.Settle.When)
	}
	if !th.ViewOn() || th.Live.PageID != "p1" || th.Live.Headline != "h" {
		t.Fatalf("live view: %+v", th.Live)
	}
	if !th.Running || th.Summary != "hi" {
		t.Fatalf("conversation: %+v", th.Conversation)
	}
	b, _ := json.Marshal(th)
	for _, k := range []string{`"project"`, `"settledAt"`, `"settleWhenDone"`, `"settleWhen"`, `"liveView"`} {
		if strings.Contains(string(b), k) {
			t.Fatalf("stored again with %s: %s", k, b)
		}
	}
	again, err := DecodeThread(b)
	if err != nil || again.Settle.At == nil || len(again.Settle.When) != 2 || !again.ViewOn() {
		t.Fatalf("the new shape does not round-trip: %v %+v", err, again)
	}
}
