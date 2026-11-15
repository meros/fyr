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
// settled; a state that became true during the pause does not fire on
// resume, an event (an anchored kind) does.
func TestWatchSuspendedWhileSettled(t *testing.T) {
	th, v := &Thread{ID: "t_1"}, &View{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "k"}, ByAgent, ct0)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_state", Then: "notify", When: When{Kind: WhenReached, Change: c.ID, Phase: PhaseReview}}}, ct0)
	step(t, th, v, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_event", Then: "notify", When: When{Kind: WhenRunEnded}}}, ct0)
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.settle", Args: Args{"now": true}}, ct0)
	if r := FindRule(th, "w_state"); r == nil || r.Status != RuleSuspended || r.SuspendedBy[0] != SuspendSettle {
		t.Fatalf("not suspended: %+v", r)
	}
	res := step(t, th, v, Input{Source: FromAgent, Kind: KindChangeStatus, Args: ChangeStatusArgs{Change: c.ID, Update: ChangeUpdate{Phase: PhaseReview}}}, ct0)
	res2 := step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}, ct0)
	if fired(res, "w_state")+fired(res2, "w_event") != 0 {
		t.Fatal("a suspended watch fired")
	}
	res = step(t, th, v, Input{Source: FromOwner, Kind: "thread.unsettle"}, ct0)
	if fired(res, "w_state") != 0 || fired(res, "w_event") != 1 {
		t.Fatalf("resume: %+v", res.Events)
	}
	if r := FindRule(th, "w_state"); r == nil || r.Status != RuleArmed {
		t.Fatalf("not resumed: %+v", r)
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
