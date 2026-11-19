package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// rules runs the scheduler on th in place, as Reduce does after each Input.
func rules(th *Thread, now time.Time) Result { return EvalRules(Rec{T: th, V: &View{}}, now) }

// snoozed is v with a snooze until `until` armed at `since`.
func snoozed(v View, until, since time.Time) View {
	Snooze(&v, until, since)
	return v
}

// step reduces one Input on th and v and keeps the result.
func step(t *testing.T, th *Thread, v *View, in Input, now time.Time) Result {
	t.Helper()
	nt, nv, res, err := Reduce(th, v, in, now)
	if err != nil {
		t.Fatalf("%s: %v", in.Kind, err)
	}
	*th, *v = *nt, *nv
	return res
}

func fired(res Result, rule string) int {
	n := 0
	for _, ev := range res.Events {
		if d, ok := ev.Data.(map[string]any); ok && ev.Type == "rule.fired" && d["rule"] == rule {
			n++
		}
	}
	return n
}

// #46: a Rule the agent arms fires once, when its Condition goes from false
// to true, records rule.fired and is then removed.
func TestAgentRuleFiresOnce(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/12"
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPending}}, ct0)
	c := th.Changes[0]
	res := step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify", Note: "merge it",
		When: When{Kind: WhenCondition, Change: c.Key, Type: CondChecks, Status: CondOK}}}, ct0)
	r := FindRule(th, "w_1")
	if r == nil || r.CreatedBy != ByAgent || r.Purpose != PurposeWatch || r.When.Change != c.ID || fired(res, "w_1") != 0 {
		t.Fatalf("armed: %+v %+v", r, res.Events)
	}
	res = step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPass}}, ct0)
	if fired(res, "w_1") != 1 || FindRule(th, "w_1") != nil {
		t.Fatalf("CI passing did not fire the watch once: %+v %+v", res.Events, th.Rules)
	}
	res = step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksFail}}, ct0)
	res2 := step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPass}}, ct0)
	if fired(res, "w_1")+fired(res2, "w_1") != 0 {
		t.Fatal("a disarmed watch fired again")
	}
}

// #46: the agent removes only its own Rules, and its Conditions are
// checked.
func TestAgentRuleRemoveAndCheck(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "settle", When: When{Kind: WhenRunEnded}}}, ct0)
	if _, _, _, err := Reduce(th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_2", Then: "notify", When: When{Kind: WhenIdle}}}, ct0); err == nil {
		t.Fatal("the agent armed a kind it may not use")
	}
	if _, _, _, err := Reduce(th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_2", Then: "notify", When: When{Kind: WhenReached, Change: "nope", Phase: PhaseLanded}}}, ct0); err == nil {
		t.Fatal("a watch on an unknown change was armed")
	}
	RunStarted(th, ct0)
	SettleWhenDone(th, ByOwner, ct0)
	if _, _, _, err := Reduce(th, v, Input{Source: FromAgent, Kind: KindRuleRemove, Args: settleDoneRule}, ct0); err == nil {
		t.Fatal("the agent removed the owner's Rule")
	}
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleRemove, Args: "w_1"}, ct0)
	if FindRule(th, "w_1") != nil || !SettlesWhenDone(th) {
		t.Fatalf("remove: %+v", th.Rules)
	}
}

