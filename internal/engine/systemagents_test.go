package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// SPEC V0.115 (1)-(3): the owner sets the system agents' provider and model on
// the host, for all jobs and per job; a provider the host does not have is
// refused; clearing an override goes back to the default; the setting
// outlives a restart; GET /api/systemAgents shows what each job runs.
func TestSystemAgentsSetting(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.e.ProfileExists = func(id string) bool { return id == "claude" || id == "zai" }
	if s := h.e.SystemAgents(); s.Default != (core.SystemAgentChoice{}) || len(s.Jobs) != 0 {
		t.Fatalf("not empty by default: %+v", s)
	}
	if r := h.cmd(core.KindSystemAgentsSet, "", core.Args{"profile": "gone", "model": "x"}); r.OK || !strings.Contains(r.Error, `"gone"`) {
		t.Fatalf("an unknown provider was accepted: %+v", r)
	}
	if r := h.cmd(core.KindSystemAgentsSet, "", core.Args{"profile": "claude", "model": "claude-haiku-5-5"}); !r.OK {
		t.Fatalf("default: %+v", r)
	}
	if r := h.cmd(core.KindSystemAgentsSet, "", core.Args{"job": core.JobSetup, "profile": "zai", "model": "glm-4.6"}); !r.OK {
		t.Fatalf("setup: %+v", r)
	}
	s := h.e.SystemAgents()
	if s.For(core.JobTitle) != (core.SystemAgentChoice{Profile: "claude", Model: "claude-haiku-5-5"}) || s.For(core.JobSetup) != (core.SystemAgentChoice{Profile: "zai", Model: "glm-4.6"}) {
		t.Fatalf("stored %+v", s)
	}

	h.e.SystemAgentResolve = func(job string) (string, string, error) {
		if job == core.JobSetup {
			return "", "", errors.New("Z.ai has no key")
		}
		return "claude", "claude-haiku-5-5", nil
	}
	h.e.SystemAgentProviders = func() []SystemProvider {
		return []SystemProvider{{ID: "claude", Name: "Claude", Models: []string{"claude-haiku-5-5"}, Haiku: "claude-haiku-5-5"}}
	}
	v := h.e.SystemAgentsView()
	if len(v.Providers) != 1 || v.Providers[0].Haiku != "claude-haiku-5-5" {
		t.Fatalf("providers %+v", v.Providers)
	}
	if len(v.Jobs) != len(core.SystemJobs) || v.Jobs[0].ID != core.JobTitle || v.Jobs[0].Model != "claude-haiku-5-5" || v.Jobs[1].Error != "Z.ai has no key" || !v.Jobs[1].Override {
		t.Fatalf("view %+v", v)
	}

	// The setting is stored: a new engine on the same database has it, and clearing the override goes back to the
	// default.
	h2 := startHarness(t, h.dir, h.db, time.Minute)
	if got := h2.e.SystemAgents().For(core.JobSetup); got.Profile != "zai" {
		t.Fatalf("after restart %+v", got)
	}
	if r := h2.cmdID("clear-setup", core.KindSystemAgentsSet, "", core.Args{"job": core.JobSetup, "clear": true}); !r.OK {
		t.Fatalf("clear: %+v", r)
	}
	if got := h2.e.SystemAgents().For(core.JobSetup); got.Profile != "claude" {
		t.Fatalf("after clear %+v", got)
	}
}
