package engine

import (
	"testing"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// V0.usage-ledger: the meter's total for a thread reaches the thread's DTO,
// and a thread without one has none.
func TestThreadUsageInTheDTO(t *testing.T) {
	e, _, _ := bare(t)
	a, b := bareThread(t, e), bareThread(t, e)
	e.SetThreadUsage(a, api.Usage{Input: 1, Output: 2, CacheRead: 30, Total: 33})
	for _, d := range e.Snapshot() {
		switch d.ID {
		case a:
			if d.Usage == nil || d.Usage.Total != 33 || d.Usage.CacheRead != 30 {
				t.Fatalf("a: %+v", d.Usage)
			}
		case b:
			if d.Usage != nil {
				t.Fatalf("b: %+v", d.Usage)
			}
		}
	}
	// The DTO has its own copy: a later total does not change one already built.
	d := e.Snapshot()
	e.SetThreadUsage(a, api.Usage{Total: 99})
	for _, x := range d {
		if x.ID == a && x.Usage.Total != 33 {
			t.Fatalf("the built DTO moved: %+v", x.Usage)
		}
	}
}

// V0.usage-jobs: a project's setup thread is Fyr's job in the ledger.
func TestThreadSourceOfASetupRun(t *testing.T) {
	e, _, _ := bare(t)
	a, b := bareThread(t, e), bareThread(t, e)
	e.mu.Lock()
	e.projects["p_x"] = &core.Project{ID: "p_x", Setup: &core.ProjectSetup{Thread: a}}
	e.mu.Unlock()
	if got := e.ThreadSource(a); got != "job:setup" {
		t.Errorf("setup thread: %q", got)
	}
	if got := e.ThreadSource(b); got != "" {
		t.Errorf("other thread: %q", got)
	}
}
