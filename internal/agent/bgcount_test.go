package agent

import (
	"testing"

	"github.com/meros/fyr/internal/provider"
)

// V0.bg-count-follows-ended: a task that ends (task_notification or
// task_updated) leaves the agent's background count at once, also when the
// CLI sends no background_tasks_changed after it. The stale count kept the
// process from stopping and read as thread-phantom-tasks.
func TestBackgroundCountFollowsEndedTasks(t *testing.T) {
	m := New(rlHost{}, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "claude"} }})
	s := &session{thread: "t1", digest: newDigest()}
	changed := func(ids ...string) {
		var ts []any
		for _, id := range ids {
			ts = append(ts, map[string]any{"task_id": id, "task_type": "local_bash"})
		}
		m.handle(s, map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": ts})
	}
	bg := func() int { m.mu.Lock(); defer m.mu.Unlock(); return s.bg }

	changed("b1", "b2")
	if bg() != 2 {
		t.Fatalf("bg %d, want 2", bg())
	}
	m.handle(s, map[string]any{"type": "system", "subtype": "task_notification", "task_id": "b1", "status": "completed"})
	if bg() != 1 {
		t.Fatalf("after one ended: bg %d, want 1", bg())
	}
	m.handle(s, map[string]any{"type": "system", "subtype": "task_updated", "task_id": "b2", "patch": map[string]any{"status": "killed"}})
	if bg() != 0 {
		t.Fatalf("after both ended: bg %d, want 0", bg())
	}
	// An ending task the list never named changes nothing.
	m.handle(s, map[string]any{"type": "system", "subtype": "task_notification", "task_id": "x", "status": "completed"})
	if bg() != 0 {
		t.Fatalf("bg %d", bg())
	}
}
