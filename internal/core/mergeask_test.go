package core

import (
	"strings"
	"testing"
	"time"
)

// V0.136: an agent's merge ask reaches the owner only when the merge can
// run now. While CI has not passed on the asked head Fyr holds the ask: no
// card, no Needs you, no push; the change shows "Waiting for CI". Green
// opens the ask (or auto-merges); red, a conflict or a required review
// closes it, and the agent hears why.

const askHead = "7333251aa0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5"
const askURL = "https://github.com/acme/acmelistingcollector/pull/846"

func askPR(at time.Time, f func(*PR)) Input {
	p := PR{URL: askURL, Repo: "acme/monorepoListingCollector", Number: 846, State: "OPEN", Head: askHead,
		Checks: ChecksPending, MergeState: "BLOCKED", ChecksDone: 2, ChecksTotal: 5, CheckedAt: &at}
	if f != nil {
		f(&p)
	}
	return Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Thread: "t1", Args: p}
}

// heldAsk is #846 on 17 Nov: the agent pushed 7333251 and asked for the
// merge at once, while 3 of 5 checks still ran.
func heldAsk(t *testing.T, now time.Time) (*Thread, Result) {
	t.Helper()
	th, _, _, err := Reduce(&Thread{ID: "t1"}, &View{}, askPR(now, nil), now)
	if err != nil {
		t.Fatal(err)
	}
	th, _, res, err := Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
		Args: ChangeRequest{ID: "d_merge", Change: th.Changes[0].ID, Ask: AskMerge, Reason: "Tonight's nightly needs it"}}, now)
	if err != nil {
		t.Fatalf("the agent may ask while CI runs: %v", err)
	}
	return th, res
}

func hasEvent(res Result, typ string) map[string]any {
	for _, ev := range res.Events {
		if d, _ := ev.Data.(map[string]any); ev.Type == typ {
			return d
		}
	}
	return nil
}

func TestMergeAskOnPendingHeadIsHeld(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	th, res := heldAsk(t, now)
	if len(th.Decisions) != 0 {
		t.Fatalf("a merge ask on pending checks reached the owner: %+v", th.Decisions)
	}
	if len(th.HeldAsks) != 1 || th.HeldAsks[0].ID != "d_merge" || th.HeldAsks[0].Effect.Args["head"] != askHead {
		t.Fatalf("the ask is not recorded: %+v", th.HeldAsks)
	}
	if hasEvent(res, "decision.opened") != nil || hasEvent(res, "decision.held") == nil {
		t.Fatalf("events %+v", res.Events)
	}
	if a := Compute(th, &View{}, now); a.Section == NeedsYou {
		t.Fatalf("a held ask counts in Needs you: %+v", a)
	}
	for _, o := range th.Changes[0].Offers {
		if o.ID == "fyr.merge" {
			t.Fatal("the card offers Merge while CI runs")
		}
	}
	n := ChangeNeed(th, &th.Changes[0])
	if n == nil || n.Cause != CauseWaiting || !n.Muted || n.Text != "Waiting for CI: 2 of 5 checks done" || n.Head != askHead {
		t.Fatalf("the card does not say it waits for CI: %+v", n)
	}
	// more checks finish: the count follows the poller, still no ask
	later := now.Add(2 * time.Minute)
	th, _, _, err := Reduce(th, &View{}, askPR(later, func(p *PR) { p.ChecksDone = 4 }), later)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.Decisions) != 0 || ChangeNeed(th, &th.Changes[0]).Text != "Waiting for CI: 4 of 5 checks done" {
		t.Fatalf("after 4 of 5: %+v %+v", th.Decisions, ChangeNeed(th, &th.Changes[0]))
	}
	// no check reported yet on a repo with CI
	th, _, _, _ = Reduce(th, &View{}, askPR(later, func(p *PR) { p.ChecksDone, p.ChecksTotal = 0, 0 }), later)
	if got := ChangeNeed(th, &th.Changes[0]).Text; got != "Waiting for CI: no check has reported yet" {
		t.Fatalf("no checks yet: %q", got)
	}
}

