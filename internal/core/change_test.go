package core

import (
	"strings"
	"testing"
	"time"
)

var ct0 = time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)

// SPEC 0.5 V0.59 (1), (5): change_open is idempotent by key; a second call
// fills only what is empty.
func TestOpenChangeIdempotent(t *testing.T) {
	th := &Thread{}
	c, made, err := OpenChange(th, ChangeOpen{Key: "k8s:prod/api", Kind: "infra", Title: "Raise api replicas", Targets: []string{"k8s:prod/api"}}, ByAgent, ct0)
	if err != nil || !made || c.Phase != PhaseWorking || c.ID != ChangeID("k8s:prod/api") {
		t.Fatalf("open: %+v %v %v", c, made, err)
	}
	c2, made2, _ := OpenChange(th, ChangeOpen{Key: "k8s:prod/api", Title: "Other", Targets: []string{"k8s:staging/api"}}, ByAgent, ct0)
	if made2 || len(th.Changes) != 1 || c2.Title != "Raise api replicas" || len(c2.Targets) != 2 {
		t.Fatalf("second open: %+v", th.Changes)
	}
	if _, _, err := OpenChange(th, ChangeOpen{Key: ""}, ByAgent, ct0); err == nil {
		t.Fatal("an empty key was accepted")
	}
	if _, _, err := OpenChange(th, ChangeOpen{Key: "x", Kind: "spaceship"}, ByAgent, ct0); err == nil {
		t.Fatal("an unknown kind was accepted")
	}
	if FindChange(th, "k8s:prod/api") != FindChange(th, c.ID) {
		t.Fatal("find by key and by id differ")
	}
}

// SPEC 0.5 V0.59 (2): facts beat claims; the agent never approves.
func TestFactsBeatClaims(t *testing.T) {
	th := &Thread{}
	ChangePRObserved(th, PR{URL: "https://github.com/o/r/pull/7", Repo: "o/r", Number: 7, Title: "Fix it", State: "OPEN", Checks: "fail"}, ct0)
	c := &th.Changes[0]
	if c.Phase != PhaseReview || c.PhaseBy != ByObserved || c.Kind != "code" {
		t.Fatalf("observed PR: %+v", c)
	}
	if err := UpdateChange(th, c, ChangeUpdate{Phase: PhaseLanded}, ByAgent, ct0); err == nil || !strings.Contains(err.Error(), "observes") {
		t.Fatalf("agent moved an observed phase: %v", err)
	}
	if err := UpdateChange(th, c, ChangeUpdate{Conditions: []Condition{{Type: "checks", Status: "ok"}}}, ByAgent, ct0); err == nil {
		t.Fatal("agent overwrote an observed condition")
	}
	other, _, _ := OpenChange(th, ChangeOpen{Key: "doc:spec", Kind: "doc", Title: "Spec"}, ByAgent, ct0)
	for _, typ := range []string{"review", "approval"} {
		if err := UpdateChange(th, other, ChangeUpdate{Conditions: []Condition{{Type: typ, Status: "ok"}}}, ByAgent, ct0); err == nil {
			t.Fatalf("agent marked %s ok", typ)
		}
	}
	if err := UpdateChange(th, other, ChangeUpdate{Phase: PhaseReview, Conditions: []Condition{{Type: "lint", Status: "ok"}}}, ByAgent, ct0); err != nil {
		t.Fatalf("a plain report failed: %v", err)
	}
	if other.Phase != PhaseReview || other.PhaseBy != ByAgent {
		t.Fatalf("reported phase: %+v", other)
	}
	if err := UpdateChange(th, c, ChangeUpdate{Phase: PhaseLive}, ByOwner, ct0); err != nil {
		t.Fatalf("the owner may set any phase: %v", err)
	}
}

