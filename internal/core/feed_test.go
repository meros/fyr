package core

import (
	"slices"
	"strings"
	"testing"
	"time"
)

const fprURL = "https://github.com/o/r/pull/7"

// feedThread is an idle thread with an open PR change, its feed empty.
func feedThread() *Thread {
	th := &Thread{ID: "t", Title: "x"}
	ChangePRObserved(th, PR{URL: fprURL, Repo: "o/r", Number: 7, State: "OPEN", Checks: ChecksPending}, ct0)
	return th
}

// step reduces one Input and returns the new thread and the result.
func fstep(t *testing.T, th *Thread, in Input, now time.Time) (*Thread, Result) {
	t.Helper()
	in.Thread = th.ID
	nt, _, res, err := Reduce(th, nil, in, now)
	if err != nil {
		t.Fatalf("%s: %v", in.Kind, err)
	}
	return nt, res
}

func observePR(t *testing.T, th *Thread, p PR, now time.Time) (*Thread, Result) {
	t.Helper()
	p.URL, p.Repo, p.Number = fprURL, "o/r", 7
	return fstep(t, th, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: p}, now)
}

func take(t *testing.T, th *Thread, a FeedTake, now time.Time) (*Thread, []Notice) {
	t.Helper()
	nt, res := fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindFeedTake, Args: a}, now)
	ns, _ := res.Reply.([]Notice)
	return nt, ns
}

func wakes(res Result) bool {
	return slices.ContainsFunc(res.Effects, func(e Effect) bool { return e.Kind == EffectFeed })
}

func whats(ns []Notice) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.What
	}
	return out
}

// queued is the one pending notice with the key, or fails.
func queued(t *testing.T, th *Thread, key string) Notice {
	t.Helper()
	for _, n := range th.Feed.Queue {
		if n.Key == key {
			return n
		}
	}
	t.Fatalf("no notice %s in %+v", key, th.Feed.Queue)
	return Notice{}
}

