package core

import (
	"testing"
	"time"
)

// SPEC 0.5 V0.70 (1): a landed change blocks only for a failure recorded
// after it landed. The live case (15 Nov): the agent reported claude-review
// failed and the change landed in the same call; the thread stayed Blocked.
func TestLandedChangeBlocksOnlyForLaterFailures(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 13, 20, 0, 0, time.UTC)
	th := &Thread{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "claude-plugins:fix/x", Kind: KindCode, Title: "Pin Node", Branch: "fix/x"}, ByAgent, t0)
	if err := UpdateChange(th, c, ChangeUpdate{Conditions: []Condition{{Type: "claude-review", Status: CondFail, Reason: "no GitHub App id"}}}, ByAgent, t0); err != nil {
		t.Fatal(err)
	}
	if needText(th) == "" {
		t.Fatal("an in-flight failure did not block")
	}
	landed := t0.Add(34 * time.Second) // the live case: a later call
	if err := UpdateChange(th, c, ChangeUpdate{Phase: PhaseLanded}, ByAgent, landed); err != nil {
		t.Fatal(err)
	}
	if r := needText(th); r != "" {
		t.Fatalf("a failure from before the landing blocked: %q", r)
	}
	later := landed.Add(time.Minute)
	if err := UpdateChange(th, c, ChangeUpdate{Conditions: []Condition{{Type: "health", Status: CondFail, Reason: "500s after the release"}}}, ByAgent, later); err != nil {
		t.Fatal(err)
	}
	if r := needText(th); r == "" {
		t.Fatal("a failure after the landing did not block")
	}
}

// SPEC 0.5 V0.70 (1): a failure reported in the same call as the landing
// happened after it (a health check after an apply) and blocks.
func TestFailureWithTheLandingBlocks(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC)
	th := &Thread{}
	c, _, _ := OpenChange(th, ChangeOpen{Key: "k8s:prod/api", Kind: KindInfra, Title: "Replicas"}, ByAgent, t0)
	if err := UpdateChange(th, c, ChangeUpdate{Phase: PhaseLanded, Conditions: []Condition{{Type: "health", Status: CondFail, Reason: "2 of 4 pods crash"}}}, ByAgent, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if needText(th) == "" {
		t.Fatal("a failure reported with the landing did not block")
	}
}

// SPEC 0.5 V0.70 (1): a landed change stored before landedAt existed takes
// its last update as the landing.
func TestDecodeLandedAt(t *testing.T) {
	b := []byte(`{"id":"t1","changes":[{"id":"c1","key":"k","kind":"code","phase":"landed","phaseBy":"agent","updatedAt":"2026-11-15T13:21:33Z","conditions":[{"type":"claude-review","status":"fail","source":"agent","at":"2026-11-15T13:20:59Z"}]}]}`)
	th, err := DecodeThread(b)
	if err != nil {
		t.Fatal(err)
	}
	if r := needText(th); r != "" {
		t.Fatalf("a stored landed change still blocks: %q", r)
	}
}

// SPEC 0.5 V0.70 (2): change_open for a code change on a branch that
// already has a code change in the same repo returns that change.
func TestOpenCodeChangeJoinsItsBranch(t *testing.T) {
	t0 := time.Date(2026, 11, 15, 13, 0, 0, 0, time.UTC)
	th := &Thread{}
	ChangePRObserved(th, PR{URL: "https://github.com/NorbanDev/claude-plugins/pull/113", Repo: "NorbanDev/claude-plugins", Number: 113, State: "OPEN", Branch: "fix/x"}, t0)
	c, made, err := OpenChange(th, ChangeOpen{Key: "claude-plugins:fix/x", Kind: KindCode, Title: "Pin Node", Branch: "fix/x", Targets: []string{"repo:github.com/NorbanDev/claude-plugins"}}, ByAgent, t0)
	if err != nil || made || len(th.Changes) != 1 || c.PRURL() == "" {
		t.Fatalf("made %v, %d changes, %+v, %v", made, len(th.Changes), c, err)
	}
	if FindChange(th, "claude-plugins:fix/x") != c {
		t.Fatal("the agent's key does not find the joined change")
	}
	other, made, _ := OpenChange(th, ChangeOpen{Key: "web:fix/x", Kind: KindCode, Branch: "fix/x", Targets: []string{"repo:github.com/NorbanDev/norban-web"}}, ByAgent, t0)
	if !made || other == c {
		t.Fatal("the same branch name in another repo joined the change")
	}
}
