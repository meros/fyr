package core

import (
	"strings"
	"testing"
	"time"
)

// V0.102: in a repo the owner opted in, Fyr itself approves an agent's merge
// ask once the PR is ready on the head the ask named. The approval is an
// observer's Input; the agent cannot send it, and the owner's setting is the
// host's (the engine's test covers it).

func greenPR(url, head string, at time.Time) Input {
	return Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "acme/app", Number: 88, State: "OPEN", Checks: ChecksPass, MergeState: "CLEAN", Head: head, CheckedAt: &at}}
}

func autoIn(repo string) Input {
	return Input{Source: Observer(ObsHost), Kind: KindAutoMerge, Thread: "t1", Args: AutoMergeArgs{Decision: "d_merge", Repo: repo}}
}

func TestAutoMergeWaitsForGreenChecksOnTheAskedHead(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := askMerge(t, url, now) // checks pass, but the merge state is not read yet
	if got := AutoMergeAsks(th); len(got) != 0 {
		t.Fatalf("an ask whose PR is not ready is offered: %+v", got)
	}
	if _, _, _, err := Reduce(th, &View{}, autoIn("acme/app"), now); err == nil {
		t.Fatal("the reduce approved a PR that is not ready")
	}
	later := now.Add(time.Minute)
	th, _, _, err := Reduce(th, &View{}, greenPR(url, "fff0000aaaa1111", later), later)
	if err != nil {
		t.Fatal(err)
	}
	// V0.136: a push closes the ask; nothing is left to approve
	if got := AutoMergeAsks(th); len(got) != 0 || len(th.Decisions) != 0 {
		t.Fatalf("a head pushed after the ask is offered: %+v %+v", got, th.Decisions)
	}
	if _, _, _, err := Reduce(th, &View{}, autoIn("acme/app"), later); err == nil || !strings.Contains(err.Error(), "no open decision") {
		t.Fatalf("a moved head: %v", err)
	}
}

func TestAutoMergeApprovesTheAskAndRunsTheBoundMerge(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := askMerge(t, url, now)
	later := now.Add(time.Minute)
	th, _, _, err := Reduce(th, &View{}, greenPR(url, "abc1234def5678", later), later)
	if err != nil {
		t.Fatal(err)
	}
	asks := AutoMergeAsks(th)
	if len(asks) != 1 || asks[0] != (AutoMergeArgs{Decision: "d_merge", Repo: "acme/app"}) {
		t.Fatalf("asks %+v", asks)
	}
	th, _, res, err := Reduce(th, &View{}, autoIn("acme/app"), later)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Decisions) != 0 {
		t.Fatalf("the ask is still open: %+v", th.Decisions)
	}
	d := closedEvent(res, "d_merge")
	if d == nil || d["auto"] != true || !strings.Contains(d["answer"].(string), "acme/app") {
		t.Fatalf("decision.closed %+v", res.Events)
	}
	if len(res.Effects) != 1 {
		t.Fatalf("effects %+v", res.Effects)
	}
	ef := res.Effects[0]
	if ef.Kind != EffectRun || !ef.Allow || ef.ByOwner || ef.Request == nil || ef.Request.Action != FyrMerge ||
		ef.Request.Args["head"] != "abc1234def5678" || ef.Request.Args["auto"] != "acme/app" {
		t.Fatalf("effect %+v %+v", ef, ef.Request)
	}
	c := th.Changes[0]
	if c.AutoApproved == nil || c.AutoApproved.Repo != "acme/app" || c.AutoApproved.Head != "abc1234def5678" {
		t.Fatalf("the change does not say it was auto-approved: %+v", c.AutoApproved)
	}
	// The result names the setting, for the thread and the agent.
	th, _, res, err = Reduce(th, &View{}, Input{Source: FromEffect("fx1"), Kind: KindEffectResult, Thread: "t1",
		Args: EffectResult{Effect: ef, OK: true, Text: "merged"}}, later)
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, ev := range res.Events {
		if m, _ := ev.Data.(map[string]any); ev.Type == "effect.result" {
			line, _ = m["text"].(string)
		}
	}
	if !strings.Contains(line, "Auto-approved") || !strings.Contains(line, "acme/app") {
		t.Fatalf("result line %q", line)
	}
}

