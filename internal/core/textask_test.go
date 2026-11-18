package core

import (
	"strings"
	"testing"
	"time"
)

var ta0 = time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)

func taEnd(t *testing.T, th *Thread, args RunEndedArgs, at time.Time) (*Thread, Result) {
	t.Helper()
	th, _ = fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, at.Add(-time.Minute))
	return fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: args}, at)
}

func taJudge(t *testing.T, th *Thread, run int, needs bool, q string, at time.Time) (*Thread, Result) {
	t.Helper()
	return fstep(t, th, Input{Source: Observer(ObsTextAsk), Kind: KindTextAskJudged, Args: TextAskJudged{Run: run, Needs: needs, Question: q}}, at)
}

func taEffect(r Result) *Effect {
	for i := range r.Effects {
		if r.Effects[i].Kind == EffectTextAsk {
			return &r.Effects[i]
		}
	}
	return nil
}

func taHas(r Result, typ string) bool {
	for _, e := range r.Events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

// SPEC V0.210 (1): a clean end of an idle thread asks for a judgment of that run.
func TestTextAskCleanEndIsJudged(t *testing.T) {
	th, r := taEnd(t, &Thread{ID: "t"}, RunEndedArgs{Data: map[string]any{"interrupted": false}}, ta0)
	ef := taEffect(r)
	if ef == nil || ef.Run != th.RunEnds || ef.Run != 1 {
		t.Fatalf("effect %+v, runEnds %d", ef, th.RunEnds)
	}
}

// SPEC V0.210 (1): nothing is judged where the end cannot be a question to the owner.
func TestTextAskNotJudged(t *testing.T) {
	queued := &Thread{ID: "t"}
	queued.SlotQueued = true
	asked := &Thread{ID: "t"}
	asked.Decisions = []Decision{{ID: "d", Kind: DecisionAsk, CreatedAt: ta0}}
	watching := &Thread{ID: "t", Rules: []Rule{{ID: "w", Purpose: "watch", Status: "armed", When: When{Kind: "at"}}}}
	cases := map[string]struct {
		th   *Thread
		args RunEndedArgs
	}{
		"failed":      {&Thread{ID: "t"}, RunEndedArgs{Failure: &Failure{Class: "process", Message: "x"}}},
		"interrupted": {&Thread{ID: "t"}, RunEndedArgs{Data: map[string]any{"interrupted": true}}},
		"open ask":    {asked, RunEndedArgs{}},
		"slot queue":  {queued, RunEndedArgs{}},
		"watch":       {watching, RunEndedArgs{}},
	}
	for name, c := range cases {
		if _, r := taEnd(t, c.th, c.args, ta0); taEffect(r) != nil {
			t.Errorf("%s: judged", name)
		}
	}
}

// SPEC V0.210 (1): a thread in Needs you for another cause is not judged.
func TestTextAskSkipsThreadAlreadyNeedingOwner(t *testing.T) {
	th := needThread(t, ready("abc"))
	_, r := taEnd(t, th, RunEndedArgs{}, nt0.Add(time.Minute))
	if taEffect(r) != nil {
		t.Fatal("a ready PR's thread was judged")
	}
}

// SPEC V0.210 (3), (4): a yes puts the idle thread in Needs you, logged, quiet.
func TestTextAskYesIsNeedsYouWithoutPush(t *testing.T) {
	th, _ := taEnd(t, &Thread{ID: "t"}, RunEndedArgs{}, ta0)
	at := th.ActivityAt
	th, r := taJudge(t, th, 1, true, "  Which logo do you want?  ", ta0.Add(time.Second))
	if th.TextAsk == nil || th.TextAsk.Question != "Which logo do you want?" || th.TextAsk.Run != 1 {
		t.Fatalf("flag %+v", th.TextAsk)
	}
	if !taHas(r, "textAsk.raised") {
		t.Fatal("not logged")
	}
	a := Compute(th, &View{SeenAt: ta0.Add(time.Hour)}, ta0.Add(2*time.Second))
	if a.State != Blocked || a.Section != NeedsYou || a.Reason != "Which logo do you want?" {
		t.Fatalf("attention %+v", a)
	}
	if !PushQuiet(th, Blocked, func(string) bool { return false }) {
		t.Fatal("an inferred question pushes")
	}
	if !th.ActivityAt.Equal(at) {
		t.Fatal("the judgment moved the activity time")
	}
}

// SPEC V0.210 (4): a hard cause on the same thread still pushes.
func TestTextAskDoesNotQuietAHardCause(t *testing.T) {
	th := needThread(t, ready("abc"))
	th.TextAsk = &TextAsk{Question: "Which?", At: nt0, Run: 1}
	if PushQuiet(th, Blocked, func(string) bool { return false }) {
		t.Fatal("a ready PR went quiet")
	}
}

// SPEC V0.210 (3): a no, a stale result and a result during a run change nothing.
func TestTextAskIgnoredResults(t *testing.T) {
	th, _ := taEnd(t, &Thread{ID: "t"}, RunEndedArgs{}, ta0)
	if x, r := taJudge(t, th, 1, false, "", ta0.Add(time.Second)); x.TextAsk != nil || taHas(r, "textAsk.raised") {
		t.Fatal("a no raised the flag")
	}
	if x, _ := taJudge(t, th, 0, true, "Old?", ta0.Add(time.Second)); x.TextAsk != nil {
		t.Fatal("a stale result raised the flag")
	}
	if x, _ := taJudge(t, th, 1, true, "   ", ta0.Add(time.Second)); x.TextAsk != nil {
		t.Fatal("an empty question raised the flag")
	}
	run, _ := fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, ta0.Add(time.Second))
	if x, _ := taJudge(t, run, 1, true, "Late?", ta0.Add(2*time.Second)); x.TextAsk != nil {
		t.Fatal("a result during a run raised the flag")
	}
}

