package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// V0.handover-filed: a thread handed over from another host is filed by its
// reduce (core.Filed) like any other thread: by its folder when the sender
// chose no project, in the chosen project by hand otherwise; the thread
// invariants check the handed-in thread and find nothing.
func TestHandInFiledByTheReduce(t *testing.T) {
	e, _, _ := bare(t)
	out := invLog(t, e)
	e.ProfileExists = func(id string) bool { return id == "claude" }
	acme, beta := t.TempDir(), t.TempDir()
	sub := filepath.Join(acme, "app")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	pid := map[string]string{}
	for name, dir := range map[string]string{"acme": acme, "beta": beta} {
		if r := e.Command(api.Cmd{CommandID: NewID("c"), Action: core.KindProjectCreate, Args: core.Args{"name": name, "folder": dir}}); !r.OK {
			t.Fatalf("project.create %s: %+v", name, r)
		}
	}
	e.mu.Lock()
	for id, p := range e.projects {
		pid[p.Name] = id
	}
	e.mu.Unlock()

	cases := []struct {
		name, project, want string
		manual              bool
	}{
		{"by folder", "", pid["acme"], false},
		{"chosen", pid["beta"], pid["beta"], true},
	}
	for _, c := range cases {
		tid, _, got, _, status, msg := e.PrepareHandIn(context.Background(), HandInSpec{
			FromHostID: "h_a", FromHostName: "host-a", FromThreadID: "t_src",
			Title: "moved", Cwd: sub, ProjectID: c.project, Profile: "claude",
		})
		if status != 200 {
			t.Fatalf("%s: hand-in: %d %s", c.name, status, msg)
		}
		e.mu.Lock()
		th := *e.threads[tid]
		e.mu.Unlock()
		if got != c.want || th.ProjectID != c.want || th.ProjectManual != c.manual {
			t.Fatalf("%s: filed in %q (reply %q) manual=%v, want %q manual=%v", c.name, th.ProjectID, got, th.ProjectManual, c.want, c.manual)
		}
		// The reduce picks what Filed picks: a replay of the stored thread
		// files it the same way.
		if id, m := core.Filed(&th, e.projects); id != th.ProjectID || m != th.ProjectManual {
			t.Fatalf("%s: Filed says %q manual=%v, thread has %q manual=%v", c.name, id, m, th.ProjectID, th.ProjectManual)
		}
		e.inv.mu.Lock()
		ring := strings.Join(e.inv.threadRing[tid], "\n")
		e.inv.mu.Unlock()
		if !strings.Contains(ring, core.KindThreadHandedIn) {
			t.Fatalf("%s: thread.handedIn was not checked:\n%s", c.name, ring)
		}
	}
	if strings.Contains(out.String(), "invariant: ") {
		t.Fatalf("invariant violations:\n%s", out.String())
	}
}