// #46: Rules that make each other true stop after maxFirings in one Input.
func TestRuleLoopIsBounded(t *testing.T) {
	RegisterKind(&InputKind{Name: "test.flip", From: []string{ByRule}, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
		c := FindChange(r.T, "k")
		st := CondOK
		if mapArgs(in).Str("to") == "fail" {
			st = CondFail
		}
		setCondition(c, "x", st, "", ByObserved, now)
		return Result{}, nil
	}})
	defer delete(Kinds, "test.flip")
	th, v := &Thread{ID: "t_1"}, &View{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "k"}, ByAgent, ct0)
	setCondition(c, "x", CondPending, "", ByObserved, ct0)
	for _, x := range []struct{ id, on, to string }{{"a", "ok", "fail"}, {"b", "fail", "ok"}, {"c", "pending", "ok"}} {
		ArmRule(th, Rule{ID: x.id, Purpose: PurposeWatch, When: When{Kind: WhenCondition, Change: c.ID, Type: "x", Status: CondStatus(x.on)},
			Then: Then{Kind: "test.flip", Args: map[string]any{"to": x.to}}, After: AfterKeep, CreatedBy: BySystem}, ct0)
	}
	res := step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0)
	loop := false
	for _, ev := range res.Events {
		loop = loop || ev.Type == "rule.loop"
	}
	if n := fired(res, "a") + fired(res, "b") + fired(res, "c"); n != maxFirings || !loop {
		t.Fatalf("firings %d, loop %v", n, loop)
	}
}

// #46: a snooze is a Rule on the View; the clock's Input at its nextCheck
// fires it, which wakes the thread.
func TestSnoozeIsARule(t *testing.T) {
	th, v := &Thread{ID: "t_1", ActivityAt: ct0}, &View{SeenAt: ct0}
	until := ct0.Add(time.Hour).Format(time.RFC3339)
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.snooze", Args: Args{"until": until}}, ct0)
	if at := NextCheck(th, v, ct0); at == nil || !at.Equal(ct0.Add(time.Hour)) || Compute(th, v, ct0).State != Snoozed {
		t.Fatalf("next check %v, %+v", at, v.Rules)
	}
	step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(30*time.Minute))
	if u, _ := SnoozeOf(v); u == nil {
		t.Fatal("woke early")
	}
	res := step(t, th, v, Input{Source: Observer(ObsClock), Kind: KindClock}, ct0.Add(time.Hour))
	if u, _ := SnoozeOf(v); u != nil || fired(res, snoozeRule) != 1 || NextCheck(th, v, ct0.Add(time.Hour)) != nil {
		t.Fatalf("the snooze did not end: %+v %+v", v.Rules, res.Events)
	}
}

// #46, SPEC 7.4: the agent's watches are suspended while the thread is
// settled; on resume an event (an anchored kind) fires once, and so does a
// state that became true during the pause, as the agent's watches fire once
// (V0.settle-merge-fixes (3)).
func TestWatchSuspendedWhileSettled(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "k"}, ByAgent, ct0)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_state", Then: "notify", When: When{Kind: WhenReached, Change: c.ID, Phase: PhaseReview}}}, ct0)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_event", Then: "notify", When: When{Kind: WhenRunEnded}}}, ct0)
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.settle", Args: Args{"now": true}}, ct0)
	if r := FindRule(th, "w_state"); r == nil || r.Status != RuleSuspended || r.SuspendedBy[0] != SuspendSettle {
		t.Fatalf("not suspended: %+v", r)
	}
	res := step(t, th, v, Input{Source: FromAgent, Kind: KindChangeUpdate, Args: ChangeUpdateArgs{Change: c.ID, Update: ChangeUpdate{Phase: PhaseReview}}}, ct0)
	res2 := step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}, ct0)
	if fired(res, "w_state")+fired(res2, "w_event") != 0 {
		t.Fatal("a suspended watch fired")
	}
	res = step(t, th, v, Input{Source: FromOwner, Kind: "thread.unsettle"}, ct0)
	if fired(res, "w_state") != 1 || fired(res, "w_event") != 1 {
		t.Fatalf("resume: %+v", res.Events)
	}
	if len(RulesOf(th, PurposeWatch)) != 0 {
		t.Fatalf("a watch that fired is still armed: %+v", th.Rules)
	}
}

