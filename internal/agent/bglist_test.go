package agent

import (
	"testing"

	"github.com/meros/fyr/internal/provider"
)

// V0.bg-list-after-ended: a background_tasks_changed that still names a task
// whose end the agent already saw (the CLI's list was cut before the end and
// arrives after it) does not bring the task back into the count. It left bg
// at 1 with the engine at 0 running tasks: thread-phantom-tasks.
func TestBackgroundListAfterEndedKeepsCountZero(t *testing.T) {
	m := New(rlHost{}, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "claude"} }})
	s := &session{thread: "t1", digest: newDigest()}
	bg := func() int { m.mu.Lock(); defer m.mu.Unlock(); return s.bg }
	list := func(ids ...string) {
		var ts []any
		for _, id := range ids {
			ts = append(ts, map[string]any{"task_id": id, "task_type": "local_bash"})
		}
		m.handle(s, map[string]any{"type": "system", "subtype": "background_tasks_changed", "tasks": ts})
	}
	m.handle(s, map[string]any{"type": "system", "subtype": "task_notification", "task_id": "b1", "status": "completed"})
	list("b1")
	if bg() != 0 {
		t.Fatalf("stale list after end: bg %d, want 0", bg())
	}
	list("b1", "b2")
	if bg() != 1 {
		t.Fatalf("bg %d, want 1 (only b2)", bg())
	}
}
