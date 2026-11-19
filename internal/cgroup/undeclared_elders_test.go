// V0.freeze-old-work: an episode's roots also take the older work that holds its memory, so the freeze names and
// holds a long job, not the newest small process (#344).
package cgroup

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// The 19 Nov case: a build detached with setsid 26 min before the episode, and a waiter that forks a fresh `sleep 30`.
func TestElderTheDetachedBuildIsTheRootNotTheWaitersSleep(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), fmt.Sprintf("cpu 1 2 3\nbtime %d\n", boot.Unix()))
	over := boot.Add(2 * time.Hour)
	tick := func(s float64) int { return int(s * 100) }
	snap := "/home/u/.claude/shell-snapshots/snapshot-zsh-1.sh"
	fakeProc2(t, proc, 10, 1, tick(100), 100, "claude", "--output-format", "stream-json")                                                                   // the CLI
	fakeProc2(t, proc, 11, 10, tick(110), 300000, "node", "mcp-server.js")                                                                                  // an old MCP server, big
	fakeProc2(t, proc, 20, 10, tick(7200-1560), 200, "/bin/zsh", "-c", "source "+snap+" && eval 'timeout 3h sh -c \"while kill -0 7; do sleep 30; done\"'") // the waiter
	fakeProc2(t, proc, 21, 20, tick(7200+200), 50, "sleep", "30")                                                                                           // its fresh sleep
	fakeProc2(t, proc, 30, 1, tick(7200-1566), 100, "timeout", "3h", "uv", "run", "python", "build_admin_ny.py")                                            // detached: its parent is outside
	fakeProc2(t, proc, 31, 30, tick(7200-1565), 2000, "uv", "run", "python", "build_admin_ny.py")
	fakeProc2(t, proc, 32, 31, tick(7200-1564), 500000, "python", "build_admin_ny.py") // about 2 GB
	write(t, filepath.Join(td, "cgroup.procs"), "10\n11\n20\n21\n30\n31\n32\n")

	// Before: only the sleep was new, so it was the work.
	if r := ReadRoots(tr, proc, "t_a", over); len(r) != 1 || r[0].PID != 21 {
		t.Fatalf("without elders %+v", r)
	}
	mem := uint64(500000) * uint64(pageSize())
	roots := ReadRootsWith(tr, proc, "t_a", over, Elders{CLI: 10, Min: mem / 4})
	h, ok := Heaviest(roots)
	if !ok || h.PID != 30 {
		t.Fatalf("heaviest %+v of %+v", h, roots)
	}
	for _, r := range roots {
		if r.PID == 10 || r.PID == 11 || r.PID == 20 {
			t.Fatalf("the CLI, its MCP server and the small waiter stay residents: %+v", roots)
		}
	}
	// Without the CLI's pid no detached process is taken: it could be the CLI.
	if r := ReadRootsWith(tr, proc, "t_a", over, Elders{Min: mem / 4}); len(r) != 1 || r[0].PID != 21 {
		t.Fatalf("no CLI %+v", r)
	}
}

// A long run_in_background Bash whose memory grew late is an elder, and the new processes under it are not roots of
// their own.
func TestElderAnOldBackgroundBashHoldsTheMemory(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), fmt.Sprintf("cpu 1 2 3\nbtime %d\n", boot.Unix()))
	over := boot.Add(2 * time.Hour)
	tick := func(s float64) int { return int(s * 100) }
	snap := "/home/u/.claude/shell-snapshots/snapshot-zsh-1.sh"
	fakeProc2(t, proc, 10, 1, tick(100), 100, "claude")
	fakeProc2(t, proc, 20, 10, tick(3600), 200, "/bin/zsh", "-c", "source "+snap+" && eval 'python build.py'")
	fakeProc2(t, proc, 21, 20, tick(3601), 600000, "python", "build.py")
	fakeProc2(t, proc, 22, 21, tick(7200+5), 100, "gdal", "x") // new, under the elder
	fakeProc2(t, proc, 30, 10, tick(7200+3), 10, "/bin/zsh", "-c", "source "+snap+" && eval 'git status'")
	write(t, filepath.Join(td, "cgroup.procs"), "10\n20\n21\n22\n30\n")
	roots := ReadRootsWith(tr, proc, "t_a", over, Elders{CLI: 10, Min: 100 << 20})
	if len(roots) != 2 || roots[0].PID != 20 || roots[1].PID != 30 {
		t.Fatalf("roots %+v", roots)
	}
	if got := CommandOf(roots[0], []string{"python build.py", "git status"}); got != "python build.py" {
		t.Fatalf("command %q", got)
	}
}

// Without cgroups: an old Bash of the CLI that holds the memory is a root; an old small one is not.
func TestElderProcessTree(t *testing.T) {
	now := time.Date(2026, 11, 19, 11, 0, 0, 0, time.UTC)
	wrap := func(c string) string {
		return "/bin/zsh -c source /Users/u/.claude/shell-snapshots/snapshot-zsh-1.sh && eval '" + c + "'"
	}
	out := psRow(10, 1, 100000, 7200, 1, "claude") + psRow(11, 10, 900000, 7100, 1, "node mcp.js") +
		psRow(20, 10, 4000, 1800, 0, wrap("python build.py")) + psRow(21, 20, 3<<20, 1790, 600, "python build.py") +
		psRow(30, 10, 2000, 1500, 0, wrap("while kill -0 7; do sleep 30; done")) + psRow(31, 30, 500, 5, 0, "sleep 30")
	tab := parseTreePS(out, now)
	cut := now.Add(-60 * time.Second)
	if r := tab.roots(10, cut, 0); len(r) != 1 || r[0].PID != 31 {
		t.Fatalf("without elders %+v", r)
	}
	r := tab.roots(10, cut, 512<<20)
	if h, _ := Heaviest(r); h.PID != 20 || len(r) != 2 {
		t.Fatalf("roots %+v", r)
	}
}
