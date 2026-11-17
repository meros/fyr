package agent

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

func systemManager(s core.SystemAgents) *Manager {
	profiles := []provider.Profile{
		{ID: "claude", Name: "Claude", Models: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}, DefaultModel: "claude-opus-5-5"},
		{ID: "deepseek", Name: "DeepSeek", Models: []string{"deepseek-chat"}, Endpoint: cli.Endpoint{BaseURL: "https://api.deepseek.com/anthropic", ModelEnv: map[string]string{"ANTHROPIC_DEFAULT_HAIKU_MODEL": "deepseek-chat-small"}}},
		{ID: "zai", Name: "Z.ai", Models: []string{"glm-4.6"}, DefaultModel: "glm-4.6", Auth: cli.Auth{Kind: cli.APIKey}, Endpoint: cli.Endpoint{BaseURL: "https://api.z.ai/api/anthropic"}},
		{ID: "off", Name: "Off", Models: []string{"claude-haiku-5-5"}, Unavailable: "no login yet"},
	}
	return New(nil, Config{
		HostName: "host-a",
		Profile: func(id string) provider.Profile {
			for _, p := range profiles {
				if p.ID == id {
					return p
				}
			}
			return profiles[0]
		},
		ProfileExists: func(id string) bool {
			for _, p := range profiles {
				if p.ID == id {
					return true
				}
			}
			return false
		},
		SystemAgents: func() core.SystemAgents { return s },
	})
}

// V0.115 (3): with no setting a system job runs Haiku on the host's default
// provider; the setting picks the provider and the model, per job.
func TestSystemAgent(t *testing.T) {
	m := systemManager(core.SystemAgents{})
	a, err := m.SystemAgent(core.JobTitle)
	if err != nil || a.Host != "host-a" || a.Profile.ID != "claude" || a.Model != "claude-haiku-5-5" {
		t.Fatalf("default: %+v %v", a, err)
	}

	m = systemManager(core.SystemAgents{
		Default: core.SystemAgentChoice{Profile: "deepseek"},
		Jobs:    map[string]core.SystemAgentChoice{core.JobSetup: {Profile: "claude", Model: "claude-sonnet-5-5"}},
	})
	if a, err := m.SystemAgent(core.JobTitle); err != nil || a.Profile.ID != "deepseek" || a.Model != "deepseek-chat-small" {
		t.Fatalf("title on deepseek: %+v %v", a, err)
	}
	if a, err := m.SystemAgent(core.JobSetup); err != nil || a.Profile.ID != "claude" || a.Model != "claude-sonnet-5-5" {
		t.Fatalf("setup override: %+v %v", a, err)
	}
}

// V0.115 (4): a provider or model that cannot run fails the job with a reason;
// it never falls back to another provider or to a bigger model.
func TestSystemAgentFailsVisibly(t *testing.T) {
	for _, c := range []struct {
		name string
		s    core.SystemAgentChoice
		want string
	}{
		{"no haiku", core.SystemAgentChoice{Profile: "zai"}, "Z.ai has no Haiku model"},
		{"gone", core.SystemAgentChoice{Profile: "nope"}, `provider "nope" is not on this host`},
		{"unavailable", core.SystemAgentChoice{Profile: "off"}, "no login yet"},
		{"unknown job", core.SystemAgentChoice{}, "unknown system job"},
	} {
		m := systemManager(core.SystemAgents{Default: c.s})
		job := core.JobTitle
		if c.name == "unknown job" {
			job = "poems"
		}
		a, err := m.SystemAgent(job)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %+v %v", c.name, a, err)
		}
		if c.name == "no haiku" && !strings.Contains(err.Error(), "Settings → System agents") {
			t.Fatalf("the reason does not say where to fix it: %v", err)
		}
	}
}

// V0.117: with no setting, a title on the live Claude login (no Haiku in its
// stored list) runs on claude-haiku-5-5.
func TestSystemAgentDefaultOnAStoredClaudeLogin(t *testing.T) {
	login := provider.Profile{ID: "claudeAlt", Name: "Claude", CLI: "claude", Auth: cli.Auth{Kind: cli.CLILogin},
		Models: []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-fable-5-2"}}
	m := New(nil, Config{Profile: func(string) provider.Profile { return login }})
	for _, job := range []string{core.JobTitle, core.JobSetup} {
		if a, err := m.SystemAgent(job); err != nil || a.Model != "claude-haiku-5-5" {
			t.Fatalf("%s: %+v %v", job, a, err)
		}
	}
}
