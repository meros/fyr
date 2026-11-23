package cgroup

// V0.guard-finish: the process table's Mechanism, on its own type, serves the
// guard on a host without cgroups (macOS, Linux without the tree). These tests
// read a fixed `ps` and record the signals: no cgroup, no real process.

import (
	"fmt"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

var (
	_ guard.Mechanism = (*ProcMechanism)(nil)
	_ guard.Mechanism = (*Mechanism)(nil)
	_ guard.Parts     = (*ProcMechanism)(nil)
	_ guard.Parts     = (*Mechanism)(nil)
	_ Control         = (*ProcStops)(nil)
	_ Control         = (*JobGroups)(nil)
)

// flaggedSampler is a sampler over the process table whose detector flagged
// t_a's undeclared work (pid 20 and its child 21), as TestV207B drives it.
func flaggedSampler(t *testing.T, out *string, stops *ProcStops) *Sampler {
	t.Helper()
	now := boot.Add(time.Hour)
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal: 16000000 kB\nMemAvailable: 12000000 kB\n")
	cli := psRow(10, 1, 100000, 3600, 1, "claude") + psRow(11, 10, 90000, 3500, 1, "node mcp.js")
	*out = cli
	s := &Sampler{Proc: proc, Cores: 8, Groups: &JobGroups{}, Control: stops, Detect: &Detector{}, Trees: stops.Trees,
		CLIs: stops.CLIs, Now: func() time.Time { return now }}
	for i := 0; i < 4; i++ {
		s.Tick()
		now = now.Add(5 * time.Second)
	}
	for i := 1; i <= 20; i++ {
		*out = cli + psRow(20, 10, 4000, 5*i, 0, "sh job.sh") + psRow(21, 20, 3<<20, 5*i, float64(20*i), "node check.js")
		s.Tick()
		now = now.Add(5 * time.Second)
	}
	if fl := s.Detect.Flags(); len(fl) != 1 || fl[0].Thread != "t_a" {
		t.Fatalf("flags %+v", fl)
	}
	return s
}

func TestProcMechanismFreezesThawsAndKillsUndeclaredWorkWithoutCgroups(t *testing.T) {
	var out string
	var sent []sentSignal
	stops := stopGroups(&out, &memKV{}, &sent)
	s := flaggedSampler(t, &out, stops)
	m := &ProcMechanism{Stops: stops, Sampler: s, Detector: s.Detect}
	if m.FreezeUndeclared("t_b") {
		t.Fatal("a thread without a flag was stopped")
	}
	if mem, ok := m.RootsMem("t_a"); !ok || mem != (4000+3<<20)*1024 {
		t.Fatalf("roots hold %d %t", mem, ok)
	}
	if _, ok := m.RootsMem("t_b"); ok {
		t.Fatal("a thread without a flag has roots")
	}
	if m.Units("t_a") != nil || m.Rotate("t_a", []resource.WorkUnit{{PID: 20}, {PID: 21}}, 20) || m.Faults("t_a") != 0 {
		t.Fatal("the process table took turns")
	}
	if !m.FreezeUndeclared("t_a") || fmt.Sprint(sent) != fmt.Sprint([]sentSignal{{20, syscall.SIGSTOP}, {21, syscall.SIGSTOP}}) {
		t.Fatalf("not stopped: %v", sent)
	}
	if u := s.Undeclared(); len(u) != 1 || !u[0].Paused {
		t.Fatalf("the sampler does not see it paused: %+v", u)
	}
	sent = nil
	m.ThawUndeclared("t_a")
	if len(sent) != 2 || sent[0].sig != syscall.SIGCONT || stops.Frozen("t_a") {
		t.Fatalf("not resumed: %v", sent)
	}
	if !m.FreezeUndeclared("t_a") {
		t.Fatal("not stopped again")
	}
	sent = nil
	if n := m.Kill("t_a", true); n != 2 || stops.Frozen("t_a") {
		t.Fatalf("killed %d: %v", n, sent)
	}
	if m.Sample().Provider == "" {
		t.Fatal("no signals")
	}
}

// A job the guard stopped resumes when its hold ends, as JobGroups ends a
// job cgroup: the sampler ticks the host's job control with the holders.
func TestProcStopsResumeAJobWhenItsHoldEnds(t *testing.T) {
	out := psRow(10, 1, 1, 600, 1, "claude") + psRow(20, 10, 1, 9, 1, "make test") + psRow(21, 20, 1, 8, 1, "go test")
	var sent []sentSignal
	p := stopGroups(&out, &memKV{}, &sent)
	m := &ProcMechanism{Stops: p}
	p.Tick([]string{"t_a"})
	p.Seen("t_a", "make test")
	if !m.Freeze("t_a") || !p.Frozen("t_a") || len(sent) != 2 {
		t.Fatalf("not stopped: %v", sent)
	}
	if p.JobEmpty("t_a") {
		t.Fatal("its processes live")
	}
	sent = nil
	p.Tick([]string{"t_a"})
	if len(sent) != 0 {
		t.Fatalf("a held job resumed: %v", sent)
	}
	p.Tick(nil)
	if p.Frozen("t_a") || len(sent) != 2 || sent[0].sig != syscall.SIGCONT {
		t.Fatalf("the ended hold's job was not resumed: %v", sent)
	}
	if m.Freeze("t_a") {
		t.Fatal("the ended hold's command is still known")
	}
}

// A nil ProcStops (a host with the cgroup tree) stops nothing.
func TestProcStopsNilDoesNothing(t *testing.T) {
	var p *ProcStops
	p.Seen("t_a", "make")
	p.Tick([]string{"t_a"})
	p.EndUndeclared("t_a")
	p.ResumeStopped()
	if p.Freeze("t_a") || p.FreezeUndeclared("t_a", []Root{{PID: 20}}) || p.Frozen("t_a") || p.JobEmpty("t_a") || p.UndeclaredEmpty("t_a") || p.KillJob("t_a", false) != 0 {
		t.Fatal("a nil ProcStops did something")
	}
	p.Thaw("t_a")
	p.ThawUndeclared("t_a")
}

// The agents' shares for the tools that ignore cgroups (V0.job-limits), as
// fyrd's wiring computed them in a closure before V0.guard-finish.
func TestJobHintsFromTheAgentsCapacity(t *testing.T) {
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal: 16000000 kB\nMemAvailable: 12000000 kB\n")
	s := &Sampler{Proc: proc, Cores: 8}
	s.Tick()
	h := JobHints{Sampler: s, Worker: func(c string, n int) string { return fmt.Sprintf("%s: %d workers", c, n) }}
	sig := s.Signals()
	c := Capacity{CPU: sig.CPU, Memory: sig.Memory}
	if fmt.Sprint(h.Env()) != fmt.Sprint(JobEnv(c)) {
		t.Fatalf("env %v, want %v", h.Env(), JobEnv(c))
	}
	if got, want := h.Hint("t_a", "pytest -n auto"), fmt.Sprintf("pytest -n auto: %d workers", Workers(c)); got != want {
		t.Fatalf("hint %q, want %q", got, want)
	}
}
