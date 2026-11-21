package core

import (
	"strings"
	"testing"
	"time"
)

// Decision 0024 §5: an ask is void when its change is done, when its thread
// is archived, or when its precondition is gone. A merge ask on a PR that
// merged on GitHub kept the thread in Needs you with "Merge #N" until the
// owner pressed something.

func askMerge(t *testing.T, url string, now time.Time) *Thread {
	t.Helper()
	th := prThread(t, url, now)
	// V0.97: a merge ask binds to the head Fyr read.
	th, _, _, err := Reduce(th, &View{}, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "acme/app", Number: 88, State: "OPEN", Checks: ChecksPass, Head: "abc1234def5678", CheckedAt: &now}}, now)
	if err != nil {
		t.Fatal(err)
	}
	th, _, _, err = Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
		Args: ChangeRequest{ID: "d_merge", Change: th.Changes[0].ID, Ask: AskMerge, Args: map[string]any{"head": "abc1234def5678"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Decisions) != 1 || Compute(th, &View{}, now).State != Asking {
		t.Fatalf("setup: decisions %+v", th.Decisions)
	}
	return th
}

func closedEvent(res Result, id string) map[string]any {
	for _, ev := range res.Events {
		if d, _ := ev.Data.(map[string]any); ev.Type == "decision.closed" && d["id"] == id {
			return d
		}
	}
	return nil
}

func TestMergeAskIsVoidWhenThePRMergesElsewhere(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := askMerge(t, url, now)
	later := now.Add(time.Minute)
	th, _, res, err := Reduce(th, &View{}, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "acme/app", Number: 88, State: "MERGED", Checks: ChecksPass, CheckedAt: &later}}, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Decisions) != 0 {
		t.Fatalf("the merge ask is still open: %+v", th.Decisions)
	}
	if d := closedEvent(res, "d_merge"); d == nil || d["void"] != true {
		t.Fatalf("no void decision.closed event: %+v", res.Events)
	}
	if a := Compute(th, &View{SeenAt: later}, later); a.Section == NeedsYou && a.State == Asking {
		t.Fatalf("still asking: %+v", a)
	}
	if txt := NoticeText(th.Feed.Queue); !strings.Contains(txt, "void") {
		t.Fatalf("the agent is not told: %q", txt)
	}
}

// V0.void-held-ask-and-prompts: a held merge ask whose PR merges closes as
// void (not a gate failure telling the agent to ask again).
func TestHeldMergeAskClosesAsVoidWhenPRMerges(t *testing.T) {
	now := time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)
	th, _ := heldAsk(t, now)
	later := now.Add(time.Minute)
	th, _, res, err := Reduce(th, &View{}, askPR(later, func(p *PR) { p.State, p.Checks = "MERGED", ChecksPass }), later)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.HeldAsks) != 0 || len(th.Decisions) != 0 {
		t.Fatalf("held ask remained: held=%+v dec=%+v", th.HeldAsks, th.Decisions)
	}
	d := closedEvent(res, "d_merge")
	if d == nil || d["void"] != true || d["gate"] != nil {
		t.Fatalf("expected void decision.closed without gate=true, got %+v", d)
	}
	var said string
	for _, n := range resultNotices(Reduced{Before: th, After: th, Events: res.Events}) {
		said += n.What
	}
	if !strings.Contains(said, "Do not ask again") || strings.Contains(said, "then ask again") {
		t.Fatalf("contradictory notice text: %q", said)
	}
}

// V0.136: a failed check closes a merge ask (the agent hears why); an
// open PR whose checks pass keeps it.
func TestMergeAskStaysWhileThePRIsOpen(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := askMerge(t, url, now)
	later := now.Add(time.Minute)
	th, _, _, err := Reduce(th, &View{}, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "acme/app", Number: 88, State: "OPEN", Checks: ChecksPass, Head: "abc1234def5678", CheckedAt: &later}}, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Decisions) != 1 {
		t.Fatalf("an open green PR closed the ask: %+v", th.Decisions)
	}
	th, _, _, err = Reduce(th, &View{}, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1",
		Args: PR{URL: url, Repo: "acme/app", Number: 88, State: "OPEN", Checks: ChecksFail, Head: "abc1234def5678", CheckedAt: &later}}, later)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Decisions) != 0 {
		t.Fatalf("a failed check kept the merge ask: %+v", th.Decisions)
	}
}

// The host side of the report: a settle ranks above an open ask, so a
// settled thread is never in Needs you, whatever it still waits on.
func TestSettledThreadWithAnOpenAskIsNotInNeedsYou(t *testing.T) {
	now := time.Date(2026, 11, 16, 13, 0, 0, 0, time.UTC)
	th := askMerge(t, "https://github.com/acme/app/pull/88", now)
	th, v, _, err := Reduce(th, &View{}, Input{Source: FromOwner, Kind: "thread.settle", Thread: "t1", Args: Args{"now": true}}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if a := Compute(th, v, now.Add(2*time.Minute)); a.Section != SettledSec {
		t.Fatalf("settled thread in %s (%s)", a.Section, a.State)
	}
}
