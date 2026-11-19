// Decision 0032 slice 2: the units of a thread's undeclared work, each frozen
// on its own, one running at a time.
package cgroup

import (
	"os"
	"path/filepath"
	"testing"
)

// turboTree is the incident's shape: one root (the shell, then turbo) with
// four heavy children and a small one, beside the CLI.
func turboTree(t *testing.T) (*JobGroups, string) {
	t.Helper()
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	proc := t.TempDir()
	fakeProc2(t, proc, 10, 1, 100, 1000, "claude")
	fakeProc2(t, proc, 20, 10, 9000, 10, "/bin/zsh", "-c", "turbo run lint")
	fakeProc2(t, proc, 21, 20, 9001, 50, "turbo", "run", "lint")
	fakeProc2(t, proc, 30, 21, 9002, 2000, "tsc", "-p", "a")
	fakeProc2(t, proc, 31, 21, 9003, 2500, "tsc", "-p", "b")
	fakeProc2(t, proc, 32, 21, 9004, 1500, "tsgolint", "a")
	fakeProc2(t, proc, 33, 21, 9005, 1500, "tsgolint", "b")
	fakeProc2(t, proc, 34, 21, 9006, 5, "sh", "-c", "echo")
	fakeProc2(t, proc, 40, 30, 9007, 100, "tsc-worker")
	write(t, filepath.Join(td, "cgroup.procs"), "10\n20\n21\n30\n31\n32\n33\n34\n40\n")
	return &JobGroups{Tree: tr, Proc: proc}, td
}

func TestR1TheUnitsAreWhereTheMemoryFansOut(t *testing.T) {
	g, _ := turboTree(t)
	us := g.Units("t_a", []Root{{PID: 20}})
	var got []int
	for _, u := range us {
		got = append(got, u.PID)
	}
	want := []int{30, 31, 32, 33}
	if len(got) != len(want) {
		t.Fatalf("units %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("units %v, want %v", got, want)
		}
	}
	if us[0].RSS <= uint64(2000*os.Getpagesize()) {
		t.Fatalf("a unit's memory leaves out its child: %d", us[0].RSS)
	}
}

func TestR2AJobWithOneHeavyProcessIsOneUnit(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	proc := t.TempDir()
	fakeProc2(t, proc, 20, 10, 9000, 10, "/bin/zsh", "-c", "tsc")
	fakeProc2(t, proc, 21, 20, 9001, 3000, "tsc")
	write(t, filepath.Join(td, "cgroup.procs"), "20\n21\n")
	g := &JobGroups{Tree: tr, Proc: proc}
	us := g.Units("t_a", []Root{{PID: 20}})
	if len(us) != 1 || us[0].PID != 21 {
		t.Fatalf("%+v", us)
	}
}

func TestR3OneUnitRunsTheOthersAreFrozenAndTheTurnMoves(t *testing.T) {
	g, td := turboTree(t)
	us := g.Units("t_a", []Root{{PID: 20}})
	if !g.RotateUndeclared("t_a", us, 30) {
		t.Fatal("no rotation")
	}
	dir := func(pid string) string { return filepath.Join(td, "undeclared", "u"+pid) }
	state := func(pid string) string { return read(t, filepath.Join(dir(pid), "cgroup.freeze")) }
	if state("30") != "0" || state("31") != "1" || state("32") != "1" || state("33") != "1" {
		t.Fatalf("freeze: %s %s %s %s", state("30"), state("31"), state("32"), state("33"))
	}
	if !g.Frozen("t_a") {
		t.Fatal("the thread does not read as paused")
	}
	// The plain directory keeps the last pid written: the unit's child moved with it.
	if got := read(t, filepath.Join(dir("30"), "cgroup.procs")); got != "40" {
		t.Fatalf("moved last: %q", got)
	}
	// The parent stays thawed: a frozen parent would hold the running unit too.
	if got := read(t, filepath.Join(td, "undeclared", "cgroup.freeze")); got != "0" {
		t.Fatalf("undeclared/ freeze %q", got)
	}
	if !g.RotateUndeclared("t_a", us, 31) || state("30") != "1" || state("31") != "0" {
		t.Fatal("the turn did not move")
	}
}

