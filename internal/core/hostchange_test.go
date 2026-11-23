package core

import (
	"testing"
)

// #35: removing an action on a change drops its action:<id> condition; its
// runs stay as history. The live case: a deploy change kept action:status
// pending after the action was gone.
func TestRemoveActionDropsCondition(t *testing.T) {
	th := &Thread{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "deploy", Kind: KindHost, Title: "Deploy"}, ByAgent, ct0)
	h, _ := HolderOf(th, "deploy")
	if err := h.Declare(ActionSpec{ID: "status", Label: "Status", Run: RunScript, Argv: []string{"true"}}); err != nil {
		t.Fatal(err)
	}
	a := *h.Find("status")
	h.Started(a, ct0)
	h.Ended(a, false, 1, "", ct0)
	if condition(c, CondAction("status")) == nil {
		t.Fatal("no action condition")
	}
	if !h.Remove("status") {
		t.Fatal("not removed")
	}
	if condition(c, CondAction("status")) != nil || len(c.Runs) != 1 {
		t.Fatalf("after remove: %+v runs %d", c.Conditions, len(c.Runs))
	}
	if r := needText(th); r != "" {
		t.Fatalf("a removed action still blocks: %q", r)
	}
}

// #35: the thread's code change on a record's branch and repo follows it:
// workspace, pushed, clean and the newest revision are copied; the end of
// the branch moves it unless a PR says how it ended. A change in another
// repo does not follow.
func TestChangeFollowsRecord(t *testing.T) {
	th := &Thread{ID: "t"}
	OpenChange(th, ChangeOpen{Key: "a", Kind: KindCode, Title: "A", Branch: "feat", Targets: []string{"repo:github.com/o/r"}}, ByAgent, ct0)
	OpenChange(th, ChangeOpen{Key: "b", Kind: KindCode, Title: "B", Branch: "feat", Targets: []string{"repo:github.com/o/other"}}, ByAgent, ct0)
	rs, rec := observe(t, nil, GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "feat", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "h1"}, ct0)
	th, _ = fstep(t, th, Input{Source: Observer(ObsGit), Kind: KindChangeObserved, Args: ChangeObservedArgs{After: *rec}}, ct0)
	a, b := FindChange(th, "a"), FindChange(th, "b")
	if a.Observed != rec.ID || a.Workspace == nil || a.Workspace.Path != "/w/r" || condition(a, CondPushed).Status != CondPending || len(a.Revisions) != 1 {
		t.Fatalf("a: %+v", a)
	}
	if b.Observed != "" {
		t.Fatalf("another repo followed: %+v", b)
	}
	if shown := ShownChanges(th, func(string) (*Change, []string) { return rec, nil }); len(shown) != 2 {
		t.Fatalf("the record shows beside the change that follows it: %+v", shown)
	}
	_, landed, _, _ := ReduceRecord(rs, Input{Source: Observer(ObsGit), Kind: KindGitEnded, Args: GitEnd{Change: rec.ID, Merged: true}}, ct0)
	th, _ = fstep(t, th, Input{Source: Observer(ObsGit), Kind: KindChangeObserved, Args: ChangeObservedArgs{Before: rec, After: *landed}}, ct0)
	if a := FindChange(th, "a"); a.Phase != PhaseLanded || a.PhaseBy != ByObserved || condition(a, CondPushed) != nil {
		t.Fatalf("a after the merge: %+v", a)
	}
}

// SPEC 0.5 V0.76 (1), #51: a branch with nothing ahead of its base has nothing to land. When the base moves on and the
// branch is behind it, the branch is not "merged": it is still the branch, and the record stays in flight.
func TestRecordBehindItsBaseIsNotLanded(t *testing.T) {
	rs, rec := observe(t, nil, GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "feat", Base: "origin/main", Ahead: 1, Unpushed: 0, Head: "h1"}, ct0)
	if rec.Phase != PhaseWorking || len(rec.Revisions) != 1 || rec.Revisions[0].ID != "h1" {
		t.Fatalf("with a commit ahead: %+v", rec)
	}
	// origin/main moves to h2 and this checkout is still at h1: behind the base, not merged into it.
	_, again, _, _ := ReduceRecord(rs, Input{Source: Observer(ObsGit), Kind: KindGitObserved, Args: GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "feat", Base: "origin/main", BaseHead: "h2", Ahead: 0, Unpushed: 0, Head: "h1"}}, ct0)
	if again.Phase != PhaseWorking {
		t.Fatalf("a branch behind its base is not landed: %s", again.Phase)
	}
	// and when it catches up with a new commit, the record takes the new revision instead of ending.
	_, more, _, _ := ReduceRecord(rs, Input{Source: Observer(ObsGit), Kind: KindGitObserved, Args: GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "feat", Base: "origin/main", BaseHead: "h2", Ahead: 1, Unpushed: 0, Head: "h3"}}, ct0)
	if more.Phase != PhaseWorking || more.Revisions[len(more.Revisions)-1].ID != "h3" {
		t.Fatalf("a new commit after the base moved: %+v", more)
	}
}

