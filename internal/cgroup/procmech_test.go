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
	"github.com/meros/fyr/internal/heavy"
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
	if us := m.Units("t_a"); len(us) != 1 || us[0].PID != 21 || us[0].RSS != (3<<20)*1024 {
		t.Fatalf("units %+v", us)
	}
	if m.Faults("t_a") != 0 {
		t.Fatal("the process table reported read-back faults")
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

// V0.mac-load-parity: without cgroups (macOS), multi-part undeclared work takes
// turns via SIGSTOP/SIGCONT (decision 0032 slice 2), stopping the inactive parts
// before resuming the active one, and Kill kills every part including the active.
func TestProcMechanismTakesTurnsWithoutCgroups(t *testing.T) {
	now := boot.Add(time.Hour)
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal: 16000000 kB\nMemAvailable: 12000000 kB\n")
	cli := psRow(10, 1, 100000, 3600, 1, "claude")
	out := cli
	var sent []sentSignal
	kv := &memKV{}
	stops := stopGroups(&out, kv, &sent)
	g := &JobGroups{}
	g.Bash("t_a", "turbo build")
	s := &Sampler{Proc: proc, Cores: 8, Groups: g, Control: stops, Detect: &Detector{}, Trees: stops.Trees,
		CLIs: stops.CLIs, Now: func() time.Time { return now }}
	for i := 0; i < 4; i++ {
		s.Tick()
		now = now.Add(5 * time.Second)
	}
	for i := 1; i <= 20; i++ {
		out = cli +
			psRow(20, 10, 4000, 5*i, 1, "turbo build") +
			psRow(21, 20, 2<<20, 5*i, float64(10*i), "node vite.js") +
			psRow(22, 20, 1<<20, 5*i, float64(10*i), "node tsc.js") +
			psRow(23, 22, 1<<20, 5*i, float64(5*i), "node worker.js")
		s.Tick()
		now = now.Add(5 * time.Second)
	}
	m := &ProcMechanism{Stops: stops, Sampler: s, Detector: s.Detect, Bashes: g.Bashes}
	units := m.Units("t_a")
	if len(units) != 2 || units[0].PID != 21 || units[1].PID != 22 || units[1].RSS != (2<<20)*1024 {
		t.Fatalf("units %+v", units)
	}
	// First turn: 21 runs, 22 and its child 23 stop.
	if !m.Rotate("t_a", units, 21) || !stops.Frozen("t_a") {
		t.Fatalf("first rotate failed: %v", sent)
	}
	if fmt.Sprint(sent) != fmt.Sprint([]sentSignal{{22, syscall.SIGSTOP}, {23, syscall.SIGSTOP}}) {
		t.Fatalf("first turn signals %v", sent)
	}
	// Second turn: 21 stops BEFORE 22 and 23 resume.
	sent = nil
	if !m.Rotate("t_a", units, 22) {
		t.Fatalf("second rotate failed: %v", sent)
	}
	if fmt.Sprint(sent) != fmt.Sprint([]sentSignal{{21, syscall.SIGSTOP}, {22, syscall.SIGCONT}, {23, syscall.SIGCONT}}) {
		t.Fatalf("second turn signals %v", sent)
	}
	// Kill kills both the stopped unit (21) and the active unit (22, 23).
	sent = nil
	if n := m.Kill("t_a", true); n != 3 || stops.Frozen("t_a") {
		t.Fatalf("killed %d: %v", n, sent)
	}
}

// V0.mac-load-parity: on a host without cgroups, Services reads each thread's
// process tree, ProcStops.Holds protects a job holding a service, and
// Sampler.Roots excludes services so FreezeUndeclared never SIGSTOPs one.
func TestProcServicesAreProtectedWithoutCgroups(t *testing.T) {
	now := boot.Add(time.Hour)
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal: 16000000 kB\nMemAvailable: 12000000 kB\n")
	cli := psRow(10, 1, 100000, 3600, 1, "claude") +
		psRow(20, 10, 2000, 3500, 0, "zsh -c process-compose up") +
		psRow(21, 20, 10000, 3499, 1, "process-compose up") +
		psRow(22, 21, 2<<20, 3498, 2, "postgres")
	out := cli
	var sent []sentSignal
	stops := stopGroups(&out, &memKV{}, &sent)
	svcBook := &heavy.Services{}
	services := &Services{Match: svcBook.Match, Trees: stops.Trees, CLIs: stops.CLIs, Now: func() time.Time { return now }}
	s := &Sampler{Proc: proc, Cores: 8, Groups: &JobGroups{}, Control: stops, Detect: &Detector{},
		Services: services, Trees: stops.Trees, CLIs: stops.CLIs, Now: func() time.Time { return now }}
	for i := 0; i < 4; i++ {
		s.Tick()
		now = now.Add(5 * time.Second)
	}
	// The service is detected from the process tree, reserved, and NOT flagged as undeclared.
	if sv := services.Of("t_a"); len(sv) != 1 || sv[0].PID != 21 || sv[0].Memory != (10000+2<<20)*1024 {
		t.Fatalf("services %+v", sv)
	}
	if fl := s.Detect.Flags(); len(fl) != 0 {
		t.Fatalf("service flagged as undeclared: %+v", fl)
	}
	// HoldChecker reports false for an unrelated heavy job and true if the job holds the service.
	check := services.HoldChecker(stops)
	stops.Seen("t_a", "make test")
	if check("t_a") {
		t.Fatal("unrelated job reported as holding service")
	}
	stops.Seen("t_a", "process-compose up")
	if !check("t_a") {
		t.Fatal("job holding service not protected")
	}
	stops.Seen("t_a", "make test")
	// Add undeclared heavy work beside the service: Roots excludes the service so only pid 30+31 stop.
	for i := 1; i <= 20; i++ {
		out = cli + psRow(30, 10, 4000, 5*i, 0, "zsh -c node heavy.js") + psRow(31, 30, 3<<20, 5*i, float64(20*i), "node heavy.js")
		s.Tick()
		now = now.Add(5 * time.Second)
	}
	fl := s.Detect.Flags()
	if len(fl) != 1 {
		t.Fatalf("expected undeclared flag for heavy.js: %+v", fl)
	}
	roots := s.Roots(fl[0])
	if len(roots) != 1 || roots[0].PID != 30 {
		t.Fatalf("roots should skip service (pid 20/21/22), got %+v", roots)
	}
	m := &ProcMechanism{Stops: stops, Sampler: s, Detector: s.Detect}
	if !m.FreezeUndeclared("t_a") || fmt.Sprint(sent) != fmt.Sprint([]sentSignal{{30, syscall.SIGSTOP}, {31, syscall.SIGSTOP}}) {
		t.Fatalf("unexpected freeze signals: %v", sent)
	}
}

// V0.mac-load-parity: without cgroups, a critical memory stall fires OnCritical
// on entry and at most once per TriggerFresh while it stays critical.
func TestSamplerOnCriticalWakesGuardWithoutCgroups(t *testing.T) {
	now := boot
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal: 16000000 kB\nMemAvailable: 12000000 kB\n")
	write(t, filepath.Join(proc, "pressure", "memory"), "some avg10=0.00 avg60=0.00 avg300=0.00 total=0\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n")
	woke := 0
	s := &Sampler{Proc: proc, Cores: 8, Now: func() time.Time { return now }, OnCritical: func() { woke++ }}
	s.Tick()
	if woke != 0 {
		t.Fatalf("woke %d while calm", woke)
	}
	write(t, filepath.Join(proc, "pressure", "memory"), "some avg10=30.00 avg60=0.00 avg300=0.00 total=100\nfull avg10=25.00 avg60=0.00 avg300=0.00 total=100\n")
	now = now.Add(5 * time.Second)
	s.Tick()
	if woke != 1 {
		t.Fatalf("woke %d on critical entry, want 1", woke)
	}
	now = now.Add(5 * time.Second)
	s.Tick()
	if woke != 1 {
		t.Fatalf("woke %d within TriggerFresh, want 1", woke)
	}
	now = now.Add(resource.TriggerFresh)
	s.Tick()
	if woke != 2 {
		t.Fatalf("woke %d after TriggerFresh, want 2", woke)
	}
	write(t, filepath.Join(proc, "pressure", "memory"), "some avg10=0.00 avg60=0.00 avg300=0.00 total=100\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=100\n")
	now = now.Add(5 * time.Second)
	s.Tick()
	write(t, filepath.Join(proc, "pressure", "memory"), "some avg10=30.00 avg60=0.00 avg300=0.00 total=200\nfull avg10=25.00 avg60=0.00 avg300=0.00 total=200\n")
	now = now.Add(5 * time.Second)
	s.Tick()
	if woke != 3 {
		t.Fatalf("woke %d on re-entry after recovery, want 3", woke)
	}
}