// #47: the default notice rules of decision 0020 are a table; every row
// has its test here.
func TestNoticeRules(t *testing.T) {
	rows := map[string]func(t *testing.T){
		// The result of an effect the agent asked for, or of an action
		// anyone pressed on its records: wake.
		"result": func(t *testing.T) {
			th := feedThread()
			req := &EffectReq{Action: FyrMerge, Args: map[string]any{"pr": fprURL}}
			th, res := fstep(t, th, Input{Source: FromEffect("fx_1"), Kind: KindEffectResult, Args: EffectResult{Effect: Effect{ID: "fx_1", Kind: EffectRun, Allow: true, Request: req}, OK: true, Text: "Merged."}}, ct0)
			n := queued(t, th, "result:pr.merge:PR #7")
			if n.Urgency != Wake || n.What != "Your effect_request pr.merge on PR #7 succeeded: Merged." || !wakes(res) {
				t.Fatalf("agent's request: %+v wakes=%v", n, wakes(res))
			}
			c := &th.Changes[0]
			spec := ActionSpec{ID: "e2e", Label: "Run e2e", Run: RunScript, Argv: []string{"make", "e2e"}}
			press := &Press{On: c.ID, Title: `the change "x"`, Action: spec}
			th, _ = fstep(t, th, Input{Source: FromEffect("fx_2"), Kind: KindEffectResult, Args: EffectResult{Effect: Effect{ID: "fx_2", Kind: EffectScript, Press: press}, OK: false, Exit: 2, Text: "ok 1\nFAIL login"}}, ct0)
			n = queued(t, th, "result:"+c.ID+":e2e")
			if n.Urgency != Wake || n.About != "change:"+c.ID || !strings.HasPrefix(n.What, `The owner ran "Run e2e" on the change "x": it failed (exit 2): ok 1`) {
				t.Fatalf("owner's press: %+v", n)
			}
			// The owner declining the agent's request is its result too.
			th2, _ := fstep(t, feedThread(), Input{Source: FromEffect("fx_3"), Kind: KindEffectResult, Args: EffectResult{Effect: Effect{ID: "fx_3", Kind: EffectRun, Request: req}, OK: false}}, ct0)
			if n := queued(t, th2, "result:pr.merge:PR #7"); !strings.Contains(n.What, "was declined by the owner") {
				t.Fatalf("declined: %+v", n)
			}
		},
		// SPEC 0.5 V0.86: a slot the agent queued for is its own: wake.
		"slot": func(t *testing.T) {
			th, res := fstep(t, feedThread(), Input{Source: Observer(ObsHost), Kind: KindSlotGranted, Args: "test"}, ct0)
			n := queued(t, th, "slot")
			if n.Urgency != Wake || n.What != "A test slot is yours now: run the job, then call slot_release." || !wakes(res) {
				t.Fatalf("slot: %+v wakes=%v", n, wakes(res))
			}
		},
		// The agent's own watch fired: what it asked for, so wake.
		"watch": func(t *testing.T) {
			th := feedThread()
			at := ct0.Add(time.Hour)
			th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", When: When{Kind: WhenAt, At: &at}, Then: "notify", Note: "check the deploy"}}, ct0)
			if len(th.Feed.Queue) != 0 {
				t.Fatalf("arming queued %+v", th.Feed.Queue)
			}
			th, res := fstep(t, th, Input{Source: Observer(ObsClock), Kind: KindClock}, at)
			n := queued(t, th, "rule:w_1")
			if n.Urgency != Wake || n.Self || n.What != "Your watch w_1 fired: check the deploy." || !wakes(res) {
				t.Fatalf("watch: %+v", n)
			}
		},
		// A fact that makes the change fail or block: wake. A closed PR is
		// done, not a failure: next (V0.95 (1)).
		"fact.blocks": func(t *testing.T) {
			th, res := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksFail, Conflict: true, Review: "CHANGES_REQUESTED"}, ct0)
			ci := queued(t, th, "cond:"+th.Changes[0].ID+":checks")
			if ci.Urgency != Wake || ci.What != "CI on PR #7: failed." || !wakes(res) {
				t.Fatalf("CI: %+v", ci)
			}
			if n := queued(t, th, "cond:"+th.Changes[0].ID+":conflict"); n.What != "Merge conflicts on PR #7: conflicting." {
				t.Fatalf("conflict: %+v", n)
			}
			th, _ = observePR(t, feedThread(), PR{State: "CLOSED", Checks: ChecksPending}, ct0)
			if n := queued(t, th, "phase:"+th.Changes[0].ID); n.Urgency != Next || n.What != "PR #7: closed." {
				t.Fatalf("closed: %+v", n)
			}
		},
		// A fact that moves the change forward: next. Coalesced with what is
		// pending on the same key, keeping the earlier delivery.
		"fact.moves": func(t *testing.T) {
			th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksFail}, ct0)
			th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPass}, ct0.Add(time.Minute))
			ci := queued(t, th, "cond:"+th.Changes[0].ID+":checks")
			if ci.What != "CI on PR #7: failed → passed." || ci.Urgency != Wake || len(th.Feed.Queue) != 1 {
				t.Fatalf("coalesced: %+v", th.Feed.Queue)
			}
			th, _ = observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass}, ct0)
			th, _ = observePR(t, th, PR{State: "MERGED", Checks: ChecksPass, MergeSHA: "m1"}, ct0.Add(time.Minute))
			if n := queued(t, th, "cond:"+th.Changes[0].ID+":checks"); n.Urgency != Next || n.What != "CI on PR #7: passed." {
				t.Fatalf("passed: %+v", n)
			}
			if n := queued(t, th, "phase:"+th.Changes[0].ID); n.Urgency != Next || n.What != "PR #7: merged." {
				t.Fatalf("merged: %+v", n)
			}
		},
		// The view or the changes not touched in this run: stop, taken at
		// the run's end in one reason.
		"upkeep.stop": func(t *testing.T) {
			th := feedThread()
			th.Live = &LiveView{Enabled: true}
			OpenChange(th, ChangeOpen{Key: "k8s:x", Kind: KindInfra, Title: "x"}, ByAgent, ct0)
			RunStarted(th, ct0.Add(time.Minute))
			_, ns := take(t, th, FeedTake{At: AtStop, View: true, Changes: true}, ct0.Add(2*time.Minute))
			if got := whats(ns); !slices.Equal(got, []string{ViewStopText, ChangesStopText}) || ns[0].Urgency != Stop {
				t.Fatalf("stop: %q", got)
			}
			if _, ns := take(t, th, FeedTake{At: AtStop}, ct0.Add(2*time.Minute)); len(ns) != 0 {
				t.Fatalf("a process without the tools was asked: %q", whats(ns))
			}
			if _, ns := take(t, th, FeedTake{At: AtTool, View: true, Changes: true}, ct0.Add(2*time.Minute)); len(ns) != 0 {
				t.Fatalf("stop notices at a tool call: %q", whats(ns))
			}
			ViewChecked(th, ct0.Add(90*time.Second))
			ChangesChecked(th, ct0.Add(90*time.Second))
			if _, ns := take(t, th, FeedTake{At: AtStop, View: true, Changes: true}, ct0.Add(2*time.Minute)); len(ns) != 0 {
				t.Fatalf("touched this run: %q", whats(ns))
			}
		},
		// A long turn without touching the view or the changes: a reminder
		// at a tool call, at most one per ReminderAfter.
		"upkeep.reminder": func(t *testing.T) {
			th := feedThread()
			th.Live = &LiveView{Enabled: true}
			RunStarted(th, ct0)
			a := FeedTake{At: AtTool, View: true}
			if _, ns := take(t, th, a, ct0.Add(ReminderAfter-time.Second)); len(ns) != 0 {
				t.Fatal("due before the interval")
			}
			th, ns := take(t, th, a, ct0.Add(ReminderAfter))
			if !slices.Equal(whats(ns), []string{ViewReminderText}) {
				t.Fatalf("not due after the interval: %q", whats(ns))
			}
			if _, ns := take(t, th, a, ct0.Add(ReminderAfter+time.Minute)); len(ns) != 0 {
				t.Fatal("a second reminder within the interval")
			}
			ViewChecked(th, ct0.Add(ReminderAfter+3*time.Minute))
			if _, ns := take(t, th, a, ct0.Add(2*ReminderAfter+time.Minute)); len(ns) != 0 {
				t.Fatal("due although the view changed 2 minutes ago")
			}
			if _, ns := take(t, th, FeedTake{At: AtTool}, ct0.Add(time.Hour)); len(ns) != 0 {
				t.Fatal("due without the view's tools")
			}
			RunEnded(th, nil, ct0.Add(time.Hour))
			if _, ns := take(t, th, a, ct0.Add(2*time.Hour)); len(ns) != 0 {
				t.Fatal("due outside a run")
			}
		},
		// A change by the owner to the thread's settings, project or settle
		// #35: a host record the thread shows merged or went: next. A change
		// of the thread that follows the record speaks for it instead.
		"record": func(t *testing.T) {
			was := Change{ID: "c_rec", Key: "git:github.com/o/r#feat", Kind: KindCode, Branch: "feat", Phase: PhaseWorking, PhaseBy: ByObserved, Targets: []string{"repo:github.com/o/r"}}
			merged := was
			merged.Phase = PhaseLanded
			gone := was
			gone.Phase = PhaseAbandoned
			src := Observer(ObsGit)
			th := &Thread{ID: "t"}
			th, _ = fstep(t, th, Input{Source: src, Kind: KindChangeObserved, Args: ChangeObservedArgs{After: was}}, ct0)
			if len(th.Refs) != 1 || len(th.Feed.Queue) != 0 {
				t.Fatalf("a new record: refs %+v, queue %+v", th.Refs, th.Feed.Queue)
			}
			th, res := fstep(t, th, Input{Source: src, Kind: KindChangeObserved, Args: ChangeObservedArgs{Before: &was, After: merged}}, ct0)
			if n := queued(t, th, "phase:c_rec"); n.Urgency != Next || n.What != "Branch feat: merged." || wakes(res) || !th.Refs[0].Ended {
				t.Fatalf("merged: %+v, refs %+v", n, th.Refs)
			}
			th = &Thread{ID: "t2"}
			th, _ = fstep(t, th, Input{Source: src, Kind: KindChangeObserved, Args: ChangeObservedArgs{After: was}}, ct0)
			th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindChangeOpen, Args: ChangeOpenArgs{Open: ChangeOpen{Key: "fix", Kind: KindCode, Title: "Fix", Branch: "feat"}}}, ct0)
			th, _ = fstep(t, th, Input{Source: src, Kind: KindChangeObserved, Args: ChangeObservedArgs{Before: &was, After: gone}}, ct0)
			c := FindChange(th, "fix")
			if c.Observed != "c_rec" || c.Phase != PhaseAbandoned || len(th.Feed.Queue) != 1 || th.Feed.Queue[0].Key != "phase:"+c.ID || th.Feed.Queue[0].Urgency != Next {
				t.Fatalf("follower: %+v, queue %+v", c, th.Feed.Queue)
			}
		},
		// state: next, which wakes nobody (V0.95 (1)); unsettle too.
		"owner": func(t *testing.T) {
			th, res := fstep(t, feedThread(), Input{Source: FromOwner, Kind: "thread.rename", Args: Args{"title": "Login fix"}}, ct0)
			if n := queued(t, th, "owner:title"); n.Urgency != Next || n.What != `The owner renamed this thread to "Login fix".` || wakes(res) {
				t.Fatalf("rename: %+v", n)
			}
			th, _ = take(t, th, FeedTake{At: AtIdle}, ct0)
			th, res = fstep(t, th, Input{Source: FromOwner, Kind: "thread.settle", Args: Args{"now": true}}, ct0)
			if n := queued(t, th, "owner:settle"); n.What != "The owner set this thread: settled." || wakes(res) {
				t.Fatalf("settle: %+v wakes=%v", n, wakes(res))
			}
			if _, ns := take(t, th, FeedTake{At: AtIdle}, ct0); len(ns) != 0 {
				t.Fatalf("a settled thread was woken: %q", whats(ns))
			}
			th, res = fstep(t, th, Input{Source: FromOwner, Kind: "thread.unsettle"}, ct0)
			if n := queued(t, th, "owner:settle"); n.What != "The owner set this thread: settled → not settled." || wakes(res) {
				t.Fatalf("revived: %+v wakes=%v", n, wakes(res))
			}
			// V0.89: Settle on an idle thread with an open PR is "settle when
			// merged", which leaves the thread awake; its notice never wakes it.
			th = feedThread()
			th.MainChange = th.Changes[0].ID
			th, res = fstep(t, th, Input{Source: FromOwner, Kind: "thread.settle"}, ct0)
			if n := queued(t, th, "owner:settle"); n.What != "The owner set this thread: settles when its PR merges." || !n.Self || wakes(res) {
				t.Fatalf("settle when merged: %+v wakes=%v", n, wakes(res))
			}
			if _, ns := take(t, th, FeedTake{At: AtIdle}, ct0); len(ns) != 0 {
				t.Fatalf("the owner's settle woke the agent: %q", whats(ns))
			}
			th, res = fstep(t, th, Input{Source: FromOwner, Kind: "thread.unsettle"}, ct0)
			if n := queued(t, th, "owner:settle"); n.Self || wakes(res) {
				t.Fatalf("unsettle after settle when merged: %+v wakes=%v", n, wakes(res))
			}
		},
	}
	for _, r := range NoticeRules {
		f := rows[r.Name]
		if f == nil {
			t.Errorf("notice rule %s has no test", r.Name)
			continue
		}
		t.Run(r.Name, f)
	}
}