// SPEC 0.5 V0.88 (1): a squash-merged branch that stays checked out is still ahead of its base. Once its record ended,
// the same head (with or without changed files) makes no new record; a new commit does.
func TestEndedBranchAtTheSameHeadMakesNoNewRecord(t *testing.T) {
	g := GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "feat", Base: "origin/main", Ahead: 1, Head: "cf50fef89f"}
	rs, rec := observe(t, nil, g, ct0)
	_, landed, _, _ := ReduceRecord(rs, Input{Source: Observer(ObsGit), Kind: KindGitEnded, Args: GitEnd{Change: rec.ID, Merged: true}}, ct0)
	rs = []*Change{landed}
	if _, again := observe(t, rs, g, ct0); again != nil {
		t.Fatalf("the merged head made a new record: %+v", again)
	}
	dirty := g
	dirty.Dirty = 2
	if _, again := observe(t, rs, dirty, ct0); again != nil {
		t.Fatalf("changed files on the merged head made a new record: %+v", again)
	}
	next := g
	next.Ahead, next.Unpushed, next.Head = 2, 1, "a1b2c3d4e5"
	_, more := observe(t, rs, next, ct0)
	if more == nil || more.ID == landed.ID || more.Phase != PhaseWorking {
		t.Fatalf("a new commit after the merge is new work: %+v", more)
	}
}

// SPEC 0.5 V0.88 (2): a merged PR ends a record only when the record's newest revision is the PR's head.
func TestMergedPRCoversOnlyItsHead(t *testing.T) {
	pr := &PR{State: "MERGED", Head: "cf50fef89f0123456789abcdef0123456789abcd"}
	rec := &Change{Revisions: []Revision{{ID: "cf50fef89f"}}}
	if !MergedPRCovers(pr, rec) {
		t.Fatal("the PR's own head is merged")
	}
	if MergedPRCovers(pr, &Change{Revisions: []Revision{{ID: "a1b2c3d4e5"}}}) {
		t.Fatal("a new commit after the merge is not that PR's merge")
	}
	if !MergedPRCovers(&PR{State: "MERGED"}, rec) || !MergedPRCovers(pr, &Change{}) {
		t.Fatal("an unknown head keeps the old rule: the branch decides")
	}
	if MergedPRCovers(&PR{State: "OPEN", Head: pr.Head}, rec) {
		t.Fatal("an open PR is not merged")
	}
}

// V0.change-worktree (3): a change whose worktree Fyr made stays Fyr's when
// it follows the record the observer reads from that worktree.
func TestFyrWorktreeStaysFyrsWhenItFollowsARecord(t *testing.T) {
	th := &Thread{ID: "t"}
	ws := &Workspace{Repo: "github.com/o/r", Path: "/w/r/.claude/worktrees/feat", Branch: "feat", Base: "origin/main", MadeByFyr: true}
	OpenChange(th, ChangeOpen{Key: "feat", Kind: KindCode, Title: "A", Branch: "feat", Targets: []string{"repo:github.com/o/r"}, Workspace: ws}, ByAgent, ct0)
	if a := FindChange(th, "feat"); a.Workspace == nil || a.Workspace == ws || !a.Workspace.MadeByFyr {
		t.Fatalf("open: %+v", a.Workspace)
	}
	_, rec := observe(t, nil, GitFacts{Repo: "github.com/o/r", Path: ws.Path, Branch: "feat", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "h1"}, ct0)
	th, _ = fstep(t, th, Input{Source: Observer(ObsGit), Kind: KindChangeObserved, Args: ChangeObservedArgs{After: *rec}}, ct0)
	if a := FindChange(th, "feat"); a.Observed != rec.ID || a.Workspace.Head != "h1" || !a.Workspace.MadeByFyr {
		t.Fatalf("after the observer: %+v", a.Workspace)
	}
}
