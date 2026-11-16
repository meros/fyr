// SPEC V0.98: a finished change is done (decision 0024 (3), slice 4).
package core

import (
	"slices"
	"testing"
	"time"
)

var dt0 = time.Date(2026, 11, 16, 10, 42, 0, 0, time.UTC)

func dstep(t *testing.T, th *Thread, src Source, kind string, args any, now time.Time) *Thread {
	t.Helper()
	th, _ = fstep(t, th, Input{Source: src, Kind: kind, Args: args}, now)
	return th
}

// SPEC V0.98 (1): done by kind and phase; Ended keeps its meaning.
func TestIsDoneByKindAndPhase(t *testing.T) {
	merged := &PR{URL: "https://github.com/o/r/pull/1", Repo: "o/r", Number: 1, State: "MERGED", Checks: ChecksPass}
	cases := []struct {
		name   string
		c      Change
		watch  bool
		done   bool
		reason string
	}{
		{"code working", Change{Kind: KindCode, Phase: PhaseWorking}, false, false, ""},
		{"code review", Change{Kind: KindCode, Phase: PhaseReview}, false, false, ""},
		{"code landed, no PR", Change{Kind: KindCode, Phase: PhaseLanded}, false, true, "landed"},
		{"code merged", Change{Kind: KindCode, Phase: PhaseLanded, PR: merged}, false, true, "merged"},
		{"code merged, deploy watch armed", Change{Kind: KindCode, Phase: PhaseLanded, PR: merged}, true, false, ""},
		{"code live, deploy watch armed", Change{Kind: KindCode, Phase: PhaseLive, PR: merged}, true, true, "live"},
		{"doc landed", Change{Kind: KindDoc, Phase: PhaseLanded}, false, true, "landed"},
		{"infra live", Change{Kind: KindInfra, Phase: PhaseLive}, false, true, "live"},
		{"infra review", Change{Kind: KindInfra, Phase: PhaseReview}, false, false, ""},
		{"abandoned", Change{Kind: KindOther, Phase: PhaseAbandoned}, false, true, "abandoned"},
		{"reverted", Change{Kind: KindCode, Phase: PhaseReverted}, false, true, "reverted"},
	}
	for _, k := range cases {
		th := &Thread{ID: "t"}
		c := k.c
		c.ID, c.Key, c.Title, c.PhaseBy = "c1", "k1", k.name, ByObserved
		th.Changes = []Change{c}
		if k.watch {
			th.Rules = []Rule{{ID: deployRuleID("c1"), Purpose: PurposeDeploy, About: "c1", Status: RuleArmed}}
		}
		markDone(nil, th, dt0)
		got := &th.Changes[0]
		if got.IsDone() != k.done {
			t.Errorf("%s: done %v, want %v", k.name, got.IsDone(), k.done)
			continue
		}
		if k.done && (got.Done.Reason != k.reason || !got.Done.At.Equal(dt0)) {
			t.Errorf("%s: mark %+v, want reason %q", k.name, got.Done, k.reason)
		}
		if got.Phase.Ended() != (c.Phase == PhaseAbandoned || c.Phase == PhaseReverted) {
			t.Errorf("%s: Ended changed meaning", k.name)
		}
	}
}