func TestHeldMergeAskOpensOnGreen(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	th, _ := heldAsk(t, now)
	green := now.Add(6 * time.Minute)
	th, _, res, err := Reduce(th, &View{}, askPR(green, func(p *PR) { p.Checks, p.MergeState, p.ChecksDone = ChecksPass, "CLEAN", 5 }), green)
	if err != nil {
		t.Fatal(err)
	}
	if len(th.HeldAsks) != 0 || len(th.Decisions) != 1 || th.Decisions[0].ID != "d_merge" {
		t.Fatalf("green did not open the ask: held %+v decisions %+v", th.HeldAsks, th.Decisions)
	}
	if hasEvent(res, "decision.opened") == nil {
		t.Fatalf("events %+v", res.Events)
	}
	if a := Compute(th, &View{}, green); a.State != Asking {
		t.Fatalf("attention %+v", a)
	}
	if n := ChangeNeed(th, &th.Changes[0]); n == nil || n.Cause != CauseAsk {
		t.Fatalf("need %+v", n)
	}
	// the repo's auto-merge setting (V0.102) sees it as any open merge ask
	if asks := AutoMergeAsks(th); len(asks) != 1 || asks[0].Decision != "d_merge" {
		t.Fatalf("auto-merge asks %+v", asks)
	}
}

func TestHeldMergeAskClosesOnRed(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		pr   func(*PR)
		want string
	}{
		{"CI failed", func(p *PR) { p.Checks, p.FailedChecks = ChecksFail, []string{"test", "lint"} }, "CI failed on 7333251: test, lint"},
		{"conflict", func(p *PR) { p.Conflict, p.MergeState, p.Base = true, "DIRTY", "main" }, "it conflicts with main"},
		{"review", func(p *PR) { p.Checks, p.MergeState, p.Review = ChecksPass, "BLOCKED", "REVIEW_REQUIRED" }, "a review is required"},
		{"push", func(p *PR) { p.Head = "9999999aaaabbbb" }, "the head moved to 9999999"},
	}
	for _, c := range cases {
		th, _ := heldAsk(t, now)
		red := now.Add(4 * time.Minute)
		th, _, res, err := Reduce(th, &View{}, askPR(red, c.pr), red)
		if err != nil {
			t.Fatal(err)
		}
		if len(th.HeldAsks) != 0 || len(th.Decisions) != 0 {
			t.Errorf("%s: the ask is still there: held %+v decisions %+v", c.name, th.HeldAsks, th.Decisions)
			continue
		}
		d := hasEvent(res, "decision.closed")
		if d == nil || d["gate"] != true || !strings.Contains(str(d["reason"]), c.want) {
			t.Errorf("%s: decision.closed %+v", c.name, d)
			continue
		}
		var said string
		for _, n := range resultNotices(Reduced{Before: th, After: th, Events: res.Events}) {
			said += n.What
		}
		if !strings.Contains(said, c.want) || strings.Contains(said, "Do not ask again") || !strings.Contains(said, "ask again") {
			t.Errorf("%s: the agent hears %q", c.name, said)
		}
		if a := Compute(th, &View{}, red); a.State == Asking {
			t.Errorf("%s: the owner is asked", c.name)
		}
	}
}

// An ask on a PR that is already red is refused at once with the reason;
// an ask on a green PR opens at once, as before.
func TestMergeAskOnRedOrGreenHead(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	ask := func(f func(*PR)) (*Thread, error) {
		th, _, _, err := Reduce(&Thread{ID: "t1"}, &View{}, askPR(now, f), now)
		if err != nil {
			t.Fatal(err)
		}
		th, _, _, err = Reduce(th, &View{}, Input{Source: FromAgent, Kind: KindChangeRequest, Thread: "t1",
			Args: ChangeRequest{ID: "d_merge", Change: askURL, Ask: AskMerge, Args: map[string]any{"method": "squash"}}}, now)
		return th, err
	}
	if _, err := ask(func(p *PR) { p.Checks, p.FailedChecks = ChecksFail, []string{"test"} }); err == nil || !strings.Contains(err.Error(), "CI failed on 7333251: test") {
		t.Fatalf("an ask on red: %v", err)
	}
	th, err := ask(func(p *PR) { p.Checks, p.MergeState = ChecksPass, "CLEAN" })
	if err != nil || len(th.Decisions) != 1 || len(th.HeldAsks) != 0 {
		t.Fatalf("an ask on green: %v %+v", err, th.Decisions)
	}
	// a repo with no CI does not wait
	th, err = ask(func(p *PR) { p.Checks, p.MergeState, p.ChecksTotal, p.ChecksDone = ChecksNone, "CLEAN", 0, 0 })
	if err != nil || len(th.Decisions) != 1 {
		t.Fatalf("an ask in a repo with no CI: %v %+v", err, th.Decisions)
	}
}

