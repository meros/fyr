package agent

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/core"
)

type compactHost struct{ rlHost }

func (compactHost) Thread(id string) (core.Thread, bool) {
	return core.Thread{ID: id, Changes: []core.Change{
		{ID: "c_open1", Key: "k8s:prod/api", Title: "Raise api replicas", Phase: core.PhaseWorking},
		{ID: "c_merged", Key: "x", Title: "Merged fix", Phase: core.PhaseLanded},
		{ID: "c_gone", Key: "y", Title: "Dropped idea", Phase: core.PhaseAbandoned},
		{ID: "c_broken", Key: "z", Title: "Deployed but broken", Phase: core.PhaseLive,
			Conditions: []core.Condition{{Type: "health", Status: core.CondFail, Reason: "crash"}}},
	}}, true
}

// SPEC V0.96 (5): a process with Fyr's tools registers a SessionStart hook
// with matcher compact, and it returns one line per open change (full id,
// title, phase) as additionalContext.
func TestCompactHookListsOpenChanges(t *testing.T) {
	ss, _ := initHooks(true)["SessionStart"].([]claude.Msg)
	if len(ss) != 1 || ss[0]["matcher"] != "compact" {
		t.Fatalf("SessionStart hooks %v, want one with matcher compact", ss)
	}
	ids, _ := ss[0]["hookCallbackIds"].([]string)
	if len(ids) != 1 {
		t.Fatalf("callback ids %v", ids)
	}
	if _, ok := initHooks(false)["SessionStart"]; ok {
		t.Fatal("a process without Fyr's tools registers SessionStart")
	}
	m := New(compactHost{}, Config{})
	s := &session{thread: "t1", changeTools: true}
	out := m.hook(s, ids[0], claude.Msg{"hook_event_name": "SessionStart", "source": "compact"})
	hso, _ := out["hookSpecificOutput"].(claude.Msg)
	ctx, _ := hso["additionalContext"].(string)
	if hso["hookEventName"] != "SessionStart" || ctx == "" {
		t.Fatalf("hook answer %v", out)
	}
	for _, want := range []string{"c_open1", "Raise api replicas", "working", "c_broken", "Deployed but broken", "live"} {
		if !strings.Contains(ctx, want) {
			t.Fatalf("context lacks %q: %q", want, ctx)
		}
	}
	for _, not := range []string{"c_merged", "c_gone"} {
		if strings.Contains(ctx, not) {
			t.Fatalf("context lists the done change %s: %q", not, ctx)
		}
	}
}
