// V0.164 section 4.5 and 12.2 (C1, C2), 12.6 (P): the commands behind an episode, from a fake /proc.
package cgroup

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProc2 is one process of a fake /proc with its start time (clock ticks after boot, 100 per second) and its
// resident pages.
func fakeProc2(t *testing.T, proc string, pid, ppid int, start int, rssPages int, argv ...string) {
	t.Helper()
	write(t, filepath.Join(proc, fmt.Sprint(pid), "cmdline"), strings.Join(argv, "\x00")+"\x00")
	// pid (comm) state ppid pgrp session tty tpgid flags minflt cminflt majflt cmajflt utime stime cutime cstime
	// priority nice threads itrealvalue starttime
	write(t, filepath.Join(proc, fmt.Sprint(pid), "stat"), fmt.Sprintf("%d (x) S %d 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 1 0 %d 0 0\n", pid, ppid, start))
	write(t, filepath.Join(proc, fmt.Sprint(pid), "statm"), fmt.Sprintf("100 %d 0 0 0 0 0\n", rssPages))
}

var boot = time.Date(2026, 11, 17, 8, 0, 0, 0, time.UTC)

func TestC1TheRootIsTheNewShellAndItsCommandTheOneTheGateSaw(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := tr.Thread("t_a")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), fmt.Sprintf("cpu 1 2 3\nbtime %d\n", boot.Unix()))
	over := boot.Add(2 * time.Hour) // the first reading over, 7200 s after boot
	tick := func(s float64) int { return int(s * 100) }
	fakeProc2(t, proc, 10, 1, tick(100), 100, "claude", "--output-format", "stream-json")                                 // the CLI
	fakeProc2(t, proc, 11, 10, tick(110), 300000, "node", "mcp-server.js")                                                // an old MCP server, big
	fakeProc2(t, proc, 20, 10, tick(7200-5), 10, "/bin/zsh", "-c", "source snap && eval 'cd web && yarn pre-push:check'") // new: 5 s before the first over reading
	fakeProc2(t, proc, 21, 20, tick(7200+1), 4000, "node", "x.js")
	fakeProc2(t, proc, 22, 20, tick(7200+2), 2000, "node", "y.js")
	fakeProc2(t, proc, 30, 10, tick(7200-4), 5, "/bin/zsh", "-c", "git status") // a second small root
	write(t, filepath.Join(td, "cgroup.procs"), "10\n11\n20\n21\n22\n30\n")
	roots := ReadRoots(tr, proc, "t_a", over)
	if len(roots) != 2 || roots[0].PID != 20 || roots[1].PID != 30 {
		t.Fatalf("roots %+v", roots)
	}
	if roots[0].RSS != uint64(10+4000+2000)*uint64(pageSize()) {
		t.Fatalf("a root's memory is its subtree's: %d", roots[0].RSS)
	}
	// The command: the newest Bash command the gate saw that the root's command line holds.
	got := CommandOf(roots[0], []string{"ls", "yarn pre-push:check", "git status"}) // oldest first
	if got != "yarn pre-push:check" {
		t.Fatalf("command %q", got)
	}
}

func TestC2WithoutAMatchTheRootsOwnCommandLineCut(t *testing.T) {
	long := strings.Repeat("x", 400)
	r := Root{PID: 7, Cmdline: "node " + long}
	got := CommandOf(r, []string{"ls"})
	if !strings.HasPrefix(got, "node xxx") || len([]rune(got)) != 300 {
		t.Fatalf("%d %q", len(got), got[:20])
	}
	// A too short command matches anything and is not used.
	if got := CommandOf(Root{Cmdline: "node a.js"}, []string{"a"}); got != "node a.js" {
		t.Fatalf("%q", got)
	}
}

// ---- section 10 / 12.6: without cgroups, the process tree under the CLI ----