// V0.136: GitHub reads the asked head as BLOCKED while its checks are
// green. The owner is not asked; the agent hears the rule GitHub enforces
// and its next step, as for a CI failure.
func TestBlockedMergeAskTellsTheAgentTheRule(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		pr        func(*PR)
		why, next string
	}{
		{"review", func(p *PR) { p.Review = "REVIEW_REQUIRED" }, "a review is required by the rules of main", "gh pr edit --add-reviewer"},
		{"signed", func(p *PR) { p.Rules = []string{"required_signatures"} }, "the rules of main require signed commits", "sign the commits"},
		{"required check", func(p *PR) { p.Rules, p.MissingChecks = []string{"required_status_checks"}, []string{"build"} }, "a required check has not reported on 7333251: build", "make the required checks run"},
		{"merge queue", func(p *PR) { p.Rules = []string{"merge_queue"} }, "main merges through a merge queue", "merge queue on GitHub"},
		{"classic protection", func(p *PR) { p.Protected = true }, "the branch protection of main blocks it", "admin rights"},
	}
	for _, c := range cases {
		th, _ := heldAsk(t, now)
		at := now.Add(5 * time.Minute)
		th, _, res, err := Reduce(th, &View{}, askPR(at, func(p *PR) {
			p.Checks, p.MergeState, p.ChecksDone, p.Base = ChecksPass, "BLOCKED", 5, "main"
			c.pr(p)
		}), at)
		if err != nil {
			t.Fatal(err)
		}
		if len(th.Decisions) != 0 || len(th.HeldAsks) != 0 || Compute(th, &View{}, at).State == Asking {
			t.Errorf("%s: the owner is asked: %+v", c.name, th.Decisions)
			continue
		}
		var said string
		for _, n := range resultNotices(Reduced{Before: th, After: th, Events: res.Events}) {
			said += n.What
		}
		if !strings.Contains(said, c.why) || !strings.Contains(said, "Next: ") || !strings.Contains(said, c.next) {
			t.Errorf("%s: the agent hears %q", c.name, said)
		}
	}
}

