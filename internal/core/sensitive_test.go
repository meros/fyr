package core

import (
	"strings"
	"testing"
	"time"
)

// V0.sensitive-pr: a sensitive merge is never approved by the repo's
// auto-merge setting; the agent's reason and the GitHub label each mark it.

func sensitiveAsk(t *testing.T, url string, now time.Time, args map[string]any) *Thread {
	t.Helper()
	th := prThread(t, url, now)
	th, _, _, err := Reduce(th, &View{}, greenPR(url, "abc1234def5678", now), now)
	if err != nil {
		t.Fatal(err)
	}
	args["head"] = "abc1234def5678"
	th, _, _, err = Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
		Args: ChangeRequest{ID: "d_merge", Change: th.Changes[0].ID, Ask: AskMerge, Args: args}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Decisions) != 1 {
		t.Fatalf("setup: decisions %+v held %+v", th.Decisions, th.HeldAsks)
	}
	return th
}

func TestSensitiveAskIsNeverAutoApproved(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := sensitiveAsk(t, url, now, map[string]any{"sensitive": "  promotes   to production "})
	if got := AutoMergeAsks(th); len(got) != 0 {
		t.Fatalf("a sensitive ask is offered to auto-merge: %+v", got)
	}
	_, _, _, err := Reduce(th, &View{}, autoIn("acme/app"), now)
	if err == nil || !strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("the reduce approved a sensitive merge: %v", err)
	}
	if got := th.Changes[0].Sensitive; got != "promotes to production" {
		t.Fatalf("the change keeps %q", got)
	}
	// The ask waits on the owner, also in a repo of the setting.
	on := func(string) bool { return true }
	if asks := ownerAsks(th, on); len(asks) != 1 {
		t.Fatalf("owner asks %+v", asks)
	}
	if PushQuiet(th, Asking, on) {
		t.Fatal("a sensitive ask is pushed quietly")
	}
	// The owner's own yes still merges it.
	if why := FyrEffects[FyrMerge].Blocked(th, th.Decisions[0].Effect.Args); why != "" {
		t.Fatalf("the owner's merge is blocked: %s", why)
	}
}

func TestSensitiveChangeStaysSensitiveOnALaterAsk(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := sensitiveAsk(t, url, now, map[string]any{"sensitive": "promotes to production"})
	th.Decisions = nil // the ask closed; the agent asks again without the mark
	th, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
		Args: ChangeRequest{ID: "d_merge", Change: th.Changes[0].ID, Ask: AskMerge, Args: map[string]any{"head": "abc1234def5678"}}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := AutoMergeAsks(th); len(got) != 0 {
		t.Fatalf("the second ask lost the mark: %+v", got)
	}
}

func TestSensitiveLabelBlocksAutoMerge(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := sensitiveAsk(t, url, now, map[string]any{})
	if got := AutoMergeAsks(th); len(got) != 1 {
		t.Fatalf("setup: a plain ask is not offered: %+v", got)
	}
	in := greenPR(url, "abc1234def5678", now)
	p := in.Args.(PR)
	p.Sensitive = true
	in.Args = p
	th, _, _, err := Reduce(th, &View{}, in, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := AutoMergeAsks(th); len(got) != 0 {
		t.Fatalf("a labelled PR is offered to auto-merge: %+v", got)
	}
	if got := SensitiveWhy(&th.Changes[0]); got != LabelSensitive {
		t.Fatalf("why %q", got)
	}
}

func TestSensitiveArgMustSayWhy(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	url := "https://github.com/acme/app/pull/88"
	th := prThread(t, url, now)
	th, _, _, _ = Reduce(th, &View{}, greenPR(url, "abc1234def5678", now), now)
	for _, v := range []any{"", "   ", true} {
		_, _, _, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
			Args: ChangeRequest{ID: "d_merge", Change: th.Changes[0].ID, Ask: AskMerge, Args: map[string]any{"head": "abc1234def5678", "sensitive": v}}}, now)
		if err == nil || !strings.Contains(err.Error(), "sensitive") {
			t.Fatalf("sensitive %#v: %v", v, err)
		}
	}
}

func TestHasSensitiveLabel(t *testing.T) {
	if !HasSensitiveLabel([]string{"bug", " Sensitive "}) || HasSensitiveLabel([]string{"insensitive", "prod"}) {
		t.Fatal("label match")
	}
}
