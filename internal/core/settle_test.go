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
	SettleWhenReached(th, c.ID, PhaseLanded, ByAgent, ct0)
	rules(th, ct0)
	UpdateChange(th, c, ChangeUpdate{Phase: PhaseLanded}, ByAgent, ct0)
	rules(th, ct0)
	if th.Settle.At != nil {
		t.Fatal("the change landing settled a running thread")
	}
	if a := Compute(th, &View{}, ct0); a.State != Working {
		t.Fatalf("attention while the run is live: %+v", a)
	}
	end := ct0.Add(time.Minute)
	RunEnded(th, nil, end)
	rules(th, end)
	if th.Settle.At == nil || !th.Settle.At.Equal(end) || Settling(th) {
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
	SettleWhenReached(th, doc.ID, PhaseReview, ByAgent, ct0)
	Registry["thread.settleWhenMerged"].Apply(th, v, nil, ct0)
	rules(th, ct0)
	if len(RulesOf(th, PurposeSettle)) != 2 || !SettlesWhenMerged(th) {
		t.Fatalf("m replaced the agent's condition: %+v", th.Rules)
	}
	if w := SettleWhenChange(th); w == nil || w.Change != doc.ID {
		t.Fatalf("the client's settleWhen: %+v", w)
	}

	un := Registry["thread.unsettle"]
	if r := un.Available(th, v, ct0); r != "" {
		t.Fatalf("unsettle with armed conditions: %q", r)
	}
	un.Apply(th, v, nil, ct0)
	if th.Settle.At != nil || Settling(th) {
		t.Fatalf("unsettle left %+v", th.Settle)
	}

	SettleWhenReached(th, doc.ID, PhaseReview, ByAgent, ct0)
	SettleWhenMerged(th, ByOwner, ct0)
	rules(th, ct0)
	UpdateChange(th, doc, ChangeUpdate{Phase: PhaseReview}, ByAgent, ct0)
	rules(th, ct0)
	if th.Settle.At == nil || Settling(th) {
		t.Fatalf("one condition holding did not settle: %+v", th.Settle)
	}
}

// #33: a run that fails drops "when done", and a failure is not hidden by
// a change condition that holds.
func TestSettleNotOnAFailedRun(t *testing.T) {
	th := &Thread{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "nix:server", Kind: KindHost, Title: "Rebuild"}, ByAgent, ct0)
	RunStarted(th, ct0)
	SettleWhenDone(th, ByAgent, ct0)
	SettleWhenReached(th, c.ID, PhaseLanded, ByAgent, ct0)
	rules(th, ct0)
	UpdateChange(th, c, ChangeUpdate{Phase: PhaseLanded}, ByAgent, ct0)
	rules(th, ct0)
	RunEnded(th, &Failure{Class: "api", Message: "overloaded", At: ct0}, ct0)
	rules(th, ct0)
	if th.Settle.At != nil || SettlesWhenDone(th) || len(RulesOf(th, PurposeSettle)) != 1 {
		t.Fatalf("after a failed run: %+v %+v", th.Settle, th.Rules)
	}
	RunStarted(th, ct0)
	rules(th, ct0)
	RunEnded(th, nil, ct0)
	rules(th, ct0)
	if th.Settle.At == nil {
		t.Fatal("the retried run's end did not settle")
	}
}

// SPEC V0.remove-t3 (#68): a thread JSON carrying the removed t3Id and source
// fields loads without error, keeps its history identity, and saves without them.
func TestDecodeThreadDropsT3Fields(t *testing.T) {
	old := `{"id":"t_1","title":"Adopted thread","cwd":"/src/app","profile":"claudeAlt","sessionId":"sess-1","source":"t3","t3Id":"t3_abc"}`
	th := &Thread{}
	if err := json.Unmarshal([]byte(old), th); err != nil {
		t.Fatal(err)
	}
	if th.ID != "t_1" || th.Title != "Adopted thread" || th.Cwd != "/src/app" || th.SessionID != "sess-1" {
		t.Fatalf("adopted fields lost: %+v", th)
	}
	b, err := json.Marshal(th)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{`"t3Id"`, `"source"`} {
		if strings.Contains(string(b), k) {
			t.Fatalf("marshaled again with %s: %s", k, b)
		}
	}
}

// V0.203: "settle when done" waits for the work itself: the run, an open
// question and every open PR. It fires in the Reduce that ends the last one.
func TestSettleWhenDoneWaitsForTheWork(t *testing.T) {
	th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{SeenAt: ct0}
	clock := func() { step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0) }
	PRObserved(th, PR{URL: "https://github.com/o/r/pull/1", State: "OPEN", Checks: ChecksPass}, ct0)
	RunStarted(th, ct0)
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.settleWhenDone"}, ct0)
	if !SettlesWhenDone(th) || th.Settle.At != nil {
		t.Fatalf("armed %v, settled %v", SettlesWhenDone(th), th.Settle.At)
	}
	if r := Registry["thread.settleWhenDone"].Available(th, v, ct0); r != "Already set" {
		t.Fatalf("a second press: %q", r)
	}
	if r := Registry["thread.settle"].Available(th, v, ct0); r != "" {
		t.Fatalf("Settle must stay available: %q", r)
	}
	RunEnded(th, nil, ct0)
	clock()
	if th.Settle.At != nil {
		t.Fatal("settled with its PR open")
	}
	th.Changes[0].PR.State = "MERGED"
	clock()
	if th.Settle.At == nil || Settling(th) {
		t.Fatalf("the merge did not settle: %+v %+v", th.Settle, th.Rules)
	}
}

// V0.203: with nothing to wait for the action is off, with the reason.
func TestSettleWhenDoneNeedsSomethingToWaitFor(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	if r := Registry["thread.settleWhenDone"].Available(th, v, ct0); r != SettleDoneIdle {
		t.Fatalf("idle thread: %q", r)
	}
	th.Failure = &Failure{Class: "api", Message: "x", At: ct0}
	if r := Registry["thread.settleWhenDone"].Available(th, v, ct0); r != "The last run failed" {
		t.Fatalf("failed thread: %q", r)
	}
}
