package github

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// memKV is the store's key-value table in memory: what survives a restart.
type memKV struct {
	mu sync.Mutex
	m  map[string]string
}

func (k *memKV) GetKV(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[key], nil
}

func (k *memKV) SetKV(key, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string]string{}
	}
	k.m[key] = v
	return nil
}

// restartCache backs the package caches with kv at clock *now, as fyrd's
// start does, and undoes it when the test ends.
func restartCache(t *testing.T, kv KV, now *time.Time) {
	t.Helper()
	setCacheNow(func() time.Time { return *now })
	UseCache(kv)
	t.Cleanup(func() { UseCache(nil); setCacheNow(nil) })
}

// V0.persist-runtime-state: a rate-limit wait outlives a restart; a new
// daemon does not spend its first poll on a refused call.
func TestBudgetRateLimitSurvivesRestart(t *testing.T) {
	now := at0
	kv := &memKV{}
	b := &Budget{Now: func() time.Time { return now }, KV: kv}
	b.SetRate(Rate{Limit: 5000, Remaining: 4000, Reset: at0.Add(40 * time.Minute), GraphQL: 4000})
	c := &counting{out: func([]string) ([]byte, error) {
		return nil, errors.New("gh api: exit status 1: API rate limit exceeded for user ID 1. (HTTP 403)")
	}}
	b.Runner(c.run, Poll)(context.Background(), "api", "repos/o/r/x")

	b2 := &Budget{Now: func() time.Time { return now }, KV: kv}
	c2 := &counting{}
	if _, err := b2.Runner(c2.run, Poll)(context.Background(), "api", "repos/o/r/x"); !errors.Is(err, ErrBudget) || len(c2.calls) != 0 {
		t.Fatalf("after a restart the poll ran: %v %v", err, c2.calls)
	}
	if p := b2.Paused(); p == nil || !p.Until.Equal(at0.Add(40*time.Minute)) {
		t.Fatalf("pause after restart %+v", p)
	}
	// Past the reset a restart keeps nothing.
	now = at0.Add(41 * time.Minute)
	b3 := &Budget{Now: func() time.Time { return now }, KV: kv}
	if _, err := b3.Runner(c2.run, Poll)(context.Background(), "api", "repos/o/r/x"); err != nil || b3.Paused() != nil {
		t.Fatalf("expired wait kept: %v %+v", err, b3.Paused())
	}
}

// V0.persist-runtime-state: whether a repo has CI (#156's ciRepos) is
// known after a restart; a "no CI" answer expires after CIRecheck.
func TestCIReposSurviveRestart(t *testing.T) {
	now := at0
	kv := &memKV{}
	restartCache(t, kv, &now)
	noCI := &scripted{out: map[string]string{"api graphql": "0"}}
	if RepoHasCI(context.Background(), noCI.run, "acme/none") {
		t.Fatal("acme/none has CI")
	}
	markCI("acme/ci")

	UseCache(kv) // restart
	if RepoHasCI(context.Background(), noCI.run, "acme/none") || noCI.count("api graphql") != 1 {
		t.Fatalf("no-CI fact lost: %d reads", noCI.count("api graphql"))
	}
	if !RepoHasCI(context.Background(), noCI.run, "acme/ci") || noCI.count("api graphql") != 1 {
		t.Fatalf("CI fact lost: %d reads", noCI.count("api graphql"))
	}
	now = at0.Add(CIRecheck + time.Minute)
	UseCache(kv)
	RepoHasCI(context.Background(), noCI.run, "acme/none")
	if noCI.count("api graphql") != 2 {
		t.Fatalf("an expired no-CI fact was kept: %d reads", noCI.count("api graphql"))
	}
	if v, ok := ciRepos.Load("acme/ci"); !ok || !v.(ciFact).has {
		t.Fatal("a CI fact younger than CIKeep was dropped")
	}
}

// V0.persist-runtime-state: base-branch rules, default branches, repos
// without deployments and final deployment statuses load after a restart
// until their expiry.
func TestRepoFactsSurviveRestart(t *testing.T) {
	now := at0
	kv := &memKV{}
	restartCache(t, kv, &now)
	baseRules.Store("acme/r@main", ruleSet{required: []string{"test"}, protected: true, at: now})
	saveBaseRules()
	defaultBranches.Store("acme/r", defaultBranchFact{branch: "main", at: now})
	saveDefaultBranches()
	markDeploys("acme/r", false)
	finalStatus.Store("acme/r#7", statusRead{State: "success", EnvURL: "https://example.com", at: now})
	saveFinalStatus()

	UseCache(kv) // restart
	if v, ok := baseRules.Load("acme/r@main"); !ok || !v.(ruleSet).protected || v.(ruleSet).required[0] != "test" {
		t.Fatalf("rules %v %v", v, ok)
	}
	if v, ok := defaultBranches.Load("acme/r"); !ok || v.(defaultBranchFact).branch != "main" {
		t.Fatalf("default branch %v %v", v, ok)
	}
	if has, known := RepoHasDeploys("acme/r"); has || !known {
		t.Fatalf("deploys %v %v", has, known)
	}
	if v, ok := finalStatus.Load("acme/r#7"); !ok || v.(statusRead).State != "success" || v.(statusRead).EnvURL != "https://example.com" {
		t.Fatalf("final status %v %v", v, ok)
	}

	now = at0.Add(RulesRecheck + RepoDeploysRecheck + DefaultBranchRecheck + time.Minute)
	UseCache(kv)
	if _, ok := baseRules.Load("acme/r@main"); ok {
		t.Error("expired rules kept")
	}
	if _, ok := defaultBranches.Load("acme/r"); ok {
		t.Error("expired default branch kept")
	}
	if _, known := RepoHasDeploys("acme/r"); known {
		t.Error("expired no-deploys fact kept")
	}
	if _, ok := finalStatus.Load("acme/r#7"); !ok {
		t.Error("a final status younger than FinalStatusKeep was dropped")
	}
}

// V0.persist-runtime-state: the poller's deploy-watch times and cached
// failing-check annotations survive a restart.
func TestPollerStateSurvivesRestart(t *testing.T) {
	now := at0
	kv := &memKV{}
	p := &Poller{KV: kv, Now: func() time.Time { return now }}
	p.loadState()
	p.deployAt = map[string]time.Time{"acme/r@abc": now}
	p.saveDeployAt()
	p.notes = map[string]noteCache{"https://github.com/acme/r/pull/1": {sha: "abc", failed: 1, notes: []core.Annotation{{Path: "a.go"}}, at: now}}
	p.saveNotes()

	p2 := &Poller{KV: kv, Now: func() time.Time { return now }}
	p2.loadState()
	if !p2.deployAt["acme/r@abc"].Equal(at0) {
		t.Fatalf("deployAt %v", p2.deployAt)
	}
	got := p2.annotations(context.Background(), "https://github.com/acme/r/pull/1", core.PR{Head: "abc"}, 1)
	if len(got) != 1 || got[0].Path != "a.go" {
		t.Fatalf("annotations %v", got)
	}

	now = at0.Add(NotesKeep + time.Minute)
	p3 := &Poller{KV: kv, Now: func() time.Time { return now }}
	p3.loadState()
	if len(p3.deployAt) != 0 || len(p3.notes) != 0 {
		t.Fatalf("expired state kept: %v %v", p3.deployAt, p3.notes)
	}
}