// SPEC V0.210 (3): the flag survives a run the owner did not start; a yes replaces the line, a no leaves it.
func TestTextAskSurvivesOtherRuns(t *testing.T) {
	th, _ := taEnd(t, &Thread{ID: "t"}, RunEndedArgs{}, ta0)
	th, _ = taJudge(t, th, 1, true, "First?", ta0.Add(time.Second))
	th, r := taEnd(t, th, RunEndedArgs{}, ta0.Add(5*time.Minute))
	if th.TextAsk == nil || taEffect(r) == nil {
		t.Fatalf("flag %+v, judged %v", th.TextAsk, taEffect(r) != nil)
	}
	th, _ = taJudge(t, th, 2, false, "", ta0.Add(6*time.Minute))
	if th.TextAsk == nil || th.TextAsk.Question != "First?" {
		t.Fatalf("a no changed the flag: %+v", th.TextAsk)
	}
	th, _ = taEnd(t, th, RunEndedArgs{}, ta0.Add(10*time.Minute))
	th, _ = taJudge(t, th, 3, true, "Second?", ta0.Add(11*time.Minute))
	if th.TextAsk.Question != "Second?" {
		t.Fatalf("a yes kept the old line: %+v", th.TextAsk)
	}
}

// SPEC V0.210 (3), (5): Not a question clears and logs it, and a call in flight cannot bring it back.
func TestNotAQuestion(t *testing.T) {
	th, _ := taEnd(t, &Thread{ID: "t"}, RunEndedArgs{}, ta0)
	if _, _, _, err := Reduce(th, nil, Input{Source: FromOwner, Kind: KindNotQuestion, Thread: "t"}, ta0); err == nil {
		t.Fatal("available without a flag")
	}
	th, _ = taJudge(t, th, 1, true, "First?", ta0.Add(time.Second))
	if _, _, _, err := Reduce(th, nil, Input{Source: FromAgent, Kind: KindNotQuestion, Thread: "t"}, ta0); err == nil {
		t.Fatal("an agent dismissed it")
	}
	th, r := fstep(t, th, Input{Source: FromOwner, Kind: KindNotQuestion}, ta0.Add(time.Minute))
	if th.TextAsk != nil || !taHas(r, "textAsk.dismissed") {
		t.Fatalf("flag %+v, events %+v", th.TextAsk, r.Events)
	}
	if a := Compute(th, &View{SeenAt: ta0.Add(time.Hour)}, ta0.Add(2*time.Minute)); a.Section == NeedsYou {
		t.Fatalf("still in Needs you: %+v", a)
	}
	if x, _ := taJudge(t, th, 1, true, "First?", ta0.Add(2*time.Minute)); x.TextAsk != nil {
		t.Fatal("a call in flight brought it back")
	}
}

// SPEC V0.210 (3): the owner's message and a settle clear it; another agent's message does not.
func TestTextAskClearedByOwnerOnly(t *testing.T) {
	flagged := func() *Thread {
		th, _ := taEnd(t, &Thread{ID: "t"}, RunEndedArgs{}, ta0)
		th, _ = taJudge(t, th, 1, true, "First?", ta0.Add(time.Second))
		return th
	}
	if th, _ := fstep(t, flagged(), Input{Source: FromOwner, Kind: "thread.send", Args: Args{"text": "yes"}}, ta0.Add(time.Minute)); th.TextAsk != nil {
		t.Fatal("the owner's message left it")
	}
	if th, _ := fstep(t, flagged(), Input{Source: FromAgent, From: "t2", Kind: "thread.send", Args: Args{"text": "status"}}, ta0.Add(time.Minute)); th.TextAsk == nil {
		t.Fatal("another agent's message cleared it")
	}
	if th, _ := fstep(t, flagged(), Input{Source: FromOwner, Kind: "thread.settle", Args: Args{"now": true}}, ta0.Add(time.Minute)); th.TextAsk != nil {
		t.Fatal("a settle left it")
	}
}

// SPEC V0.210 (2): the model's answer is read strictly; the question is at most 12 words.
func TestParseTextAsk(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("word ", 20))
	cases := []struct {
		in    string
		ok    bool
		needs bool
		q     string
	}{
		{`{"needs":true,"q":"Which one?"}`, true, true, "Which one?"},
		{"```json\n{\"needs\": false, \"q\": \"\"}\n```", true, false, ""},
		{`{"needs":true,"q":"` + long + `"}`, true, true, strings.TrimSpace(strings.Repeat("word ", 12)) + "…"},
		{`{"needs":true,"q":""}`, false, false, ""},
		{"I think so", false, false, ""},
	}
	for _, c := range cases {
		needs, q, ok := ParseTextAsk(c.in)
		if ok != c.ok || needs != c.needs || q != c.q {
			t.Errorf("%q: %v %q %v", c.in, needs, q, ok)
		}
	}
}

// SPEC V0.210 (2): the job is one the owner's System agents setting governs.
func TestTextAskIsASystemJob(t *testing.T) {
	if !IsSystemJob(JobQuestion) {
		t.Fatal("not a system job")
	}
}