// SPEC 0.5 V0.59 (4): every PR is a code change, found by URL, else by its
// head branch; merged is landed, closed abandoned.
func TestPRBecomesCodeChange(t *testing.T) {
	th := &Thread{}
	GitObserved(th, GitFacts{Repo: "github.com/o/r", Branch: "feat", Base: "origin/main", Ahead: 2, Unpushed: 2, Head: "abc"}, ct0)
	if len(th.Changes) != 1 || th.Changes[0].Branch != "feat" {
		t.Fatalf("git change: %+v", th.Changes)
	}
	ChangePRObserved(th, PR{URL: "https://github.com/o/r/pull/9", Repo: "o/r", Number: 9, Title: "Feature", State: "OPEN", Checks: "pending", Branch: "feat"}, ct0)
	if len(th.Changes) != 1 {
		t.Fatalf("the PR made a second change instead of joining the branch: %+v", th.Changes)
	}
	c := &th.Changes[0]
	if c.Title != "Feature" || c.Phase != PhaseReview || prOf(c) == "" {
		t.Fatalf("joined: %+v", c)
	}
	// A second PR of the same thread is its own change.
	ChangePRObserved(th, PR{URL: "https://github.com/o/other/pull/3", Repo: "o/other", Number: 3, State: "OPEN"}, ct0)
	if len(th.Changes) != 2 || len(PRURLs(th)) != 2 {
		t.Fatalf("second PR: %+v", th.Changes)
	}
	ChangePRObserved(th, PR{URL: "https://github.com/o/r/pull/9", Repo: "o/r", Number: 9, State: "MERGED", Branch: "feat"}, ct0.Add(time.Hour))
	ChangePRObserved(th, PR{URL: "https://github.com/o/other/pull/3", Repo: "o/other", Number: 3, State: "CLOSED"}, ct0.Add(time.Hour))
	if th.Changes[0].Phase != PhaseLanded || th.Changes[1].Phase != PhaseAbandoned || len(PRURLs(th)) != 0 {
		t.Fatalf("after merge and close: %s %s", th.Changes[0].Phase, th.Changes[1].Phase)
	}
	// PRObserved keeps thread.pr and adds the change.
	th2 := &Thread{}
	PRObserved(th2, PR{URL: "https://github.com/o/r/pull/1", Repo: "o/r", Number: 1, State: "OPEN"}, ct0)
	if th2.PR == nil || len(th2.Changes) != 1 {
		t.Fatalf("PRObserved: %+v %+v", th2.PR, th2.Changes)
	}
}

// SPEC 0.5 V0.59 (3): a branch with work has a code change; clean and pushed
// follow the facts; a clean branch with nothing ahead makes none.
func TestGitObserved(t *testing.T) {
	th := &Thread{}
	GitObserved(th, GitFacts{Branch: "main", Base: "origin/main"}, ct0)
	if len(th.Changes) != 0 {
		t.Fatal("a clean branch made a change")
	}
	GitObserved(th, GitFacts{Repo: "github.com/o/r", Branch: "main", Base: "origin/main", Dirty: 3}, ct0)
	c := &th.Changes[0]
	if c.Phase != PhaseWorking || condition(c, "clean").Status != "pending" || condition(c, "pushed").Status != "ok" {
		t.Fatalf("dirty main: %+v", c.Conditions)
	}
	if !strings.Contains(condition(c, "clean").Reason, "3 files") {
		t.Fatalf("reason: %q", condition(c, "clean").Reason)
	}
	GitObserved(th, GitFacts{Repo: "github.com/o/r", Branch: "main", Base: "origin/main", Ahead: 1, Unpushed: 1, Head: "h1"}, ct0)
	if len(th.Changes) != 1 || condition(c, "clean").Status != "ok" || condition(c, "pushed").Status != "pending" || len(c.Revisions) != 1 {
		t.Fatalf("after commit: %+v", c)
	}
	// Pending never blocks.
	if r := ChangeBlocking(th); r != "" {
		t.Fatalf("pending blocked: %q", r)
	}
}

