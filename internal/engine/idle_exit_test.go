package engine

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	rnr "github.com/meros/fyr/internal/runner"
)

// V0.idle-exit-ends-tasks (#724): the agent's turn ended on a background
// command, and its idle process is then killed by a signal. Its tasks end
// with it: they read stopped, the thread has no Waiting claim for them, and
// the timeline says the process was lost.
func TestIdleProcessKilledEndsItsBackgroundTasks(t *testing.T) {
	t.Run("direct", func(t *testing.T) { idleKill(t, newHarness(t, time.Hour)) })
	t.Run("runner", func(t *testing.T) {
		sock := rnr.SocketPath(t.TempDir())
		srv := &rnr.Server{Logf: t.Logf}
		if err := srv.Listen(sock); err != nil {
			t.Fatal(err)
		}
		go srv.Serve()
		t.Cleanup(func() { srv.Close(time.Second) })
		dir := t.TempDir()
		idleKill(t, startHarnessWith(t, dir, filepath.Join(dir, "fyr.db"), time.Hour, &rnr.Client{Path: sock}, nil))
	})
}

func idleKill(t *testing.T, h *harness) {
	id := h.create("bgstuck")
	h.wait(id, "the turn ended on the background command", func(th core.Thread, a core.Attention) bool {
		return a.State == core.Waiting && !th.Running && len(th.Tasks) == 1 && th.Tasks[0].Status == core.TaskRunning
	})
	pid := h.m.CLIs()[id]
	if pid <= 0 {
		t.Fatalf("no pid for the thread's CLI: %v", h.m.CLIs())
	}
	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	th := h.wait(id, "the task reads stopped", func(th core.Thread, _ core.Attention) bool {
		return len(th.Tasks) == 1 && th.Tasks[0].Status == core.TaskStopped
	})
	v := h.viewOf(id)
	if a := core.ComputeWith(&th, &v, time.Now(), nil); a.State == core.Waiting {
		t.Fatalf("still Waiting: %q", a.Reason)
	}
	// The stop is in the timeline: an idle loss ends no run, so before
	// V0.idle-exit-ends-tasks it left no trace and read as never observed.
	if !hasEvent(h.events(id), "task", `"summary":"ended with the agent process"`) {
		t.Fatalf("the timeline does not say the task ended with the process: %v", h.events(id))
	}
}
