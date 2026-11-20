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

// SPEC V0.210 (1), V0.text-ask-nudge: nothing is judged where the end
// cannot be a question to the owner, or where the agent offered a chip.
func TestTextAskNotJudged(t *testing.T) {
	queued := &Thread{ID: "t"}
	queued.SlotQueued = true
	asked := &Thread{ID: "t"}
	asked.Decisions = []Decision{{ID: "d", Kind: DecisionAsk, CreatedAt: ta0}}
	watching := &Thread{ID: "t", Rules: []Rule{{ID: "w", Purpose: "watch", Status: "armed", When: When{Kind: "at"}}}}
	chip := &Thread{ID: "t"}
	chip.Offers = []ActionSpec{{ID: "next", On: OnThread, Label: "Arm a watch", Run: RunAgent, Prompt: "arm it"}}
	cases := map[string]struct {
		th   *Thread
		args RunEndedArgs
	}{
		"failed":      {&Thread{ID: "t"}, RunEndedArgs{Failure: &Failure{Class: "process", Message: "x"}}},
		"interrupted": {&Thread{ID: "t"}, RunEndedArgs{Data: map[string]any{"interrupted": true}}},
		"open ask":    {asked, RunEndedArgs{}},
		"slot queue":  {queued, RunEndedArgs{}},
		"watch":       {watching, RunEndedArgs{}},
		"chip":        {chip, RunEndedArgs{}},
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

// V0.text-ask-nudge (1): a yes tells the agent and logs it; the owner sees
// nothing of it, so the thread is not in Needs you.
func TestTextAskYesNudgesTheAgent(t *testing.T) {
	th, _ := taEnd(t, &Thread{ID: "t"}, RunEndedArgs{}, ta0)
	at := th.ActivityAt
	th, r := taJudge(t, th, 1, true, "  Which logo do you want?  ", ta0.Add(time.Second))
	if !taHas(r, EventTextAskNudged) || th.TextNudged != 1 {
		t.Fatalf("events %+v, nudged %d", r.Events, th.TextNudged)
	}
	if n := queued(t, th, "text-question"); n.Urgency != Wake || !strings.Contains(n.What, "Which logo do you want?") {
		t.Fatalf("notice %+v", n)
	}
	if a := Compute(th, &View{SeenAt: ta0.Add(time.Hour)}, ta0.Add(2*time.Second)); a.Section == NeedsYou {
		t.Fatalf("Fyr's reading put the thread in Needs you: %+v", a)
	}
	if !th.ActivityAt.Equal(at) {
		t.Fatal("the judgment moved the activity time")
	}
}

// SPEC V0.210 (3): a no, a stale result, an empty question and a result
// during a run tell the agent nothing.
func TestTextAskIgnoredResults(t *testing.T) {
	th, _ := taEnd(t, &Thread{ID: "t"}, RunEndedArgs{}, ta0)
	for name, x := range map[string]struct {
		run   int
		needs bool
		q     string
	}{"no": {1, false, ""}, "stale": {0, true, "Old?"}, "empty": {1, true, "   "}} {
		if _, r := taJudge(t, th, x.run, x.needs, x.q, ta0.Add(time.Second)); taHas(r, EventTextAskNudged) {
			t.Errorf("%s: nudged", name)
		}
	}
	run, _ := fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, ta0.Add(time.Second))
	if _, r := taJudge(t, run, 1, true, "Late?", ta0.Add(2*time.Second)); taHas(r, EventTextAskNudged) {
		t.Fatal("a result during a run nudged")
	}
}

// V0.text-ask-nudge (2): the run the nudge starts is the agent's answer and
// is not read again; the run after it is, and the same run is nudged once.
func TestTextAskNudgesOnce(t *testing.T) {
	th, _ := taEnd(t, &Thread{ID: "t"}, RunEndedArgs{}, ta0)
	th, _ = taJudge(t, th, 1, true, "First?", ta0.Add(time.Second))
	if _, r := taJudge(t, th, 1, true, "First?", ta0.Add(2*time.Second)); taHas(r, EventTextAskNudged) {
		t.Fatal("the same run nudged twice")
	}
	th, ns := take(t, th, FeedTake{At: AtIdle}, ta0.Add(3*time.Second))
	if len(ns) != 1 {
		t.Fatalf("delivered %+v", ns)
	}
	th, r := taEnd(t, th, RunEndedArgs{}, ta0.Add(time.Minute))
	if taEffect(r) != nil {
		t.Fatal("the answer to the nudge was judged")
	}
	if _, r = taEnd(t, th, RunEndedArgs{}, ta0.Add(10*time.Minute)); taEffect(r) == nil {
		t.Fatal("a later run was not judged")
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
