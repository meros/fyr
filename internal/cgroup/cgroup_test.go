// SPEC 0.5 V0.80: the unit splits into server/ and agents/, each thread runs in agents/<id>/, and Fyr runs as
// before without a delegated cgroup. The cgroupfs here is a plain directory.
package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const unit = "system.slice/fyr.service"

type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(f string, a ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(f, a...))
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// fakeFS is a cgroupfs with the unit delegated: the controllers are there,
// the unit holds the launcher (111) and fyrd (the test's pid).
func fakeFS(t *testing.T, self string) (Options, *logs) {
	t.Helper()
	dir := t.TempDir()
	fs := filepath.Join(dir, "cgroup")
	write(t, filepath.Join(fs, unit, "cgroup.controllers"), "cpuset cpu io memory pids\n")
	write(t, filepath.Join(fs, unit, "cgroup.procs"), fmt.Sprintf("111\n%d\n", os.Getpid()))
	write(t, filepath.Join(fs, unit, "cgroup.subtree_control"), "")
	selfFile := filepath.Join(dir, "self-cgroup")
	write(t, selfFile, self)
	l := &logs{}
	return Options{
		Budget: Budget{Enable: true, ServerMemoryLow: "1G", AgentsCPUMax: "400000 100000",
			AgentsMemoryHigh: "13G", AgentsMemoryMax: "14G", AgentsMemorySwapMax: "16G"},
		FS: fs, Self: selfFile, PID: os.Getpid(), Logf: l.logf,
		Probe: func(string) error { return nil },
	}, l
}

func TestV080SplitsTheUnit(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	if tr == nil {
		t.Fatalf("Setup is off: %v", l.lines)
	}
	root := filepath.Join(o.FS, unit)
	if tr.Root() != root {
		t.Fatalf("root %q", tr.Root())
	}
	want := map[string]string{
		"server/cgroup.procs":           strconv.Itoa(os.Getpid()), // fyrd moved itself last
		"cgroup.subtree_control":        "+cpu +memory",
		"server/cpu.weight":             "1000",
		"server/memory.low":             "1G",
		"agents/cpu.weight":             "100",
		"agents/cpu.max":                "400000 100000",
		"agents/memory.high":            "13G",
		"agents/memory.max":             "14G",
		"agents/memory.swap.max":        "16G",
		"agents/cgroup.subtree_control": "+cpu +memory",
	}
	for f, v := range want {
		if got := read(t, filepath.Join(root, f)); got != v {
			t.Errorf("%s = %q, want %q", f, got, v)
		}
	}
	if len(l.lines) != 1 || !strings.Contains(l.lines[0], "cgroups: on") {
		t.Fatalf("log %v", l.lines)
	}
}

func TestV080SetupMovesEveryProcessOfTheUnit(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	var moved []string
	o.move = func(file, pid string) error {
		moved = append(moved, filepath.Base(filepath.Dir(file))+":"+pid)
		return nil
	}
	if Setup(o) == nil {
		t.Fatal("off")
	}
	if got := strings.Join(moved, ","); got != "server:111,server:"+strconv.Itoa(os.Getpid()) {
		t.Fatalf("moved %s", got)
	}
}

func TestV080RestartUnderServerFindsTheUnit(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"/server\n")
	tr := Setup(o)
	if tr == nil || tr.Root() != filepath.Join(o.FS, unit) {
		t.Fatalf("root %v", tr)
	}
}

func TestV080OffRunsAsBeforeAndSaysWhyOnce(t *testing.T) {
	cases := map[string]func(o *Options){
		"not configured":        func(o *Options) { o.Enable = false },
		"no cgroup v2":          func(o *Options) { write(t, o.Self, "1:name=systemd:/x\n") },
		"not in a service unit": func(o *Options) { write(t, o.Self, "0::/user.slice/user-1000.slice/session-3.scope\n") },
		"controllers missing":   func(o *Options) { write(t, filepath.Join(o.FS, unit, "cgroup.controllers"), "cpu pids\n") },
		"probe refused":         func(o *Options) { o.Probe = func(string) error { return fmt.Errorf("bad file descriptor") } },
		"unit is not writable":  func(o *Options) { os.Chmod(filepath.Join(o.FS, unit), 0o555) },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			o, l := fakeFS(t, "0::/"+unit+"\n")
			mut(&o)
			defer os.Chmod(filepath.Join(o.FS, unit), 0o755)
			tr := Setup(o)
			if tr != nil {
				t.Fatalf("Setup is on")
			}
			if len(l.lines) != 1 || !strings.Contains(l.lines[0], "cgroups: off") {
				t.Fatalf("log %v", l.lines)
			}
			// A nil tree is the off state: no directory, nothing to release.
			if d := tr.Thread("t_1"); d != "" {
				t.Fatalf("thread dir %q", d)
			}
			tr.Release("t_1")
			if d := tr.Deploy(); d != "" {
				t.Fatalf("deploy dir %q", d)
			}
		})
	}
}

