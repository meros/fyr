package core

import (
	"slices"
	"testing"
	"time"
)

// Issue 69: one work item, one card.

const (
	ocRepo = "github.com/meros/fyr"
	ocPR   = "https://github.com/meros/fyr/pull/67"
)

func ocOpen(t *testing.T, th *Thread, o ChangeOpen, pr string) *Thread {
	t.Helper()
	if pr != "" {
		// As the change_open tool does: the PR is evidence on the change.
		o.Evidence = append(o.Evidence, Evidence{Kind: "pr", URL: pr, Title: "#67"})
	}
	th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindChangeOpen, Args: ChangeOpenArgs{Open: o, PR: pr}}, ct0)
	return th
}

func ocPRSeen(t *testing.T, th *Thread, state string, at time.Time) *Thread {
	t.Helper()
	th, _ = fstep(t, th, Input{Source: Observer(ObsGitHub), Kind: KindPRObserved, Args: PR{URL: ocPR, Repo: "meros/fyr", Number: 67, Title: "Dual installs", Branch: "fix/y", State: state, Checks: ChecksPass, CheckedAt: &at}}, at)
	return th
}

func ocShown(th *Thread, rec *Change) []Change {
	return ShownChanges(th, func(id string) (*Change, []string) {
		if rec != nil && id == rec.ID {
			return rec, []string{th.ID}
		}
		return nil, nil
	})
}

// SPEC 0.5 V0.87 (1), V0.59 (3): a change of any kind on a branch follows the host record of that branch: it is
// linked (observed), shows the record's revisions until the agent writes its own, and lands with the branch. The
// thread shows one card for it.
func TestDocChangeOnBranchFollowsRecord(t *testing.T) {
	th := &Thread{ID: "t"}
	th = ocOpen(t, th, ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Doc", Branch: "fix/y", Targets: []string{"repo:" + ocRepo}}, "")
	rs, rec := observe(t, nil, GitFacts{Repo: ocRepo, Path: "/w/fyr", Branch: "fix/y", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "h1"}, ct0)
	th, _ = fstep(t, th, Input{Source: Observer(ObsGit), Kind: KindChangeObserved, Args: ChangeObservedArgs{After: *rec}}, ct0)
	c := FindChange(th, "doc:x")
	if c.Observed != rec.ID || c.Phase != rec.Phase || condition(c, CondPushed) == nil || condition(c, CondPushed).Status != CondPending {
		t.Fatalf("the doc change does not follow the record: %+v", c)
	}
	if len(c.Revisions) != 1 || c.Revisions[0].ID != "h1" {
		t.Fatalf("the record's revisions are not shown: %+v", c.Revisions)
	}
	if shown := ocShown(th, rec); len(shown) != 1 {
		t.Fatalf("two cards for one branch: %+v", shown)
	}
	// A second change_open on the same key is the same change (guard).
	th = ocOpen(t, th, ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Doc", Branch: "fix/y"}, "")
	if len(th.Changes) != 1 {
		t.Fatalf("a second change_open made a second change: %+v", th.Changes)
	}
	// The agent writes its own revision; the record's next one is added beside it.
	th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindChangeRevise, Args: ChangeReviseArgs{Change: "doc:x", Revision: Revision{ID: "draft-2"}}}, ct0)
	t1 := ct0.Add(time.Minute)
	rs2, rec2 := observe(t, rs, GitFacts{Repo: ocRepo, Path: "/w/fyr", Branch: "fix/y", Base: "origin/main", Ahead: 2, Unpushed: 0, Head: "h2"}, t1)
	th, _ = fstep(t, th, Input{Source: Observer(ObsGit), Kind: KindChangeObserved, Args: ChangeObservedArgs{Before: rec, After: *rec2}}, t1)
	c = FindChange(th, "doc:x")
	ids := []string{}
	for _, r := range c.Revisions {
		ids = append(ids, r.ID)
	}
	if !slices.Equal(ids, []string{"h1", "draft-2", "h2"}) {
		t.Fatalf("revisions after the agent's own: %v", ids)
	}
	_, landed, _, _ := ReduceRecord(rs2, Input{Source: Observer(ObsGit), Kind: KindGitEnded, Args: GitEnd{Change: rec2.ID, Merged: true}}, t1)
	th, _ = fstep(t, th, Input{Source: Observer(ObsGit), Kind: KindChangeObserved, Args: ChangeObservedArgs{Before: rec2, After: *landed}}, t1)
	if c := FindChange(th, "doc:x"); c.Phase != PhaseLanded || c.PhaseBy != ByObserved {
		t.Fatalf("the doc change did not land with its branch: %+v", c)
	}
}