// SPEC V0.98 (1): a PR merged into a branch that is not the default one is
// not done until its merge commit is in the default branch; until then the
// poller keeps reading it. When that read failed, the base change decides.
func TestStackedPRNotDoneUntilInDefault(t *testing.T) {
	no, yes := false, true
	pr := func(in *bool) PR {
		at := dt0
		return PR{URL: "https://github.com/o/r/pull/2", Repo: "o/r", Number: 2, State: "MERGED", Checks: ChecksPass, Branch: "feat/b", Base: "feat/a", Stacked: true, InDefault: in, CheckedAt: &at}
	}
	th := &Thread{ID: "t"}
	th = dstep(t, th, Observer(ObsGitHub), KindPRObserved, pr(&no), dt0)
	c := FindChange(th, "pr:o/r#2")
	if c == nil || c.Phase != PhaseLanded || c.IsDone() {
		t.Fatalf("a stacked PR outside the default branch is done: %+v", c)
	}
	if !slices.Contains(PRURLs(th, dt0.Add(48*time.Hour)), pr(nil).URL) {
		t.Fatal("the stacked PR is no longer polled")
	}
	th = dstep(t, th, Observer(ObsGitHub), KindPRObserved, pr(&yes), dt0.Add(time.Hour))
	if c := FindChange(th, "pr:o/r#2"); !c.IsDone() || c.Done.Reason != "merged" {
		t.Fatalf("in the default branch and not done: %+v", c.Done)
	}
	if slices.Contains(PRURLs(th, dt0.Add(48*time.Hour)), pr(nil).URL) {
		t.Fatal("a done stacked PR is still polled")
	}

	// The read failed: done when the change of the base branch is done.
	th = &Thread{ID: "t"}
	th = ocOpen(t, th, ChangeOpen{Key: "base", Kind: KindCode, Title: "Base", Branch: "feat/a", Targets: []string{"repo:github.com/o/r"}}, "")
	th = dstep(t, th, Observer(ObsGitHub), KindPRObserved, pr(nil), dt0)
	if c := FindChange(th, "pr:o/r#2"); c.IsDone() {
		t.Fatal("done while its base change is open")
	}
	th = dstep(t, th, FromOwner, KindChangeDone, Args{"change": "base"}, dt0.Add(time.Minute))
	if c := FindChange(th, "pr:o/r#2"); !c.IsDone() {
		t.Fatal("not done after its base change is done")
	}
}

// SPEC V0.98 (1): a merged change whose deploy watch is armed stays until it
// is live or the watch ends.
func TestDoneWaitsForDeployWatch(t *testing.T) {
	merged := func(at time.Time) PR {
		return PR{URL: "https://github.com/meros/fyr/pull/9", Repo: "meros/fyr", Number: 9, State: "MERGED", Checks: ChecksPass, MergeSHA: "m9", MergedAt: &at, CheckedAt: &at}
	}
	th := dstep(t, &Thread{ID: "t"}, Observer(ObsGitHub), KindPRObserved, merged(dt0), dt0)
	c := FindChange(th, "pr:meros/fyr#9")
	if FindRule(th, deployRuleID(c.ID)) == nil {
		t.Fatal("setup: no deploy watch")
	}
	if c.IsDone() {
		t.Fatal("done while the deploy watch is armed")
	}
	live := dstep(t, th, Observer(ObsDeploy), KindDeployObserved, DeployObservedArgs{Change: c.ID, Obs: DeployObs{Deployments: []Deployment{{Env: "production", Production: true, State: "success"}}}}, dt0.Add(time.Minute))
	if c := FindChange(live, c.ID); c.Phase != PhaseLive || !c.IsDone() || c.Done.Reason != "live" {
		t.Fatalf("live and not done: %s %+v", c.Phase, c.Done)
	}
	ended := dstep(t, th, Observer(ObsDeploy), KindDeployObserved, DeployObservedArgs{Change: c.ID, Obs: DeployObs{None: true}}, dt0.Add(time.Minute))
	if c := FindChange(ended, c.ID); c.Phase != PhaseLanded || !c.IsDone() {
		t.Fatalf("the watch ended and the change is not done: %s %+v", c.Phase, c.Done)
	}
}