// #47: what the agent's own tool call causes never wakes it (no loop): its
// watch that its own change_status makes true is taken at the next tool
// call, not as a turn of its own; a failure that arrives wakes it, and
// the idle message carries both, the most urgent first.
func TestFeedLoopGuard(t *testing.T) {
	th := feedThread()
	th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindChangeOpen, Args: ChangeOpenArgs{Open: ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Doc"}}}, ct0)
	c := FindChange(th, "doc:x")
	th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindRuleArm, Args: RuleArmArgs{ID: "w_1", When: When{Kind: WhenReached, Change: c.ID, Phase: PhaseReview}, Then: "notify", Note: "ask for review"}}, ct0)
	th, res := fstep(t, th, Input{Source: FromAgent, Kind: KindChangeStatus, Args: ChangeStatusArgs{Change: c.ID, Update: ChangeUpdate{Phase: PhaseReview}}}, ct0)
	if n := queued(t, th, "rule:w_1"); !n.Self || wakes(res) {
		t.Fatalf("own watch: %+v wakes=%v", n, wakes(res))
	}
	if _, ns := take(t, th, FeedTake{At: AtIdle}, ct0); len(ns) != 0 {
		t.Fatalf("the agent's own call woke it: %q", whats(ns))
	}
	if _, ns := take(t, th, FeedTake{At: AtTool}, ct0); len(ns) != 1 {
		t.Fatalf("not taken at the next tool call: %q", whats(ns))
	}
	th, res = observePR(t, th, PR{State: "OPEN", Checks: ChecksFail}, ct0)
	if !wakes(res) {
		t.Fatal("a fact did not wake the agent")
	}
	th, ns := take(t, th, FeedTake{At: AtIdle}, ct0)
	if got := whats(ns); len(got) != 2 || ns[0].Key != "rule:w_1" || got[1] != "CI on PR #7: failed." {
		t.Fatalf("idle delivery: %q", got)
	}
	if len(th.Feed.Queue) != 0 {
		t.Fatalf("left in the queue: %+v", th.Feed.Queue)
	}
	if NoticeText(ns) != "[Fyr] Your watch w_1 fired: ask for review.\n[Fyr] CI on PR #7: failed." {
		t.Fatalf("text %q", NoticeText(ns))
	}
	// The most urgent first, whatever the order they came in.
	th, _ = observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksPass}, ct0)
	req := &EffectReq{Action: FyrMerge, Args: map[string]any{"pr": fprURL}}
	th, _ = fstep(t, th, Input{Source: FromEffect("fx_1"), Kind: KindEffectResult, Args: EffectResult{Effect: Effect{ID: "fx_1", Kind: EffectRun, Allow: true, Request: req}, OK: true}}, ct0)
	if _, ns := take(t, th, FeedTake{At: AtIdle}, ct0); len(ns) != 2 || ns[0].Urgency != Wake || ns[1].Urgency != Next {
		t.Fatalf("order: %+v", ns)
	}
}