func TestR4AThawReleasesEveryUnitAndTheEndRemovesThem(t *testing.T) {
	g, td := turboTree(t)
	us := g.Units("t_a", []Root{{PID: 20}})
	g.RotateUndeclared("t_a", us, 30)
	g.ThawUndeclared("t_a")
	ud := filepath.Join(td, "undeclared")
	for _, p := range []string{"30", "31", "32", "33"} {
		if got := read(t, filepath.Join(ud, "u"+p, "cgroup.freeze")); got != "0" {
			t.Fatalf("u%s freeze %q", p, got)
		}
		write(t, filepath.Join(ud, "u"+p, "cgroup.procs"), "")
	}
	if g.Frozen("t_a") {
		t.Fatal("still paused")
	}
	g.EndUndeclared("t_a")
	if _, err := os.Stat(ud); !os.IsNotExist(err) {
		t.Fatal("undeclared/ stays")
	}
}

func TestR5RootsAreStillFoundInsideUnits(t *testing.T) {
	g, td := turboTree(t)
	us := g.Units("t_a", []Root{{PID: 20}})
	g.RotateUndeclared("t_a", us, 30)
	write(t, filepath.Join(td, "cgroup.procs"), "10\n20\n21\n34\n")
	write(t, filepath.Join(td, "undeclared", "u30", "cgroup.procs"), "30\n40\n")
	write(t, filepath.Join(td, "undeclared", "u31", "cgroup.procs"), "31\n")
	write(t, filepath.Join(td, "undeclared", "u32", "cgroup.procs"), "32\n")
	write(t, filepath.Join(td, "undeclared", "u33", "cgroup.procs"), "33\n")
	again := g.Units("t_a", []Root{{PID: 20}})
	if len(again) != 4 || again[0].PID != 30 {
		t.Fatalf("after the move: %+v", again)
	}
}

func TestR6MajorFaultsAreReadAsBytes(t *testing.T) {
	g, td := turboTree(t)
	write(t, filepath.Join(td, "memory.stat"), "anon 1\npgmajfault 3\n")
	if got := g.MajorFaultBytes("t_a"); got != uint64(3*os.Getpagesize()) {
		t.Fatalf("%d", got)
	}
}

func TestR7AUnitStaysAUnitWhenItsMemoryChanges(t *testing.T) {
	g, td := turboTree(t)
	us := g.Units("t_a", []Root{{PID: 20}})
	g.RotateUndeclared("t_a", us, 30)
	// Unit 30 fans out into two heavy children of its own: it is still one unit.
	fakeProc2(t, g.Proc, 41, 30, 9008, 9000, "tsc-worker")
	fakeProc2(t, g.Proc, 42, 30, 9009, 9000, "tsc-worker")
	write(t, filepath.Join(td, "undeclared", "u30", "cgroup.procs"), "30\n40\n41\n42\n")
	write(t, filepath.Join(td, "cgroup.procs"), "10\n20\n21\n31\n32\n33\n34\n")
	again := g.Units("t_a", []Root{{PID: 20}})
	if len(again) != 4 || again[0].PID != 30 {
		t.Fatalf("%+v", again)
	}
}

func TestR8AUnitThatIsNoLongerListedIsLetRun(t *testing.T) {
	g, td := turboTree(t)
	us := g.Units("t_a", []Root{{PID: 20}})
	g.RotateUndeclared("t_a", us, 30)
	if !g.RotateUndeclared("t_a", us[:3], 30) {
		t.Fatal("no rotation")
	}
	if got := read(t, filepath.Join(td, "undeclared", "u33", "cgroup.freeze")); got != "0" {
		t.Fatalf("the unlisted unit stays frozen: %q", got)
	}
}
