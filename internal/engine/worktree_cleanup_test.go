package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// V0.change-worktree-cleanup: the temp sweep hears of the worktrees Fyr
// made for changes that landed or ended, with when; their branches no
// longer keep them in use. A worktree the agent made, and one of a change
// in flight, stay as before.
func TestTempThreadsNameFyrsWorktreesOfEndedChanges(t *testing.T) {
	e, _, now := bare(t)
	id := bareThread(t, e)
	landed := now.Add(-2 * time.Hour)
	fyr := func(p string) *core.Workspace { return &core.Workspace{Path: p, MadeByFyr: true} }
	e.mu.Lock()
	e.threads[id].Changes = []core.Change{
		{ID: "c_l", Key: "l", Kind: core.KindCode, Branch: "l", Phase: core.PhaseLanded, LandedAt: &landed, UpdatedAt: *now, Workspace: fyr("/r/.claude/worktrees/l")},
		{ID: "c_a", Key: "a", Kind: core.KindCode, Branch: "a", Phase: core.PhaseAbandoned, UpdatedAt: *now, Workspace: fyr("/r/.claude/worktrees/a")},
		{ID: "c_d", Key: "d", Kind: core.KindCode, Branch: "d", Phase: core.PhaseWorking, UpdatedAt: *now, Done: &core.DoneMark{At: *now}, Workspace: fyr("/r/.claude/worktrees/d")},
		{ID: "c_w", Key: "w", Kind: core.KindCode, Branch: "w", Phase: core.PhaseWorking, UpdatedAt: *now, Workspace: fyr("/r/.claude/worktrees/w")},
		{ID: "c_m", Key: "m", Kind: core.KindCode, Branch: "m", Phase: core.PhaseLanded, UpdatedAt: *now, Workspace: &core.Workspace{Path: "/r/.claude/worktrees/m"}},
	}
	e.mu.Unlock()
	var x TempThread
	for _, t := range e.TempThreads() {
		if t.ID == id {
			x = t
		}
	}
	want := map[string]time.Time{"/r/.claude/worktrees/l": landed, "/r/.claude/worktrees/a": *now, "/r/.claude/worktrees/d": *now}
	if len(x.Done) != len(want) {
		t.Fatalf("done %v", x.Done)
	}
	for p, at := range want {
		if !x.Done[p].Equal(at) {
			t.Fatalf("done %v, want %v", x.Done, want)
		}
	}
	if len(x.Branches) != 2 || x.Branches[0] != "w" || x.Branches[1] != "m" {
		t.Fatalf("branches %v", x.Branches)
	}
}
