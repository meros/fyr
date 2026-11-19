// Decision 0029 slice S5 (#189): a slot holder's heavy job goes into a cgroup of its own under its thread's
// (agents/<thread>/job/), found by its command line; it can be frozen and thawed; the cgroup goes when the job ends.
// The cgroupfs and /proc are plain directories here.
package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeProc(t *testing.T, proc string, pid, ppid int, argv ...string) {
	t.Helper()
	write(t, filepath.Join(proc, fmt.Sprint(pid), "cmdline"), strings.Join(argv, "\x00")+"\x00")
	write(t, filepath.Join(proc, fmt.Sprint(pid), "stat"), fmt.Sprintf("%d (x) S %d 0 0", pid, ppid))
}

func TestS5AHeavyJobGetsItsOwnCgroup(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	proc := t.TempDir()
	// The CLI (10), its shell running the job (20) and the job's child (21); an unrelated shell (30).
	fakeProc(t, proc, 10, 1, "claude", "--output-format", "stream-json")
	fakeProc(t, proc, 20, 10, "/bin/zsh", "-c", "source snap && eval 'cd web && make test-go 2>&1 | tail'")
	fakeProc(t, proc, 21, 20, "go", "test", "./...")
	fakeProc(t, proc, 30, 10, "/bin/zsh", "-c", "git status")
	write(t, filepath.Join(td, "cgroup.procs"), "10\n20\n21\n30\n")
	g := &JobGroups{Tree: tr, Proc: proc}

	g.Sync("t_a", "make test-go")
	job := filepath.Join(td, "job")
	if got := read(t, filepath.Join(job, "cgroup.procs")); got != "21" && !strings.Contains(got, "21") {
		t.Fatalf("the job's processes were not moved: %q", got)
	}
	if !g.Has("t_a") {
		t.Fatal("no job group")
	}
	// Freeze and thaw.
	if !g.Freeze("t_a") || read(t, filepath.Join(job, "cgroup.freeze")) != "1" || !g.Frozen("t_a") {
		t.Fatal("freeze")
	}
	g.Thaw("t_a")
	if read(t, filepath.Join(job, "cgroup.freeze")) != "0" || g.Frozen("t_a") {
		t.Fatal("thaw")
	}
	// Nothing matches: no group; an ended job's empty cgroup goes.
	if g.Sync("t_b", "make test"); g.Has("t_b") {
		t.Fatal("a group without a job")
	}
	write(t, filepath.Join(job, "cgroup.procs"), "")
	g.End("t_a")
	if _, err := os.Stat(job); !os.IsNotExist(err) || g.Has("t_a") {
		t.Fatal("the ended job's cgroup is still there")
	}
}

func TestS5ReleaseRemovesTheJobCgroupFirst(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	os.Mkdir(filepath.Join(td, "job"), 0o755)
	tr.Release("t_a")
	// The kernel refuses to remove a cgroup with a child; here, the child is gone before the thread's turn.
	if _, err := os.Stat(filepath.Join(td, "job")); !os.IsNotExist(err) {
		t.Fatal("the job's cgroup was not removed before its thread's")
	}
}