// #47: the agent's own reports, git facts of its checkout, pending checks
// and records it touched are silent: nothing is queued.
func TestFeedSilent(t *testing.T) {
	th := feedThread()
	th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindChangeOpen, Args: ChangeOpenArgs{Open: ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Doc"}}}, ct0)
	th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindChangeStatus, Args: ChangeStatusArgs{Change: "doc:x", Update: ChangeUpdate{Phase: PhaseLanded, Conditions: []Condition{{Type: "lint", Status: CondFail}}}}}, ct0)
	RunStarted(th, ct0)
	th, _ = fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{Git: GitFacts{Branch: "feat", Ahead: 2, Unpushed: 2, Dirty: 1, Head: "abc"}}}, ct0)
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPending}, ct0)
	if len(th.Feed.Queue) != 0 {
		t.Fatalf("queued %+v", th.Feed.Queue)
	}
}

// #47: a thread saved before the feed loads with an empty queue, and the
// queue survives a store round trip.
func TestFeedStored(t *testing.T) {
	th, _ := observePR(t, feedThread(), PR{State: "OPEN", Checks: ChecksFail}, ct0)
	c := CloneThread(th)
	if len(c.Feed.Queue) != 1 || c.Feed.Queue[0].What != "CI on PR #7: failed." {
		t.Fatalf("round trip: %+v", c.Feed)
	}
	old := CloneThread(feedThread())
	if len(old.Feed.Queue) != 0 {
		t.Fatalf("old shape: %+v", old.Feed)
	}
}

