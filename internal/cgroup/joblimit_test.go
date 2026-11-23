// V0.job-limits (decision 0036 (C)): a job sizes itself. The CLI starts in the
// thread's cli/ leaf, so the thread's cgroup can enable cpu and memory for its
// children; a job moved into job/ or undeclared/ gets memory.high from its
// class's learned peak and cpu.max from its learned demand, both lifted while
// the agents' swap is nearly full. The cgroupfs and /proc are plain directories.
package cgroup

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/guard/resource"
)

func TestJL1TheCLIStartsInALeafAndTheThreadEnablesControllers(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := filepath.Join(o.FS, unit, "agents", "t_a")
	// An older fyrd started the CLI in the thread's own cgroup.
	write(t, filepath.Join(td, "cgroup.procs"), "10\n")
	cli := tr.Thread("t_a")
	if cli != filepath.Join(td, CLIDir) {
		t.Fatalf("start dir %q", cli)
	}
	if got := read(t, filepath.Join(cli, "cgroup.procs")); got != "10" {
		t.Fatalf("the old CLI was not moved into cli/: %q", got)
	}
	if got := read(t, filepath.Join(td, "cgroup.subtree_control")); got != controllers {
		t.Fatalf("subtree_control %q", got)
	}
	tr.Release("t_a")
	if _, err := os.Stat(cli); !os.IsNotExist(err) {
		t.Fatal("cli/ outlived the thread's last Release")
	}
}

func TestJL2LimitsFromTheLearnedClass(t *testing.T) {
	const gb = 1 << 30
	page := uint64(os.Getpagesize())
	cases := []struct {
		name           string
		z              JobSize
		budget, used   uint64
		cores          float64
		lifted         bool
		high, cpu, why string
	}{
		{"learned", JobSize{Class: "fyr: make test-go", Peak: 2 * gb, Demand: 2.9, Known: true}, 12 * gb, 4 * gb, 8, false, "2684354560", "400000 100000", "learned"},
		{"at least a core", JobSize{Peak: gb, Demand: 0.2, Known: true}, 12 * gb, 0, 8, false, "1342177280", "100000 100000", "learned"},
		{"demand above the agents' cores", JobSize{Peak: gb, Demand: 7, Known: true}, 12 * gb, 0, 8, false, "1342177280", "max", "learned"},
		{"unknown: the free budget", JobSize{}, 12 * gb, 9 * gb, 8, false, "3221225472", "max", "unknown"},
		{"unknown: at most half the budget", JobSize{}, 12 * gb, gb, 8, false, "6442450944", "max", "unknown"},
		{"swap nearly full", JobSize{Peak: gb, Demand: 1, Known: true}, 12 * gb, 0, 8, true, "max", "max", "swap-full"},
		{"no budget", JobSize{}, 0, 0, 8, false, "max", "max", "no-budget"},
	}
	for _, c := range cases {
		high, cpu, why := jobLimits(c.z, c.budget, c.used, c.cores, c.lifted)
		if c.high != "max" { // whole pages, as the kernel keeps it
			n, _ := strconv.ParseUint(c.high, 10, 64)
			c.high = strconv.FormatUint(n/page*page, 10)
		}
		if high != c.high || cpu != c.cpu || why != c.why {
			t.Errorf("%s: high=%s cpu=%s why=%s, want %s %s %s", c.name, high, cpu, why, c.high, c.cpu, c.why)
		}
	}
}