func TestOnlyAnObserverAutoApproves(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := askMerge(t, url, now)
	th, _, _, _ = Reduce(th, &View{}, greenPR(url, "abc1234def5678", now), now)
	for _, src := range []Source{FromAgent, FromOwner} {
		in := autoIn("acme/app")
		in.Source = src
		if _, _, _, err := Reduce(th, &View{}, in, now); err == nil {
			t.Fatalf("%s sent the auto-approval", src)
		}
	}
	if k := Kinds[KindAutoMergeSet]; k == nil || len(k.From) != 1 || k.From[0] != ByOwner {
		t.Fatalf("the setting is not the owner's alone: %+v", k)
	}
	if _, err := ReduceHost(Input{Source: FromAgent, Kind: KindAutoMergeSet, Args: Args{"repo": "acme/app", "on": true}}, now); err == nil {
		t.Fatal("an agent turned the setting on")
	}
	if _, err := ReduceHost(Input{Source: FromOwner, Kind: KindAutoMergeSet, Args: Args{"repo": "not a repo", "on": true}}, now); err == nil {
		t.Fatal("a bad repo name was accepted")
	}
}

func TestAutoMergeWaitsWhileTheThreadRuns(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := askMerge(t, url, now)
	th, _, _, _ = Reduce(th, &View{}, greenPR(url, "abc1234def5678", now), now)
	th.Running = true // the agent may still push
	if got := AutoMergeAsks(th); len(got) != 0 {
		t.Fatalf("offered while the run goes on: %+v", got)
	}
}

// V0.200: an auto-merge at work sends no push; what the owner must act on,
// or asked to hear, still does.
func TestPushQuietFollowsTheAutoMerge(t *testing.T) {
	now := time.Date(2026, 11, 18, 13, 0, 0, 0, time.UTC)
	const head = "abc1234def5678"
	url := "https://github.com/acme/app/pull/88"
	on := func(repo string) bool { return RepoKey(repo) == "acme/app" }
	off := func(string) bool { return false }
	th := askMerge(t, url, now)
	later := now.Add(time.Minute)
	th, _, _, err := Reduce(th, &View{}, greenPR(url, head, later), later)
	if err != nil {
		t.Fatal(err)
	}
	if !PushQuiet(th, Asking, on) {
		t.Fatal("an ask Fyr approves itself pushes")
	}
	if PushQuiet(th, Asking, off) {
		t.Fatal("an ask in a repo without the setting is quiet")
	}
	th.Running = true
	if !PushQuiet(th, Asking, on) {
		t.Fatal("an ask Fyr approves when the run ends pushes")
	}
	th.Running = false
	other := Decision{ID: "d_q", Kind: DecisionAsk, CreatedAt: later, Questions: []Question{{Question: "Which?"}}}
	th.Decisions = append(th.Decisions, other)
	if PushQuiet(th, Asking, on) {
		t.Fatal("a question beside the ask is quiet")
	}
	th.Decisions = th.Decisions[:1]

	if PushQuiet(th, Done, on) {
		t.Fatal("done is quiet before any auto-merge")
	}
	th, _, _, err = Reduce(th, &View{}, autoIn("acme/app"), later)
	if err != nil {
		t.Fatal(err)
	}
	if got := Compute(th, &View{}, later).State; got != Blocked || !PushQuiet(th, got, on) {
		t.Fatalf("the ready PR of the running merge: %s, quiet %v", got, PushQuiet(th, got, on))
	}
	if !PushQuiet(th, Done, on) {
		t.Fatal("done after the auto-merge pushes")
	}
	if PushQuiet(th, Failed, on) {
		t.Fatal("a failure is quiet")
	}

	c := FindChange(th, th.QuietMerge)
	setCondition(c, CondMerge, CondFail, "Merge refused", ByObserved, later)
	if PushQuiet(th, Done, on) || PushQuiet(th, Blocked, on) {
		t.Fatal("a refused merge is quiet")
	}
	dropConditions(c, CondMerge)
	c.PR.Head = "fff0000fff0000"
	if PushQuiet(th, Done, on) {
		t.Fatal("a new head is quiet")
	}
	c.PR.Head = head
	c.Phase = PhaseLive
	if PushQuiet(th, Done, on) {
		t.Fatal("a live deploy is quiet")
	}
	c.Phase = PhaseLanded
	if !PushQuiet(th, Done, on) {
		t.Fatal("the landed merge pushes")
	}
	DecisionOpened(th, other)
	th.Decisions = nil
	if PushQuiet(th, Done, on) {
		t.Fatal("done after a new question is quiet")
	}
}