// #47 for #17: what the feed costs the agent in a typical run of a PR:
// opened, CI fails, a fix, CI passes, the owner merges, it deploys. It
// reports characters per notice and notices per run, and keeps each notice
// one short sentence.
func TestFeedCost(t *testing.T) {
	th := feedThread()
	var delivered []Notice
	deliver := func(at Point) {
		var ns []Notice
		th, ns = take(t, th, FeedTake{At: at, View: true, Changes: true}, ct0)
		delivered = append(delivered, ns...)
	}
	RunStarted(th, ct0)
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksFail}, ct0)
	deliver(AtTool) // the agent is working: context at its next tool call
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPending, Head: "h2"}, ct0)
	th, _ = observePR(t, th, PR{State: "OPEN", Checks: ChecksPass, Head: "h2", Review: "APPROVED"}, ct0)
	th, _ = fstep(t, th, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}, ct0)
	deliver(AtIdle) // passed and approved: one turn
	req := &EffectReq{Action: FyrMerge, Args: map[string]any{"pr": fprURL, "method": "squash"}}
	th, _ = fstep(t, th, Input{Source: FromEffect("fx_1"), Kind: KindEffectResult, Args: EffectResult{Effect: Effect{ID: "fx_1", Kind: EffectRun, Allow: true, ByOwner: true, Request: req}, OK: true, Text: "Merged #7."}}, ct0)
	th, _ = observePR(t, th, PR{State: "MERGED", Checks: ChecksPass, MergeSHA: "m1"}, ct0)
	deliver(AtIdle) // the merge and its result: one turn
	c := th.Changes[0]
	th, _ = fstep(t, th, Input{Source: Observer(ObsDeploy), Kind: KindDeployObserved, Args: DeployObservedArgs{Change: c.ID, Obs: DeployObs{Deployments: []Deployment{{Env: "production", State: "success", Production: true}}}}}, ct0)
	deliver(AtIdle)
	chars := 0
	for _, n := range delivered {
		chars += len(NoticeText([]Notice{n}))
		if len(n.What) > 200 {
			t.Errorf("a notice longer than 200 characters: %q", n.What)
		}
	}
	t.Logf("feed cost: %d notices, %d characters, %d per notice on average", len(delivered), chars, chars/max(len(delivered), 1))
	for _, n := range delivered {
		t.Logf("  %-6s %s", n.Urgency, n.What)
	}
	if len(delivered) > 8 {
		t.Errorf("%d notices for one PR's life; coalescing failed", len(delivered))
	}
}