// V0.review-threads: a pull_request rule asks for a review only when it
// requires approvals. #1008 on 20 Nov: green, BLOCKED, review decision
// empty, a pull_request rule with 0 approvals and thread resolution on, and
// one unresolved bot review thread. Fyr said a review was required, twice,
// and the agent and the owner (the PR's author, who cannot approve it)
// chased an approval nobody needed.
func TestBlockedMergeAskNamesReviewThreadsOrApproval(t *testing.T) {
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	rules := []string{"pull_request", "required_status_checks"}
	cases := []struct {
		name      string
		pr        func(*PR)
		why, next string
		not       string
	}{
		{"#1008: 0 approvals, an unresolved thread", func(p *PR) {
			p.Review, p.Rules, p.Approvals, p.ThreadResolution, p.UnresolvedThreads = "", rules, 0, true, 1
		}, "1 review thread is unresolved and the rules of main require every review thread resolved", "resolve it", "review is required"},
		{"0 approvals, three unresolved threads", func(p *PR) {
			p.Rules, p.ThreadResolution, p.UnresolvedThreads = rules, true, 3
		}, "3 review threads are unresolved", "answer each unresolved review thread", "review is required"},
		{"0 approvals, threads resolved", func(p *PR) {
			p.Rules, p.ThreadResolution = rules, true
		}, "the rules of main block it (pull_request, required_status_checks)", "admin rights", "review is required"},
		{"review required", func(p *PR) {
			p.Review, p.Rules, p.ThreadResolution, p.UnresolvedThreads = "REVIEW_REQUIRED", rules, true, 2
		}, "a review is required by the rules of main", "the PR's author cannot approve their own PR", "unresolved"},
		{"1 approval, none given", func(p *PR) { p.Rules, p.Approvals = rules, 1 }, "a review is required by the rules of main", "the PR's author cannot approve their own PR", ""},
	}
	for _, c := range cases {
		th, _ := heldAsk(t, now)
		at := now.Add(5 * time.Minute)
		th, _, res, err := Reduce(th, &View{}, askPR(at, func(p *PR) {
			p.Checks, p.MergeState, p.ChecksDone, p.Base = ChecksPass, "BLOCKED", 5, "main"
			c.pr(p)
		}), at)
		if err != nil {
			t.Fatal(err)
		}
		if len(th.Decisions) != 0 || len(th.HeldAsks) != 0 {
			t.Errorf("%s: the owner is asked: %+v", c.name, th.Decisions)
			continue
		}
		var said string
		for _, n := range resultNotices(Reduced{Before: th, After: th, Events: res.Events}) {
			said += n.What
		}
		if !strings.Contains(said, c.why) || !strings.Contains(said, c.next) || (c.not != "" && strings.Contains(said, c.not)) {
			t.Errorf("%s: the agent hears %q", c.name, said)
		}
	}
	// an approved PR under a rule that requires one is not held for a review
	why, _ := blockedWhy(&PR{Base: "main", Review: "APPROVED", Rules: rules, Approvals: 1, ThreadResolution: true, UnresolvedThreads: 1})
	if !strings.Contains(why, "1 review thread is unresolved") {
		t.Errorf("approved with a thread open: %q", why)
	}
	// GitHub's refusal for an unresolved conversation
	if next := refusedNext(&PR{Base: "main"}, "A conversation must be resolved before this pull request can be merged."); !strings.Contains(next, "resolve it") {
		t.Errorf("refused for a conversation: %q", next)
	}
}

// V0.136: GitHub refuses a merge Fyr runs (here the owner's yes to a green
// ask). The change carries GitHub's words, so the card shows them and
// Stopped on a failure applies; the agent hears them once, with the next
// step. A push clears it.
func TestRefusedMergeTellsTheAgent(t *testing.T) {
	now := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	th, _ := heldAsk(t, now)
	green := now.Add(5 * time.Minute)
	th, _, _, err := Reduce(th, &View{}, askPR(green, func(p *PR) { p.Checks, p.MergeState, p.Base = ChecksPass, "CLEAN", "main" }), green)
	if err != nil || len(th.Decisions) != 1 {
		t.Fatalf("setup: %v %+v", err, th.Decisions)
	}
	th, _, res, err := Reduce(th, &View{}, Input{Source: FromOwner, Kind: "thread.answer", Thread: "t1", Args: Args{"decisionId": "d_merge", "allow": true}}, green)
	if err != nil || len(res.Effects) != 1 {
		t.Fatalf("yes: %v %+v", err, res.Effects)
	}
	ghErr := "GraphQL: Base branch policy prohibits the merge. (mergePullRequest)"
	at := green.Add(10 * time.Second)
	th, _, res, err = Reduce(th, &View{}, Input{Source: FromEffect("fx1"), Kind: KindEffectResult, Thread: "t1",
		Args: EffectResult{Effect: res.Effects[0], OK: false, Text: ghErr}}, at)
	if err != nil {
		t.Fatal(err)
	}
	k := condition(&th.Changes[0], CondMerge)
	if k == nil || k.Status != CondFail || !strings.Contains(k.Reason, "Base branch policy prohibits the merge") {
		t.Fatalf("the card does not show the refusal: %+v", th.Changes[0].Conditions)
	}
	notices := resultNotices(Reduced{Before: th, After: th, Events: res.Events})
	if len(notices) != 1 || !strings.Contains(notices[0].What, "The merge of PR #846 failed: GraphQL: Base branch policy prohibits the merge") || !strings.Contains(notices[0].What, "Next: ") {
		t.Fatalf("the agent hears %+v", notices)
	}
	// a push: the refusal was about the old head
	push := at.Add(time.Minute)
	th, _, _, _ = Reduce(th, &View{}, askPR(push, func(p *PR) { p.Head = "9999999aaaabbbb" }), push)
	if condition(&th.Changes[0], CondMerge) != nil {
		t.Fatal("a push kept the refused merge on the card")
	}
}