// SPEC 0.5 V0.59 (5): a new revision resets observed and reported
// conditions; the same revision twice is one.
func TestReviseResetsConditions(t *testing.T) {
	th := &Thread{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "tf:prod", Kind: "infra", Title: "Plan"}, ByAgent, ct0)
	UpdateChange(th, c, ChangeUpdate{Conditions: []Condition{{Type: "plan", Status: "fail", Reason: "drift"}}}, ByAgent, ct0)
	if !ReviseChange(c, Revision{ID: "sha256:aa", Summary: "replan"}, ct0) {
		t.Fatal("revision not added")
	}
	if ReviseChange(c, Revision{ID: "sha256:aa"}, ct0) {
		t.Fatal("the same revision twice was added")
	}
	if k := condition(c, "plan"); k.Status != "pending" || len(c.Revisions) != 1 {
		t.Fatalf("after revise: %+v", c)
	}
}

// SPEC 0.5 V0.59 (6): a failed condition or needs:owner blocks; an ended
// change does not.
func TestChangeAttention(t *testing.T) {
	th := &Thread{ActivityAt: ct0}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "k8s:prod/api", Kind: "infra", Title: "api replicas"}, ByAgent, ct0)
	v := &View{SeenAt: ct0.Add(time.Minute)}
	if a := Compute(th, v, ct0.Add(2*time.Minute)); a.State != Seen {
		t.Fatalf("a working change alone: %+v", a)
	}
	UpdateChange(th, c, ChangeUpdate{Conditions: []Condition{{Type: "apply", Status: "fail", Reason: "apply failed on gke-prod"}}}, ByAgent, ct0)
	if a := Compute(th, v, ct0.Add(2*time.Minute)); a.State != Blocked || a.Reason != "api replicas: apply failed on gke-prod" {
		t.Fatalf("failed condition: %+v", a)
	}
	UpdateChange(th, c, ChangeUpdate{Phase: PhaseAbandoned}, ByAgent, ct0)
	if r := ChangeBlocking(th); r != "" {
		t.Fatalf("an ended change blocked: %q", r)
	}
	d, _, _ := OpenChange(th, ChangeOpen{Key: "doc:x", Kind: "doc", Title: "Design doc"}, ByAgent, ct0)
	owner := "owner"
	UpdateChange(th, d, ChangeUpdate{Needs: &owner}, ByAgent, ct0)
	if r := ChangeBlocking(th); r != "Design doc: needs you" {
		t.Fatalf("needs owner: %q", r)
	}
}

// SPEC 0.5 V0.59 (7): settle when a change reaches a phase; live counts as
// landed.
func TestSettleWhenChange(t *testing.T) {
	th := &Thread{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "nix:server", Kind: "host", Title: "Rebuild"}, ByAgent, ct0)
	th.SettleWhen = &SettleWhen{Change: c.ID, Phase: PhaseLanded}
	UpdateChange(th, c, ChangeUpdate{Phase: PhaseReview}, ByAgent, ct0)
	if th.SettledAt != nil {
		t.Fatal("settled before landed")
	}
	UpdateChange(th, c, ChangeUpdate{Phase: PhaseLive}, ByAgent, ct0)
	if th.SettledAt == nil || th.SettleWhen != nil {
		t.Fatal("live did not count as landed")
	}
}

// SPEC 0.5 V0.59 (1): at most 50 changes; ended ones go first.
func TestTrimChanges(t *testing.T) {
	th := &Thread{}
	for i := 0; i < 50; i++ {
		c, _, _ := OpenChange(th, ChangeOpen{Key: "k" + string(rune('A'+i))}, ByAgent, ct0)
		if i == 3 {
			c.Phase = PhaseAbandoned
		}
	}
	OpenChange(th, ChangeOpen{Key: "new"}, ByAgent, ct0.Add(time.Hour))
	if len(th.Changes) != 50 || FindChange(th, "k"+string(rune('A'+3))) != nil || FindChange(th, "new") == nil {
		t.Fatalf("trim: %d changes", len(th.Changes))
	}
}