// SPEC 0.5 V0.87 (2), V0.59 (4): a PR for the branch of a change that follows a record lands on that change, not on a
// second change keyed by the PR.
func TestPRForBranchLandsOnFollower(t *testing.T) {
	th := &Thread{ID: "t"}
	th = ocOpen(t, th, ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Doc", Branch: "fix/y", Targets: []string{"repo:" + ocRepo}}, "")
	_, rec := observe(t, nil, GitFacts{Repo: ocRepo, Path: "/w/fyr", Branch: "fix/y", Base: "origin/main", Ahead: 1, Unpushed: 0, Head: "h1"}, ct0)
	th, _ = fstep(t, th, Input{Source: Observer(ObsGit), Kind: KindChangeObserved, Args: ChangeObservedArgs{After: *rec}}, ct0)
	th = ocPRSeen(t, th, "OPEN", ct0)
	if len(th.Changes) != 1 {
		t.Fatalf("the PR made a second change: %+v", th.Changes)
	}
	c := &th.Changes[0]
	if c.Key != "doc:x" || c.PRURL() != ocPR || c.Phase != PhaseReview || c.Observed != rec.ID {
		t.Fatalf("the PR is not on the change of its branch: %+v", c)
	}
	if shown := ocShown(th, rec); len(shown) != 1 {
		t.Fatalf("two cards: %+v", shown)
	}
}

// SPEC 0.5 V0.87 (2): the live case of issue 69. Fyr made pr:meros/fyr#67 from the PR's URL; then the agent opened
// its own change with that PR. Fyr's change folds into the agent's: one change, the PR's key an alias, the observed
// phase and the thread's main change on it.
func TestAgentChangeWithPRFoldsFyrsPRChange(t *testing.T) {
	th := &Thread{ID: "t"}
	PRObserved(th, PR{URL: ocPR, Repo: "meros/fyr", Number: 67, State: "OPEN", Checks: ChecksPending}, ct0)
	th = ocPRSeen(t, th, "OPEN", ct0)
	fyrs := th.Changes[0].ID
	if th.Changes[0].Key != "pr:meros/fyr#67" || th.MainChange != fyrs || FindRule(th, "builtins:"+fyrs) == nil {
		t.Fatalf("setup: %+v main %s", th.Changes, th.MainChange)
	}
	th = ocOpen(t, th, ChangeOpen{Key: "fyr:decision-0021", Kind: KindDoc, Title: "Decision 0021"}, ocPR)
	if len(th.Changes) != 1 {
		t.Fatalf("two changes for one PR: %+v", th.Changes)
	}
	c := &th.Changes[0]
	if c.Key != "fyr:decision-0021" || c.Title != "Decision 0021" || !slices.Contains(c.Aliases, "pr:meros/fyr#67") {
		t.Fatalf("not the agent's change: %+v", c)
	}
	if c.PR == nil || c.PR.CheckedAt == nil || c.Phase != PhaseReview || c.PhaseBy != ByObserved {
		t.Fatalf("the PR's facts did not move over: %+v %+v", c, c.PR)
	}
	if th.MainChange != c.ID || FindRule(th, "builtins:"+c.ID) == nil || FindRule(th, "builtins:"+fyrs) != nil {
		t.Fatalf("main %s rules %+v", th.MainChange, th.Rules)
	}
	if FindChange(th, "pr:meros/fyr#67") != c {
		t.Fatal("the PR's key does not find the change")
	}
	// The merge lands the one change.
	th = ocPRSeen(t, th, "MERGED", ct0.Add(time.Hour))
	if len(th.Changes) != 1 || th.Changes[0].Phase != PhaseLanded {
		t.Fatalf("after the merge: %+v", th.Changes)
	}
}

// SPEC 0.5 V0.87 (2): change_attach with the PR on an agent's change folds Fyr's change for that PR into it.
func TestAttachPRFoldsFyrsPRChange(t *testing.T) {
	th := &Thread{ID: "t"}
	th = ocPRSeen(t, th, "OPEN", ct0)
	th = ocOpen(t, th, ChangeOpen{Key: "fyr:decision-0021", Kind: KindDoc, Title: "Decision 0021"}, "")
	if len(th.Changes) != 2 {
		t.Fatalf("setup: %+v", th.Changes)
	}
	th, _ = fstep(t, th, Input{Source: FromAgent, Kind: KindChangeAttach, Args: ChangeAttachArgs{Change: "fyr:decision-0021", Evidence: []Evidence{{Kind: "pr", URL: ocPR}}}}, ct0)
	if len(th.Changes) != 1 || th.Changes[0].Key != "fyr:decision-0021" || th.Changes[0].Phase != PhaseReview {
		t.Fatalf("after attach: %+v", th.Changes)
	}
}

// SPEC 0.5 V0.87 (2): the agent opens a doc change on the branch of Fyr's PR change: the PR's change folds into it.
func TestDocChangeOnPRBranchFoldsFyrsPRChange(t *testing.T) {
	th := &Thread{ID: "t"}
	th = ocPRSeen(t, th, "OPEN", ct0)
	th = ocOpen(t, th, ChangeOpen{Key: "doc:x", Kind: KindDoc, Title: "Doc", Branch: "fix/y"}, "")
	if len(th.Changes) != 1 || th.Changes[0].Key != "doc:x" || th.Changes[0].PRURL() != ocPR {
		t.Fatalf("two changes for one branch: %+v", th.Changes)
	}
}

// SPEC 0.5 V0.87 (2): a thread stored with two changes that hold one PR (Fyr's and the agent's) is one change after
// the next poll of that PR.
func TestPRPollFoldsStoredTwins(t *testing.T) {
	at := ct0
	th := &Thread{ID: "t", Changes: []Change{
		{ID: ChangeID("pr:meros/fyr#67"), Key: "pr:meros/fyr#67", Kind: KindCode, Title: "meros/fyr#67", Branch: "fix/y", Phase: PhaseLanded, PhaseBy: ByObserved, LandedAt: &at,
			PR: &PR{URL: ocPR, Repo: "meros/fyr", Number: 67, State: "MERGED", CheckedAt: &at}, Evidence: []Evidence{{Kind: "pr", URL: ocPR}},
			Revisions: []Revision{{ID: "r1", By: ByAgent, At: at}, {ID: "r2", By: ByAgent, At: at}}},
		{ID: ChangeID("fyr:decision-0021"), Key: "fyr:decision-0021", Kind: KindDoc, Title: "Decision 0021", Phase: PhaseReview, PhaseBy: ByAgent,
			PR: &PR{URL: ocPR, Repo: "meros/fyr", Number: 67, State: "OPEN"}, Evidence: []Evidence{{Kind: "pr", URL: ocPR}},
			Revisions: []Revision{{ID: "d1", By: ByAgent, At: at}}},
	}}
	th.MainChange = th.Changes[0].ID
	th = ocPRSeen(t, th, "MERGED", ct0.Add(time.Hour))
	if len(th.Changes) != 1 {
		t.Fatalf("still two changes: %+v", th.Changes)
	}
	c := &th.Changes[0]
	if c.Key != "fyr:decision-0021" || c.Phase != PhaseLanded || len(c.Revisions) != 3 || th.MainChange != c.ID {
		t.Fatalf("folded: %+v main %s", c, th.MainChange)
	}
}