// #46: a thread and a view stored before Rules load: settle.when becomes
// settle Rules, a deploy watch that had not ended its deploy Rule (one that
// ended none), a change with a PR gets its built-in actions' Rule, and the
// view's snooze its snooze Rule.
func TestDecodeBeforeRules(t *testing.T) {
	old := `{"id":"t_1","updatedAt":"2026-11-15T08:00:00Z","running":true,
		"settle":{"when":[{"kind":"done"},{"kind":"change","change":"c_1","phase":"live"}]},
		"changes":[{"id":"c_1","key":"k","kind":"code","title":"Fix","phase":"landed",
			"pr":{"url":"https://github.com/o/r/pull/7","repo":"o/r","number":7,"state":"MERGED","checks":"pass","mergeSha":"m1"},
			"deployWatch":{"repo":"o/r","sha":"m1","mergedAt":"2026-11-15T07:00:00Z","checked":true,"seen":true}},
			{"id":"c_2","key":"k2","kind":"code","title":"Old","phase":"landed",
			"deployWatch":{"repo":"o/r","sha":"m0","mergedAt":"2026-11-10T07:00:00Z","done":true}}]}`
	th, err := DecodeThread([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if !SettlesWhenDone(th) || len(RulesOf(th, PurposeSettle)) != 2 {
		t.Fatalf("settle rules: %+v", th.Rules)
	}
	if w := SettleWhenChange(th); w == nil || w.Change != "c_1" || w.Phase != PhaseLive {
		t.Fatalf("settle when change: %+v", w)
	}
	merged := time.Date(2026, 11, 15, 7, 0, 0, 0, time.UTC)
	w := DeployWatches(th, merged.Add(48*time.Hour)) // seen: 7 days
	if len(w) != 1 || w[0].Change != "c_1" || w[0].SHA != "m1" || w[0].Repo != "o/r" || w[0].First {
		t.Fatalf("deploy watches: %+v", w)
	}
	if FindRule(th, deployRuleID("c_2")) != nil || FindRule(th, "builtins:c_1") == nil {
		t.Fatalf("rules: %+v", th.Rules)
	}
	b, _ := json.Marshal(th)
	again, err := DecodeThread(b)
	if err != nil || len(again.Rules) != len(th.Rules) {
		t.Fatalf("the new shape does not round-trip: %v %+v", err, again.Rules)
	}
	if strings.Contains(string(b), `"deployWatch":{`) {
		t.Fatalf("stored again with deployWatch: %s", b)
	}

	v, err := DecodeView([]byte(`{"user":"owner","entity":"t_1","seenAt":"2026-11-15T08:00:00Z","snoozeUntil":"2026-11-16T06:00:00Z","snoozedAt":"2026-11-15T08:30:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	until, since := SnoozeOf(v)
	if until == nil || !until.Equal(time.Date(2026, 11, 16, 6, 0, 0, 0, time.UTC)) || !since.Equal(time.Date(2026, 11, 15, 8, 30, 0, 0, time.UTC)) {
		t.Fatalf("snooze: %v %v", until, since)
	}
	vb, _ := json.Marshal(v)
	if strings.Contains(string(vb), `"snoozeUntil"`) {
		t.Fatalf("view stored again with snoozeUntil: %s", vb)
	}
}

// #120, V0.checks-follow-head: a watch on checks ok armed right after a
// push, before the poller has read the new head, does not fire on the
// previous head's result. It holds only on a PR read after it was armed,
// and arming it reads the PR at once.
func TestWatchOnChecksWaitsForNewHead(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/12"
	read := func(checks Checks, head string, at time.Time) Result {
		return step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved,
			Args: PR{URL: url, State: "OPEN", Checks: checks, Head: head, CheckedAt: &at}}, at)
	}
	read(ChecksPass, "aaaa111", ct0)
	c := th.Changes[0]
	// The agent pushes head B and arms the watch before the next poll.
	res := step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify",
		When: When{Any: []When{{Kind: WhenCondition, Change: c.Key, Type: CondChecks, Status: CondOK},
			{Kind: WhenCondition, Change: c.Key, Type: CondChecks, Status: CondFail}}}}}, ct0.Add(time.Minute))
	if fired(res, "w_1") != 0 {
		t.Fatal("the watch fired on the old head's checks")
	}
	if len(res.Effects) != 1 || res.Effects[0].Kind != EffectChangeRefresh {
		t.Fatalf("arming a watch on checks did not read the PR: %+v", res.Effects)
	}
	if res = read(ChecksPending, "bbbb222", ct0.Add(2*time.Minute)); fired(res, "w_1") != 0 {
		t.Fatal("the watch fired while the new head's checks ran")
	}
	if res = read(ChecksPass, "bbbb222", ct0.Add(3*time.Minute)); fired(res, "w_1") != 1 {
		t.Fatalf("checks ok on the new head did not fire the watch: %+v", res.Events)
	}
}

// A watch on checks armed when nothing is pending fires on the next read
// of the same head: the anchor waits for a read, not for a new head.
func TestWatchOnChecksFiresOnTheNextReadOfTheSameHead(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/12"
	at := ct0
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPass, Head: "aaaa111", CheckedAt: &at}}, at)
	c := th.Changes[0]
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify",
		When: When{Kind: WhenCondition, Change: c.Key, Type: CondChecks, Status: CondOK}}}, ct0.Add(time.Minute))
	at2 := ct0.Add(2 * time.Minute)
	res := step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPass, Head: "aaaa111", CheckedAt: &at2}}, at2)
	if fired(res, "w_1") != 1 {
		t.Fatalf("the next read of the same green head did not fire the watch: %+v", res.Events)
	}
}

// A read GitHub was asked for before the watch was armed (a poll in flight
// during the push) does not count as a read after it.
func TestWatchOnChecksIgnoresAReadAskedBeforeItWasArmed(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/12"
	read := func(checks Checks, head string, asked, at time.Time) Result {
		return step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved,
			Args: PR{URL: url, State: "OPEN", Checks: checks, Head: head, CheckedAt: &asked}}, at)
	}
	read(ChecksPass, "aaaa111", ct0, ct0)
	c := th.Changes[0]
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify",
		When: When{Kind: WhenCondition, Change: c.Key, Type: CondChecks, Status: CondOK}}}, ct0.Add(2*time.Minute))
	if res := read(ChecksPass, "aaaa111", ct0.Add(time.Minute), ct0.Add(3*time.Minute)); fired(res, "w_1") != 0 {
		t.Fatal("a read asked for before the watch was armed fired it")
	}
	if res := read(ChecksPass, "bbbb222", ct0.Add(4*time.Minute), ct0.Add(4*time.Minute)); fired(res, "w_1") != 1 {
		t.Fatalf("a read after the arm did not fire the watch: %+v", res.Events)
	}
}

// A watch on a PR Fyr no longer polls (the change is done) is not anchored:
// no read would come, so it reads the stored state as before.
func TestWatchOnChecksOfAnUnpolledPRIsNotAnchored(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	const url = "https://github.com/o/r/pull/12"
	at := ct0
	step(t, th, v, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: url, State: "OPEN", Checks: ChecksPass, Head: "aaaa111", CheckedAt: &at}}, at)
	th.Changes[0].Done = &DoneMark{At: ct0, By: ByOwner}
	c := th.Changes[0]
	res := step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", Then: "notify",
		When: When{Kind: WhenCondition, Change: c.Key, Type: CondChecks, Status: CondOK}}}, ct0.Add(time.Minute))
	if len(res.Effects) != 0 {
		t.Fatalf("a watch on an unpolled PR asked for a read: %+v", res.Effects)
	}
	if r := FindRule(th, "w_1"); r != nil {
		if _, ok := r.Armed["pr:"+c.ID]; ok {
			t.Fatal("a watch on an unpolled PR was anchored")
		}
	}
}