func TestJL3AMovedJobGetsItsLimitsTheValveLiftsThemAndAHitRaisesItsClass(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	agents := filepath.Join(o.FS, unit, "agents")
	write(t, filepath.Join(agents, "memory.current"), "1073741824\n")
	proc := t.TempDir()
	fakeProc(t, proc, 10, 1, "claude")
	fakeProc(t, proc, 20, 10, "/bin/zsh", "-c", "make test-go")
	write(t, filepath.Join(td, CLIDir, "cgroup.procs"), "10\n20\n")
	var raised []string
	g := &JobGroups{Tree: tr, Proc: proc, Cores: 8, // agents/cpu.max gives 4 of them
		Size: func(thread string, undeclared bool) JobSize {
			return JobSize{Class: "fyr: make test-go", Peak: 1 << 30, Demand: 2, Known: true}
		},
		Raise: func(class string) { raised = append(raised, class) }}

	g.Sync("t_a", "make test-go")
	job := filepath.Join(td, jobDir)
	if got := read(t, filepath.Join(job, "cgroup.procs")); got != "20" {
		t.Fatalf("job procs %q", got)
	}
	if got := read(t, filepath.Join(job, "memory.high")); got != "1342177280" {
		t.Fatalf("memory.high %q", got)
	}
	if got := read(t, filepath.Join(job, "cpu.max")); got != "300000 100000" {
		t.Fatalf("cpu.max %q", got)
	}
	if !l.has("guard: job limit thread=t_a cgroup=job class=\"fyr: make test-go\" high=1280MB cpu=3cores why=learned learned_peak=1024MB") {
		t.Fatalf("log %v", l.lines)
	}

	// The job hits its limit: the class's peak is raised once.
	write(t, filepath.Join(job, "memory.events"), "low 0\nhigh 7\nmax 0\noom 0\n")
	g.resize()
	g.resize()
	if len(raised) != 1 || raised[0] != "fyr: make test-go" || !l.has("guard: job limit hit thread=t_a") {
		t.Fatalf("raised %v log %v", raised, l.lines)
	}

	// The agents' swap is nearly full: the limits lift, and come back.
	write(t, filepath.Join(agents, "memory.swap.current"), "16G")
	g.resize()
	if read(t, filepath.Join(job, "memory.high")) != "max" || read(t, filepath.Join(job, "cpu.max")) != "max" {
		t.Fatal("not lifted")
	}
	write(t, filepath.Join(agents, "memory.swap.current"), "1G")
	g.resize()
	if read(t, filepath.Join(job, "memory.high")) != "1342177280" {
		t.Fatal("not back")
	}
	// The job ends: its cgroup and what was written go.
	write(t, filepath.Join(job, "cgroup.procs"), "")
	os.Remove(filepath.Join(job, "memory.events")) // the kernel's rmdir needs none of this
	g.End("t_a")
	if _, err := os.Stat(job); !os.IsNotExist(err) || len(g.sized) != 0 {
		t.Fatalf("left %v", g.sized)
	}
}

// TestJL4 (undeclared work of an unknown class gets the free budget) went with
// V0.freeze-charge: see TestFreezeChargeUndeclaredWorkOfAnUnknownClassGetsNoLimit.

func TestJL5RaisePeakAndSizeOf(t *testing.T) {
	j := &Jobs{}
	j.mu.Lock()
	j.load()
	j.table["fyr: make test-go"] = resource.Learned{Demand: 2.9, Peak: 1000, Runs: 3}
	j.mu.Unlock()
	j.RaiseOverrun("fyr: make test-go")
	j.RaiseOverrun("unknown")
	if l, _ := j.Learned("fyr: make test-go"); l.Peak != 1200 {
		t.Fatalf("peak %d", l.Peak)
	}
	j.mu.Lock()
	j.projects = map[string]string{"t_a": "fyr"}
	j.mu.Unlock()
	j.Saw("t_a", "make test-go")
	if z := j.SizeOf("t_a", false); !z.Known || z.Class != "fyr: make test-go" || z.Peak != 1200 || z.Demand != 2.9 {
		t.Fatalf("size %+v", z)
	}
	if z := j.SizeOf("t_b", false); z.Known || z.Class != "" {
		t.Fatalf("size of a thread without a class %+v", z)
	}
}

func (l *logs) has(prefix string) bool {
	for _, x := range l.lines {
		if strings.HasPrefix(x, prefix) {
			return true
		}
	}
	return false
}

func TestJL6TheCLIEnvironmentIsSharesOfTheHost(t *testing.T) {
	env := JobEnv(Capacity{CPU: 6, Memory: 12 << 30})
	if strings.Join(env, " ") != "GOMEMLIMIT=6144MiB PYTEST_XDIST_AUTO_NUM_WORKERS=3" {
		t.Fatalf("%v", env)
	}
	if Workers(Capacity{CPU: 1}) != 1 || len(JobEnv(Capacity{})) != 0 {
		t.Fatal("a small or unknown host")
	}
}

// In the scope layout the CLI starts in its scope's own cgroup (systemd-run --scope); the next tick moves it into
// the scope's cli/ leaf and enables cpu and memory for the scope's children, so its job gets limits there too.
func TestJL7AScopesCLIMovesToItsLeaf(t *testing.T) {
	o, _, _ := scopeFS(t, false)
	tr := Setup(o)
	if tr == nil || tr.Layout() != LayoutScope {
		t.Fatal("not the scope layout")
	}
	tr.Launch("t_a")
	sc := filepath.Join(o.FS, slice, scopeUnit("t_a", 0))
	write(t, filepath.Join(sc, "cgroup.procs"), "10\n11\n") // the scope as systemd made it
	tr.SplitThreads()
	if got := read(t, filepath.Join(sc, CLIDir, "cgroup.procs")); got != "11" { // the fake file keeps the last write
		t.Fatalf("cli procs %q", got)
	}
	if got := read(t, filepath.Join(sc, "cgroup.subtree_control")); got != controllers {
		t.Fatalf("subtree_control %q", got)
	}
	proc := t.TempDir()
	fakeProc(t, proc, 11, 1, "claude")
	fakeProc(t, proc, 20, 11, "make", "test-go")
	write(t, filepath.Join(sc, "cgroup.procs"), "")
	write(t, filepath.Join(sc, CLIDir, "cgroup.procs"), "11\n20\n")
	g := &JobGroups{Tree: tr, Proc: proc, Size: func(string, bool) JobSize { return JobSize{Peak: 1 << 30, Demand: 1, Known: true} }}
	g.Sync("t_a", "make test-go")
	if got := read(t, filepath.Join(sc, jobDir, "memory.high")); got != "1342177280" {
		t.Fatalf("job memory.high %q", got)
	}
}