func TestP1AProcTreeUnderTheCLIGrowingTwoCoresPerSecond(t *testing.T) {
	proc := t.TempDir()
	write(t, filepath.Join(proc, "stat"), fmt.Sprintf("cpu 1 2 3\nbtime %d\n", boot.Unix()))
	fakeProc2(t, proc, 10, 1, 100, 100, "claude")
	fakeProc2(t, proc, 20, 10, 200, 50, "node", "job.js")
	r := &TreeReader{Proc: proc}
	now := boot.Add(time.Hour)
	set := func(utime int) {
		write(t, filepath.Join(proc, "20", "stat"), fmt.Sprintf("20 (x) S 10 0 0 0 0 0 0 0 0 0 %d 0 0 0 0 0 1 0 200 0 0\n", utime))
	}
	r.Read(now, map[string]int{"t_a": 10})
	var cores float64
	for i := 1; i <= 12; i++ { // 60 s: 2 core-seconds per second = 200 ticks per second
		now = now.Add(5 * time.Second)
		set(i * 5 * 200)
		for _, rd := range r.Read(now, map[string]int{"t_a": 10}) {
			if rd.CPU != nil {
				cores = *rd.CPU
			}
		}
	}
	if cores < 1.9 || cores > 2.1 {
		t.Fatalf("cores %.2f", cores)
	}
	d := &Detector{}
	// Fed to the detector, the growth flags after W (the readings above, replayed).
	n := boot.Add(time.Hour)
	s := Capacity{CPU: 4, Memory: 14e9}
	two := 2.0
	d.Observe(n, DetectInput{Capacity: &s, Config: DefaultUndeclared, Reads: []Reading{{Thread: "t_a", Work: 1e9}}})
	for i := 0; i < 10; i++ {
		n = n.Add(5 * time.Second)
		d.Observe(n, DetectInput{Capacity: &s, Config: DefaultUndeclared, Reads: []Reading{{Thread: "t_a", CPU: &two, Work: 1e9}}})
	}
	if len(d.Flags()) != 1 {
		t.Fatal("not flagged")
	}
}

func TestP2PsOutputInTheMacOSFormSumsEachThreadsTree(t *testing.T) {
	out := "  10     1  100000 0:01.00\n  20    10  400000 0:05.00\n  21    20  100000 0:02.00\n  50     1   50000 0:00.10\n  60    50   25000 0:00.20\n   1     0   1000 0:00.00\n"
	got := TreesFromPS(out, map[string]int{"t_a": 10, "t_b": 50})
	if got["t_a"].RSS != (100000+400000+100000)*1024 || got["t_b"].RSS != (50000+25000)*1024 {
		t.Fatalf("%+v", got)
	}
	if got["t_a"].CPUSeconds < 7.9 || got["t_a"].CPUSeconds > 8.1 {
		t.Fatalf("cpu seconds %.2f", got["t_a"].CPUSeconds)
	}
}

func TestP3TheSameTracesGiveTheSameFlags(t *testing.T) {
	// D1, D3 and D6 are about the detector, which does not care where the readings came from; the tree reader's
	// readings carry no inactive_file, so Work is the summed RSS.
	for name, run := range map[string]func(s *sim){
		"D1": func(s *sim) { s.run("t_a", 600, 0.03, 0.9*gb) },
		"D3": func(s *sim) { s.run("t_a", 20, 4, 0.9*gb); s.run("t_a", 100, 0.03, 0.9*gb) },
		"D6": func(s *sim) { s.run("t_a", 20, 0.1, 3.9*gb); s.run("t_a", 100, 0.03, 0.9*gb) },
	} {
		s := newSim(t)
		s.seed("t_a", 0.9*gb)
		run(s)
		if len(s.d.Flags()) != 0 || len(s.ends) != 0 {
			t.Errorf("%s flagged", name)
		}
	}
}

// V0.201: the CLI's wrapper quotes the command; a wrapper that matches nothing names none.
func TestV201CommandInsideTheShellWrapper(t *testing.T) {
	w := `/bin/zsh -c source /home/u/.claude/shell-snapshots/snapshot-zsh-1.sh 2>/dev/null || true && export A=$'b'; eval 'cd x && grep '\''a b'\'' f
make check' < /dev/null`
	cmd := "cd x && grep 'a b' f\nmake check"
	if got := CommandOf(Root{Cmdline: w}, []string{"ls -la", cmd}); got != cmd {
		t.Fatalf("%q", got)
	}
	if got := CommandOf(Root{Cmdline: w}, []string{"ls -la"}); got != "" {
		t.Fatalf("%q", got)
	}
}
