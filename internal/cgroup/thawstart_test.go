// V0.thaw-at-start: work a guard froze stays frozen when fyrd restarts, and
// the new fyrd remembers none of it; at start every frozen job/, undeclared/
// and undeclared/u*/ under a thread's cgroup is thawed. Release removes
// undeclared/ as it removes job/. The cgroupfs is plain directories here.
package cgroup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestThawAtStartThawsWhatTheLastFyrdLeftFrozen(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	write(t, filepath.Join(td, CLIDir, "cgroup.procs"), "10\n")
	frozen := []string{jobDir, undeclaredDir, filepath.Join(undeclaredDir, "u123")}
	for _, d := range frozen {
		write(t, filepath.Join(td, d, "cgroup.procs"), "20\n")
		write(t, filepath.Join(td, d, "cgroup.freeze"), "1\n")
	}
	ob := threadOf(tr, "t_b") // a thread whose job runs: nothing to thaw, nothing logged
	write(t, filepath.Join(ob, jobDir, "cgroup.freeze"), "0\n")

	g := &JobGroups{Tree: tr}
	g.ThawLeftFrozen()
	for _, d := range frozen {
		if got := read(t, filepath.Join(td, d, "cgroup.freeze")); got != "0" {
			t.Errorf("%s still frozen: %q", d, got)
		}
	}
	var thawed []string
	for _, s := range l.lines {
		if strings.HasPrefix(s, "guard: thawed") {
			thawed = append(thawed, s)
		}
	}
	if len(thawed) != 3 || !strings.Contains(strings.Join(thawed, "\n"), `thread=t_a`) || !strings.Contains(thawed[0], `reason="left frozen by the last fyrd"`) {
		t.Fatalf("one line per thaw: %q", thawed)
	}
}

// The scope layout: a thread's scope and a thread an older fyrd started in
// the legacy agents/ both get thawed.
func TestThawAtStartScopeAndLegacy(t *testing.T) {
	o, l, _ := scopeFS(t, true)
	o.ScopeProbe = func(prefix []string) (string, string, error) {
		return "/" + slice + "/" + scopePrefix + "probe-" + strconv.Itoa(o.PID) + scopeSuffix, "1", nil
	}
	old := Setup(Options{Budget: o.Budget, FS: o.FS, Self: o.Self, PID: o.PID, Probe: o.Probe, Logf: l.logf})
	legacyDir := filepath.Dir(old.Thread("t_old"))
	write(t, filepath.Join(legacyDir, CLIDir, "cgroup.procs"), "100\n")
	write(t, filepath.Join(legacyDir, undeclaredDir, "cgroup.freeze"), "1\n")
	o.Self = filepath.Join(t.TempDir(), "self")
	write(t, o.Self, "0::/"+unit+"/server\n")
	tr := Setup(o)
	if tr == nil || tr.Layout() != LayoutScope {
		t.Fatalf("tree %+v: %v", tr, l.lines)
	}
	scope := filepath.Join(o.FS, slice, "fyr-agent-t_new.scope")
	write(t, filepath.Join(scope, CLIDir, "cgroup.procs"), "200\n")
	write(t, filepath.Join(scope, jobDir, "cgroup.freeze"), "1\n")

	(&JobGroups{Tree: tr}).ThawLeftFrozen()
	for _, f := range []string{filepath.Join(legacyDir, undeclaredDir, "cgroup.freeze"), filepath.Join(scope, jobDir, "cgroup.freeze")} {
		if got := read(t, f); got != "0" {
			t.Errorf("%s: %q", f, got)
		}
	}
}

func TestReleaseRemovesUndeclared(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	write(t, filepath.Join(td, undeclaredDir, "u123", "cgroup.freeze"), "0\n")
	write(t, filepath.Join(td, undeclaredDir, "cgroup.freeze"), "0\n")
	tr.Release("t_a")
	// The kernel refuses to remove a cgroup with a child: u123/ goes first, then undeclared/.
	if _, err := os.Stat(filepath.Join(td, undeclaredDir)); !os.IsNotExist(err) {
		t.Fatal("undeclared/ was not removed")
	}
}
