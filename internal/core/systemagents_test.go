package core

import (
	"testing"
	"time"
)

// V0.115 (1): the system agents setting is the owner's alone; an agent cannot
// change it, and a job Fyr does not know is refused.
func TestSystemAgentsSetIsTheOwners(t *testing.T) {
	now := time.Date(2026, 11, 16, 18, 0, 0, 0, time.UTC)
	if k := Kinds[KindSystemAgentsSet]; k == nil || len(k.From) != 1 || k.From[0] != ByOwner || !k.Await {
		t.Fatalf("the setting is not the owner's alone: %+v", k)
	}
	args := Args{"profile": "claude", "model": "claude-haiku-5-5"}
	if _, err := ReduceHost(Input{Source: FromAgent, Kind: KindSystemAgentsSet, Args: args}, now); err == nil {
		t.Fatal("an agent changed the setting")
	}
	if _, err := ReduceHost(Input{Source: FromOwner, Kind: KindSystemAgentsSet, Args: Args{"job": "poems", "model": "x"}}, now); err == nil {
		t.Fatal("an unknown job was accepted")
	}
	r, err := ReduceHost(Input{Source: FromOwner, Kind: KindSystemAgentsSet, Args: Args{"job": JobTitle, "profile": "zai", "model": "glm-4.6"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Effects) != 1 || r.Effects[0].Kind != EffectSystemAgentsSet {
		t.Fatalf("effects %+v", r.Effects)
	}
	a := r.Effects[0].Request.Args
	if a["job"] != JobTitle || a["profile"] != "zai" || a["model"] != "glm-4.6" {
		t.Fatalf("args %+v", a)
	}
}

// V0.115 (2): a job's choice is its override, else the default, field by field.
func TestSystemAgentsFor(t *testing.T) {
	var s SystemAgents
	if c := s.For(JobTitle); c != (SystemAgentChoice{}) {
		t.Fatalf("empty setting: %+v", c)
	}
	s.Default = SystemAgentChoice{Profile: "claude", Model: "claude-haiku-5-5"}
	s.Jobs = map[string]SystemAgentChoice{JobSetup: {Model: "claude-sonnet-5-5"}}
	if c := s.For(JobTitle); c != s.Default {
		t.Fatalf("title: %+v", c)
	}
	if c := s.For(JobSetup); c.Profile != "claude" || c.Model != "claude-sonnet-5-5" {
		t.Fatalf("setup: %+v", c)
	}
	// An override that names a provider takes its own model, or that provider's Haiku: the default's model may not
	// be on it.
	s.Jobs[JobTitle] = SystemAgentChoice{Profile: "zai"}
	if c := s.For(JobTitle); c.Profile != "zai" || c.Model != "" {
		t.Fatalf("title on zai: %+v", c)
	}
	if len(SystemJobs) < 2 || SystemJobs[0].ID != JobTitle || SystemJobs[1].ID != JobSetup {
		t.Fatalf("jobs %+v", SystemJobs)
	}
}
