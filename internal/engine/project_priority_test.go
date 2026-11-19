package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/slots"
	"github.com/meros/fyr/internal/store"
)

// V0.project-priority: ProjectRank follows the project's priority and the
// thread's project, at once and after a restart, and the slot pool asks it.
func TestVProjectPriorityRank(t *testing.T) {
	h := newHarness(t, time.Minute)
	ws := filepath.Join(h.dir, "acme")
	os.MkdirAll(ws, 0o755)
	h.cmd("project.create", "", core.Args{"name": "acme", "folder": ws})
	th := h.cmd("thread.create", "", core.Args{"cwd": ws, "text": "x"})
	other := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "y"})
	p := projectOf(h.e, th.ThreadID)
	if h.e.ProjectRank(th.ThreadID) != 0 {
		t.Fatal("a normal project ranks")
	}
	if r := h.cmd("project.update", "", core.Args{"id": p.ID, "priority": "high"}); !r.OK {
		t.Fatalf("%+v", r)
	}
	if h.e.ProjectRank(th.ThreadID) != 1 || h.e.ProjectRank(other.ThreadID) != 0 {
		t.Fatalf("high: %d %d", h.e.ProjectRank(th.ThreadID), h.e.ProjectRank(other.ThreadID))
	}
	// A thread filed into the project by hand takes its rank.
	h.cmd("thread.setProject", other.ThreadID, core.Args{"projectId": p.ID})
	if h.e.ProjectRank(other.ThreadID) != 1 {
		t.Fatal("a filed thread kept its rank")
	}
	// A new thread in its folder has it from the start.
	n := h.cmd("thread.create", "", core.Args{"cwd": ws, "text": "z"})
	if h.e.ProjectRank(n.ThreadID) != 1 {
		t.Fatal("a new thread has no rank")
	}
	pool := slots.New(1)
	h.e.UseSlots(pool)
	pool.Acquire("t_holder", slots.Test)
	pool.Acquire("t_none", slots.Test)
	pool.Acquire(th.ThreadID, slots.Test)
	if w := pool.Snapshot().Waiting; w[0].Owner != th.ThreadID {
		t.Fatalf("the high thread waits behind: %+v", w)
	}
	h.cmd("project.update", "", core.Args{"id": p.ID, "priority": "low"})
	if h.e.ProjectRank(th.ThreadID) != -1 {
		t.Fatal("low")
	}
	h.m.StopAll()
	h.e.st.Close()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	e2.Load()
	if e2.ProjectRank(th.ThreadID) != -1 {
		t.Fatal("the rank is gone after a restart")
	}
}