// V0.joblimit-slice: in the scope layout the agents are fyr-agents.slice, not fyr.service/agents/: the swap valve
// reads the slice's swap, so the limits lift when it is nearly full.
func TestJL8TheValveReadsTheAgentsSliceInTheScopeLayout(t *testing.T) {
	o, l, _ := scopeFS(t, false)
	tr := Setup(o)
	tr.Launch("t_a")
	sc := filepath.Join(o.FS, slice, scopeUnit("t_a", 0))
	write(t, filepath.Join(sc, "cgroup.procs"), "11\n")
	tr.SplitThreads()
	proc := t.TempDir()
	fakeProc(t, proc, 11, 1, "claude")
	fakeProc(t, proc, 20, 11, "make", "test-go")
	write(t, filepath.Join(sc, "cgroup.procs"), "")
	write(t, filepath.Join(sc, CLIDir, "cgroup.procs"), "11\n20\n")
	agents := filepath.Join(o.FS, slice)
	write(t, filepath.Join(agents, "memory.swap.max"), "16G")
	write(t, filepath.Join(agents, "memory.swap.current"), "1G")
	g := &JobGroups{Tree: tr, Proc: proc, Size: func(string, bool) JobSize { return JobSize{Peak: 1 << 30, Demand: 1, Known: true} }}
	g.Sync("t_a", "make test-go")
	job := filepath.Join(sc, jobDir)
	if got := read(t, filepath.Join(job, "memory.high")); got != "1342177280" {
		t.Fatalf("job memory.high %q", got)
	}
	write(t, filepath.Join(agents, "memory.swap.current"), "15G") // ≥ 90 % of 16G
	g.resize()
	if read(t, filepath.Join(job, "memory.high")) != "max" || !l.has("guard: job limit thread=t_a cgroup=job class=\"\" high=max cpu=max why=swap-full") {
		t.Fatalf("the valve did not open in the scope layout: memory.high %q log %v", read(t, filepath.Join(job, "memory.high")), l.lines)
	}
	write(t, filepath.Join(agents, "memory.swap.current"), "1G")
	g.resize()
	if got := read(t, filepath.Join(job, "memory.high")); got != "1342177280" {
		t.Fatalf("not back: %q", got)
	}
}

// V0.joblimit-slice: an unknown class's memory.high is sized once, when the job is moved. Recomputed each tick from
// the agents' use, which counts the job itself, it shrank as the job grew and throttled it the harder the more it held.
func TestJL9AnUnknownJobKeepsItsFirstLimitAsItGrows(t *testing.T) {
	o, l := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	agents := filepath.Join(o.FS, unit, "agents")
	write(t, filepath.Join(agents, "memory.current"), "1G") // budget 13G: min(12G free, 6.5G half)
	proc := t.TempDir()
	fakeProc(t, proc, 10, 1, "claude")
	fakeProc(t, proc, 20, 10, "make", "build")
	write(t, filepath.Join(td, CLIDir, "cgroup.procs"), "10\n20\n")
	g := &JobGroups{Tree: tr, Proc: proc, Size: func(string, bool) JobSize { return JobSize{Class: "fyr: make build"} }}
	g.Sync("t_a", "make build")
	job := filepath.Join(td, jobDir)
	first := read(t, filepath.Join(job, "memory.high"))
	if first != strconv.FormatUint((13<<30)/2/uint64(os.Getpagesize())*uint64(os.Getpagesize()), 10) {
		t.Fatalf("first memory.high %q", first)
	}
	for _, used := range []string{"6G", "10G", "12G"} { // the job grows
		write(t, filepath.Join(agents, "memory.current"), used)
		g.resize()
		if got := read(t, filepath.Join(job, "memory.high")); got != first {
			t.Fatalf("at %s the limit moved to %q from %q", used, got, first)
		}
	}
	n := 0
	for _, x := range l.lines {
		if strings.HasPrefix(x, "guard: job limit thread=t_a") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d limit lines, want 1: %v", n, l.lines)
	}
	// The valve still lifts it, and it comes back to its first value.
	write(t, filepath.Join(agents, "memory.swap.current"), "16G")
	g.resize()
	if read(t, filepath.Join(job, "memory.high")) != "max" {
		t.Fatal("not lifted")
	}
	write(t, filepath.Join(agents, "memory.swap.current"), "1G")
	g.resize()
	if got := read(t, filepath.Join(job, "memory.high")); got != first {
		t.Fatalf("back to %q, want %q", got, first)
	}
}
