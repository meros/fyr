// V0.guard-tidy: no reclaim near oomd's limit, a restarted CLI is never a
// root, undeclared/'s limit lifts while its parts take turns, and a rotation
// after the episode's end does nothing.
package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Decision 0036 "Not chosen": a reclaim raises pgscan, so oomd would pick the
// work fyrd just reclaimed. Near oomd's limit the guard does not reclaim.
func TestGuardTidyNoReclaimNearOomdsLimit(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	o.OomdLimit = func(string) float64 { return 60 }
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	agents := filepath.Dir(td)
	write(t, filepath.Join(agents, "memory.swap.max"), "10240M")
	write(t, filepath.Join(agents, "memory.swap.current"), "0")
	jd := filepath.Join(td, jobDir)
	write(t, filepath.Join(jd, "memory.stat"), "anon 1073741824\n")
	write(t, filepath.Join(jd, "memory.reclaim"), "")
	write(t, filepath.Join(jd, "cgroup.freeze"), "1")
	root := filepath.Join(o.FS, unit)
	g := &JobGroups{Tree: tr, Proc: t.TempDir()}

	write(t, filepath.Join(root, "memory.pressure"), memPSI(55)) // within 10 points of 60
	g.reclaimFrozen("t_a")
	g.reclaimFrozen("t_a") // within the minute: one line
	if got := read(t, filepath.Join(jd, "memory.reclaim")); got != "" {
		t.Fatalf("reclaimed near oomd's limit: %q", got)
	}
	n := 0
	for _, s := range l.lines {
		if strings.Contains(s, "guard: no reclaim of t_a's frozen work near oomd's limit: unit_full=55% limit=60%") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d skip lines: %v", n, l.lines)
	}

	write(t, filepath.Join(root, "memory.pressure"), memPSI(20)) // far from it: V0.guard-swap's reclaim
	g.reclaimFrozen("t_a")
	if got := read(t, filepath.Join(jd, "memory.reclaim")); got != "1073741824 swappiness=200" {
		t.Fatalf("away from the limit: reclaim %q", got)
	}
}

// A CLI restarted during the episode (a handover) is new, but never a root:
// frozen, it would take its MCP servers with it. A Bash shell it starts is.
func TestGuardTidyARestartedCLIIsNoRoot(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), fmt.Sprintf("cpu 1 2 3\nbtime %d\n", boot.Unix()))
	over := boot.Add(2 * time.Hour)
	tick := func(s float64) int { return int(s * 100) }
	snap := "/home/u/.claude/shell-snapshots/snapshot-zsh-1.sh"
	fakeProc2(t, proc, 50, 1, tick(7200+30), 100, "claude", "--resume")                                    // restarted mid-episode
	fakeProc2(t, proc, 51, 50, tick(7200+31), 300000, "node", "mcp-server.js")                             // its MCP server
	fakeProc2(t, proc, 52, 50, tick(7200+40), 200, "/bin/zsh", "-c", "source "+snap+" && eval 'make e2e'") // its Bash
	fakeProc2(t, proc, 53, 52, tick(7200+41), 400000, "make", "e2e")
	write(t, filepath.Join(td, "cgroup.procs"), "50\n51\n52\n53\n")
	roots := ReadRootsWith(tr, proc, "t_a", over, Elders{CLI: 50, Min: 1 << 20})
	if len(roots) != 1 || roots[0].PID != 52 {
		t.Fatalf("roots %+v, want the Bash shell alone", roots)
	}
}

// undeclared/ holds the frozen parts too: while they take turns its limit is
// lifted, and put back when the turns end.
func TestGuardTidyTurnsLiftUndeclaredsLimit(t *testing.T) {
	g, td := turboTree(t)
	g.Cores = 8
	g.Size = func(string, bool) JobSize {
		return JobSize{Class: "lint", Peak: 1 << 30, Demand: 1, Known: true, Runs: 5}
	}
	ud := filepath.Join(td, undeclaredDir)
	write(t, filepath.Join(ud, "memory.high"), "max")
	g.sizeJob("t_a", ud, true) // FreezeUndeclared sized it earlier in the episode
	sized := read(t, filepath.Join(ud, "memory.high"))
	if sized == "max" {
		t.Fatal("setup: not sized")
	}
	if !g.RotateUndeclared("t_a", g.Units("t_a", []Root{{PID: 20}}), 30) {
		t.Fatal("no rotation")
	}
	if got := read(t, filepath.Join(ud, "memory.high")); got != "max" {
		t.Fatalf("undeclared/'s memory.high while taking turns: %s, want max", got)
	}
	g.resize() // the swap valve's tick keeps it lifted
	if got := read(t, filepath.Join(ud, "memory.high")); got != "max" {
		t.Fatalf("after a resize: %s", got)
	}
	g.ThawUndeclared("t_a")
	if got := read(t, filepath.Join(ud, "memory.high")); got != sized {
		t.Fatalf("after the turns: %s, want %s back", got, sized)
	}
}

// EndUndeclared (the sampler) and RotateUndeclared (the engine's tick) race:
// a rotation once the episode ended must not make and freeze u*/ again.
func TestGuardTidyNoRotationAfterTheEpisodeEnded(t *testing.T) {
	g, td := turboTree(t)
	live := true
	g.Live = func(string) bool { return live }
	us := g.Units("t_a", []Root{{PID: 20}})
	if !g.RotateUndeclared("t_a", us, 30) {
		t.Fatal("no rotation")
	}
	live = false // the detector ended the episode
	g.EndUndeclared("t_a")
	for _, d := range unitDirs(filepath.Join(td, undeclaredDir)) {
		os.RemoveAll(d) // the kernel removes the emptied units; the fake keeps their pids
	}
	if g.RotateUndeclared("t_a", us, 31) {
		t.Fatal("a rotation after the episode's end froze the work again")
	}
	if ds := unitDirs(filepath.Join(td, undeclaredDir)); len(ds) != 0 {
		t.Fatalf("units made again: %v", ds)
	}
	if g.Frozen("t_a") {
		t.Fatal("the thread reads as paused after its episode ended")
	}
}
