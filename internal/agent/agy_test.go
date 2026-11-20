package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/agycli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

type fakeHost struct{ th core.Thread }

func (f fakeHost) Apply(core.Input) (any, error)   { return nil, nil }
func (f fakeHost) Thread(string) (core.Thread, bool) { return f.th, true }

func TestAgyAdapterSystemModelAndHandoverPlan(t *testing.T) {
	gdir := filepath.Join(t.TempDir(), ".gemini")
	convDir := filepath.Join(gdir, "antigravity-cli", "conversations")
	os.MkdirAll(convDir, 0o700)
	os.WriteFile(filepath.Join(convDir, "s-agy.db"), []byte("db"), 0o600)

	agyProf := provider.Profile{
		ID: "agy", Name: "Antigravity", CLI: agycli.ID, Binary: "agy",
		Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: gdir},
		Models: []string{"gemini-4-neon", "gemini-3.8-flash"}, DefaultModel: "gemini-4-neon",
	}
	agy2 := provider.Profile{
		ID: "agy2", Name: "Antigravity 2", CLI: agycli.ID, Binary: "agy",
		Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: filepath.Join(t.TempDir(), ".gemini-2")},
	}
	claudeProf := provider.Profile{
		ID: "claude", Name: "Claude", CLI: "claude", Binary: "claude",
	}
	profs := map[string]provider.Profile{"agy": agyProf, "agy2": agy2, "claude": claudeProf}

	m := New(fakeHost{th: core.Thread{ID: "t1", Profile: "agy", SessionID: "s-agy", Cwd: "/work"}}, Config{
		Profile: func(id string) provider.Profile { return profs[id] },
	})
	ad, err := m.adapter(agyProf)
	if err != nil || ad.ID() != agycli.ID {
		t.Fatalf("adapter: %v %v", ad, err)
	}
	if got := HaikuModel(agyProf); got != "gemini-3.8-flash" {
		t.Fatalf("fast model for agy: %q", got)
	}
	// Same CLI with existing conversation DB → Rung 1 (resume).
	if plan := m.HandoverPlan("t1", "agy2"); plan.Rung != RungResume {
		t.Fatalf("agy->agy handover plan: %+v", plan)
	}
	// Different CLI (agy -> claude) → Rung 2 (adopt).
	if plan := m.HandoverPlan("t1", "claude"); plan.Rung != RungAdopt || plan.Note != "The target runs another CLI" {
		t.Fatalf("agy->claude handover plan: %+v", plan)
	}
}
