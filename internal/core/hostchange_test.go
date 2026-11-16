package core

import (
	"testing"
	"time"
)

// #35: a thread stored before host records keeps loading: its observed git
// change moves into a host record with the same id, and the thread gets a
// reference to it. A git change that a PR joined stays in the thread and
// follows the record. Two threads with a copy of one branch's change make
// one record, the newest copy.
func TestMigrateObserved(t *testing.T) {
	old := func(id, at string) []byte {
		return []byte(`{"id":"` + id + `","cwd":"/w/fyr","changes":[
			{"id":"c_v0","key":"git:github.com/meros/fyr#v0","kind":"code","title":"v0","branch":"v0","targets":["repo:github.com/meros/fyr"],
			 "phase":"working","phaseBy":"observed","updatedAt":"` + at + `",
			 "conditions":[{"type":"pushed","status":"pending","reason":"7 commits not pushed","source":"observed","at":"` + at + `"}],
			 "revisions":[{"id":"h7","by":"observed","at":"` + at + `"}]},
			{"id":"c_pr","key":"git:github.com/meros/fyr#feat","kind":"code","title":"Feature","branch":"feat","targets":["repo:github.com/meros/fyr"],
			 "phase":"review","phaseBy":"observed","updatedAt":"` + at + `",
			 "pr":{"url":"https://github.com/meros/fyr/pull/3","repo":"meros/fyr","number":3,"state":"OPEN","checks":"pending"}},
			{"id":"c_doc","key":"doc:x","kind":"doc","title":"Doc","phase":"working","phaseBy":"agent"}]}`)
	}
	rs := map[string]*Change{}
	a, err := DecodeThread(old("t1", "2026-11-15T08:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := DecodeThread(old("t2", "2026-11-15T09:00:00Z"))
	if !MigrateObserved(a, rs) || !MigrateObserved(b, rs) {
		t.Fatal("nothing migrated")
	}
	if MigrateObserved(a, rs) {
		t.Fatal("a second migration changed the thread")
	}
	if len(rs) != 2 || rs["c_v0"] == nil || rs["c_pr"] == nil {
		t.Fatalf("records: %v", rs)
	}
	v0 := rs["c_v0"]
	if !v0.UpdatedAt.Equal(time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)) || v0.Workspace == nil || v0.Workspace.Path != "/w/fyr" || v0.Workspace.Repo != "github.com/meros/fyr" {
		t.Fatalf("v0 record: %+v %+v", v0, v0.Workspace)
	}
	if rs["c_pr"].PR != nil {
		t.Fatal("the record carries the thread's PR")
	}
	for _, th := range []*Thread{a, b} {
		if len(th.Changes) != 2 || th.Changes[0].ID != "c_pr" || th.Changes[0].Observed != "c_pr" || th.Changes[1].Key != "doc:x" {
			t.Fatalf("%s changes: %+v", th.ID, th.Changes)
		}
		if len(th.Refs) != 2 || th.Refs[0] != (Ref{ID: "c_v0", Repo: "github.com/meros/fyr", Branch: "v0"}) {
			t.Fatalf("%s refs: %+v", th.ID, th.Refs)
		}
		// Shown once each: the PR change, the doc and the v0 record.
		shown := ShownChanges(th, func(id string) (*Change, []string) { return rs[id], []string{"t1", "t2"} })
		if len(shown) != 3 || shown[2].ID != "c_v0" || len(shown[2].Threads) != 2 {
			t.Fatalf("%s shows %+v", th.ID, shown)
		}
	}
}

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
	if r := ChangeBlocking(th); r != "" {
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
	rs, rec := observe(t, nil, GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "main", Base: "origin/main", Ahead: 1, Unpushed: 0, Head: "h1"}, ct0)
	if rec.Phase != PhaseWorking || len(rec.Revisions) != 1 || rec.Revisions[0].ID != "h1" {
		t.Fatalf("with a commit ahead: %+v", rec)
	}
	// origin/main moves to h2 and this checkout is still at h1: behind the base, not merged into it.
	_, again, _, _ := ReduceRecord(rs, Input{Source: Observer(ObsGit), Kind: KindGitObserved, Args: GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "main", Base: "origin/main", BaseHead: "h2", Ahead: 0, Unpushed: 0, Head: "h1"}}, ct0)
	if again.Phase != PhaseWorking {
		t.Fatalf("a branch behind its base is not landed: %s", again.Phase)
	}
	// and when it catches up with a new commit, the record takes the new revision instead of ending.
	_, more, _, _ := ReduceRecord(rs, Input{Source: Observer(ObsGit), Kind: KindGitObserved, Args: GitFacts{Repo: "github.com/o/r", Path: "/w/r", Branch: "main", Base: "origin/main", BaseHead: "h2", Ahead: 1, Unpushed: 0, Head: "h3"}}, ct0)
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