// SPEC V0.98 (4): touchedAt moves with the change's facts, not with a poll
// that reads the same PR; the owner's Done finishes an open change.
func TestUntouchedAndOwnerDoneOnThreadChange(t *testing.T) {
	open := func(at time.Time, checks Checks) PR {
		return PR{URL: "https://github.com/o/r/pull/3", Repo: "o/r", Number: 3, State: "OPEN", Checks: checks, Branch: "x", CheckedAt: &at}
	}
	th := dstep(t, &Thread{ID: "t"}, Observer(ObsGitHub), KindPRObserved, open(dt0, ChecksPass), dt0)
	t3 := dt0.Add(72 * time.Hour)
	th = dstep(t, th, Observer(ObsGitHub), KindPRObserved, open(t3, ChecksPass), t3)
	c := FindChange(th, "pr:o/r#3")
	if c.TouchedAt == nil || !c.TouchedAt.Equal(dt0) {
		t.Fatalf("a poll that read the same PR touched it: %v", c.TouchedAt)
	}
	t4 := t3.Add(time.Minute)
	th = dstep(t, th, Observer(ObsGitHub), KindPRObserved, open(t4, ChecksFail), t4)
	if c := FindChange(th, "pr:o/r#3"); !c.TouchedAt.Equal(t4) {
		t.Fatalf("failed checks did not touch it: %v", c.TouchedAt)
	}
	th = dstep(t, th, FromOwner, KindChangeDone, Args{"change": "pr:o/r#3"}, t4)
	c = FindChange(th, "pr:o/r#3")
	if !c.IsDone() || c.Done.By != ByOwner || c.Done.Reason != DoneByOwner || c.Phase != PhaseReview {
		t.Fatalf("owner Done: %s %+v", c.Phase, c.Done)
	}
	if c.Blocking() != "" {
		t.Fatalf("a change the owner marked done blocks: %q", c.Blocking())
	}
	if slices.Contains(PRURLs(th, t4), "https://github.com/o/r/pull/3") {
		t.Fatal("a change the owner marked done is still polled")
	}
	// A later poll does not take the owner's mark away.
	th = dstep(t, th, Observer(ObsGitHub), KindPRObserved, open(t4.Add(time.Minute), ChecksPass), t4.Add(time.Minute))
	if !FindChange(th, "pr:o/r#3").IsDone() {
		t.Fatal("a poll undid the owner's Done")
	}
	if _, _, _, err := Reduce(th, nil, Input{Source: FromAgent, Kind: KindChangeDone, Thread: "t", Args: Args{"change": "pr:o/r#3"}}, t4); err == nil {
		t.Fatal("the agent sent change.done")
	}
}

// SPEC V0.98 (4): the owner's Done on a host record, the one owner Input a
// record takes; an observed record is touched when its facts move.
func TestOwnerDoneOnHostRecord(t *testing.T) {
	rs, rec := observe(t, nil, GitFacts{Repo: ocRepo, Path: "/w/fyr", Branch: "spike", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "s1"}, dt0)
	if rec.TouchedAt == nil || !rec.TouchedAt.Equal(dt0) {
		t.Fatalf("a new record is not touched: %v", rec.TouchedAt)
	}
	if _, _, _, err := ReduceRecord(rs, Input{Source: FromOwner, Kind: KindGitObserved, Args: GitFacts{Repo: ocRepo, Branch: "spike"}}, dt0); err == nil {
		t.Fatal("the owner sent git facts")
	}
	if _, _, _, err := ReduceRecord(rs, Input{Source: FromAgent, Kind: KindRecordDone, Args: Args{"change": rec.ID}}, dt0); err == nil {
		t.Fatal("the agent marked a record done")
	}
	later := dt0.Add(80 * time.Hour)
	before, after, _, err := ReduceRecord(rs, Input{Source: FromOwner, Kind: KindRecordDone, Args: Args{"change": rec.ID}}, later)
	if err != nil || before != rec || after == nil {
		t.Fatalf("record.done: %v %v", err, after)
	}
	if !after.IsDone() || after.Done.By != ByOwner || after.Done.Reason != DoneByOwner || !after.Done.At.Equal(later) {
		t.Fatalf("record mark: %+v", after.Done)
	}
	if rec.IsDone() {
		t.Fatal("the reduce changed the stored record")
	}
	// The same branch and head checked out again makes no new record.
	rs2 := []*Change{after}
	if _, again := observe(t, rs2, GitFacts{Repo: ocRepo, Path: "/w/fyr", Branch: "spike", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "s1"}, later); again != nil && again.ID != after.ID {
		t.Fatalf("a done record's branch made a new record: %+v", again)
	}
	// A record that merged is done.
	_, landed, _, _ := ReduceRecord([]*Change{rec}, Input{Source: Observer(ObsGit), Kind: KindGitEnded, Args: GitEnd{Change: rec.ID, Merged: true}}, later)
	if !landed.IsDone() || landed.Done.Reason != "merged" {
		t.Fatalf("a merged record is not done: %+v", landed.Done)
	}
}