func TestV080ThreadCgroupLivesWhileItsProcessesDo(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	d := tr.Thread("t_abc")
	if d != filepath.Join(o.FS, unit, "agents", "t_abc") {
		t.Fatalf("dir %q", d)
	}
	if w := read(t, filepath.Join(d, "cpu.weight")); w != "100" {
		t.Fatalf("cpu.weight %q", w)
	}
	// A handover starts the new process before the old one's reader ends.
	if tr.Thread("t_abc") != d {
		t.Fatal("second start, other dir")
	}
	// The fake cgroupfs has files in the dir; the real one removes them with it.
	os.RemoveAll(d)
	os.Mkdir(d, 0o755)
	tr.Release("t_abc")
	if _, err := os.Stat(d); err != nil {
		t.Fatal("removed while a process still runs in it")
	}
	tr.Release("t_abc")
	if _, err := os.Stat(d); !os.IsNotExist(err) {
		t.Fatal("not removed after the last process")
	}
	if got := tr.Thread("../x"); got != filepath.Join(o.FS, unit, "agents", "___x") {
		t.Fatalf("unsafe name gave %q", got)
	}
	if got := tr.Deploy(); got != filepath.Join(o.FS, unit, "agents", "deploy") {
		t.Fatalf("deploy %q", got)
	}
}

func TestV080LoadSample(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	proc := t.TempDir()
	write(t, filepath.Join(proc, "loadavg"), "10.50 6.00 4.25 3/900 1234\n")
	write(t, filepath.Join(proc, "pressure", "cpu"), "some avg10=42.50 avg60=30.00 avg300=12.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n")
	agents := filepath.Join(o.FS, unit, "agents")
	td := tr.Thread("t_busy")
	stat := func(p string, usec int) {
		write(t, filepath.Join(p, "cpu.stat"), fmt.Sprintf("usage_usec %d\nuser_usec 1\nsystem_usec 1\n", usec))
	}
	stat(agents, 10_000_000)
	stat(td, 4_000_000)
	write(t, filepath.Join(agents, "memory.current"), "3000000000\n")
	write(t, filepath.Join(td, "memory.current"), "1000000000\n")
	now := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	s := &Sampler{Tree: tr, Proc: proc, Cores: 8, Now: func() time.Time { return now }}

	l := s.Get()
	if l.Cores != 8 || l.Load != [3]float64{10.5, 6, 4.25} || l.CPUPressure == nil || *l.CPUPressure != 42.5 || !l.Cgroups {
		t.Fatalf("machine %+v", l)
	}
	if l.Agents == nil || l.Agents.CPU != nil || l.Agents.CPUMax != 4 || l.Agents.Memory != 3_000_000_000 || l.Agents.MemoryMax != 14<<30 {
		t.Fatalf("agents on the first read %+v", l.Agents)
	}
	if len(l.Threads) != 1 || l.Threads[0].ID != "t_busy" || l.Threads[0].CPU != nil || l.Threads[0].Memory != 1_000_000_000 {
		t.Fatalf("threads on the first read %+v", l.Threads)
	}

	// A second ask within 2 s reads nothing new.
	stat(td, 9_000_000)
	now = now.Add(time.Second)
	if l2 := s.Get(); l2.Threads[0].CPU != nil {
		t.Fatal("read again within 2 s")
	}
	now = now.Add(time.Second) // 2 s after the first read: 5 s of CPU → 2.5 cores
	stat(agents, 16_000_000)
	l = s.Get()
	if c := l.Threads[0].CPU; c == nil || *c != 2.5 {
		t.Fatalf("thread cpu %v", c)
	}
	if c := l.Agents.CPU; c == nil || *c != 3 {
		t.Fatalf("agents cpu %v", c)
	}
}

func TestV080LoadWithoutCgroups(t *testing.T) {
	proc := t.TempDir()
	write(t, filepath.Join(proc, "loadavg"), "1.00 2.00 3.00 1/10 1\n")
	s := &Sampler{Proc: proc, Cores: 4}
	l := s.Get()
	if l.Cgroups || l.Agents != nil || l.Threads == nil || len(l.Threads) != 0 || l.CPUPressure != nil || l.Load[2] != 3 {
		t.Fatalf("%+v", l)
	}
}