// SPEC V0.98 (5): the PR of `gh pr create` arrives without a branch; the
// first poll reads its head and joins the agent's open change on that branch
// in the same repo: one card.
func TestGhPRCreateJoinsBranchChange(t *testing.T) {
	th := &Thread{ID: "t"}
	th = ocOpen(t, th, ChangeOpen{Key: "fyr:x", Kind: KindCode, Title: "Fix x", Branch: "fix/y", Targets: []string{"repo:" + ocRepo}}, "")
	th = ocOpen(t, th, ChangeOpen{Key: "other:x", Kind: KindCode, Title: "Other repo", Branch: "fix/y", Targets: []string{"repo:github.com/acme/app"}}, "")
	// The digest's PR: no branch.
	th = dstep(t, th, Observer(ObsProcess), KindPRObserved, PR{URL: ocPR, Repo: "meros/fyr", Number: 67, State: "OPEN", Checks: ChecksPending}, dt0)
	if len(th.Changes) != 3 {
		t.Fatalf("setup: %+v", th.Changes)
	}
	th = ocPRSeen(t, th, "OPEN", dt0.Add(time.Minute))
	if len(th.Changes) != 2 {
		t.Fatalf("two cards for one work item: %+v", th.Changes)
	}
	c := FindChange(th, "fyr:x")
	if c == nil || c.PRURL() != ocPR || c.Phase != PhaseReview || !slices.Contains(c.Aliases, "pr:meros/fyr#67") || FindChange(th, "pr:meros/fyr#67") != c {
		t.Fatalf("the PR did not join the agent's change: %+v", c)
	}
	if o := FindChange(th, "other:x"); o.PRURL() != "" {
		t.Fatal("the PR joined a change of another repo")
	}
}

// SPEC V0.98 (7): the load pass folds duplicate pairs and marks every change
// at or past landed done, except one whose deploy watch is armed.
func TestMigrateDone(t *testing.T) {
	merged := &PR{URL: "https://github.com/o/r/pull/5", Repo: "o/r", Number: 5, State: "MERGED", Checks: ChecksPass, Branch: "dup"}
	watched := &PR{URL: "https://github.com/o/r/pull/6", Repo: "o/r", Number: 6, State: "MERGED", Checks: ChecksPass, Branch: "w"}
	th := &Thread{ID: "t", Changes: []Change{
		{ID: "a", Key: "agent:dup", Kind: KindCode, Title: "Dup", Branch: "dup", Targets: []string{"repo:github.com/o/r"}, Phase: PhaseWorking, PhaseBy: ByAgent},
		{ID: "p", Key: "pr:o/r#5", Kind: KindCode, Title: "Dup PR", Branch: "dup", Targets: []string{"repo:github.com/o/r"}, Phase: PhaseLanded, PhaseBy: ByObserved, PR: merged},
		{ID: "l", Key: "doc", Kind: KindDoc, Title: "Doc", Phase: PhaseLive, PhaseBy: ByAgent},
		{ID: "w", Key: "pr:o/r#6", Kind: KindCode, Title: "Watched", Branch: "w", Phase: PhaseLanded, PhaseBy: ByObserved, PR: watched},
		{ID: "x", Key: "gone", Kind: KindOther, Title: "Gone", Phase: PhaseAbandoned, PhaseBy: ByOwner},
		{ID: "o", Key: "open", Kind: KindInfra, Title: "Open", Phase: PhaseReview, PhaseBy: ByAgent},
	}, Rules: []Rule{{ID: deployRuleID("w"), Purpose: PurposeDeploy, About: "w", Status: RuleArmed}}}
	if !MigrateDone(th, dt0) {
		t.Fatal("nothing migrated")
	}
	if FindChange(th, "pr:o/r#5") == nil || FindChange(th, "pr:o/r#5").ID != "a" || len(th.Changes) != 5 {
		t.Fatalf("the duplicate pair did not fold: %+v", th.Changes)
	}
	want := map[string]string{"a": DoneBeforeV2, "l": DoneBeforeV2, "x": "abandoned"}
	for _, c := range th.Changes {
		r, ok := want[c.ID]
		if c.IsDone() != ok || (ok && c.Done.Reason != r) {
			t.Errorf("%s (%s): done %+v, want %q", c.ID, c.Phase, c.Done, r)
		}
	}
	if MigrateDone(th, dt0) {
		t.Fatal("a second pass changed the thread")
	}
	rec := &Change{ID: "r", Kind: KindCode, Phase: PhaseLanded, PhaseBy: ByObserved}
	if !MigrateRecordDone(rec, dt0) || !rec.IsDone() || rec.Done.Reason != DoneBeforeV2 {
		t.Fatalf("a landed record is not done: %+v", rec.Done)
	}
}
