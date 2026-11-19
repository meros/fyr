// V0.agents-unit (decision 0036 (B), #221): with the module's agents slice
// each thread runs in a scope of its own outside fyrd's unit; without it, or
// without permission, the V0.80 layout stays. The cgroupfs is a plain
// directory and the probe is faked.
package cgroup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const slice = "fyr.slice/fyr-agents.slice"

// scopeFS is a host with the module's slice: fyrd's unit is not delegated
// (no controllers to split), and the probe lands in the slice.
func scopeFS(t *testing.T, delegated bool) (Options, *logs, *[]string) {
	t.Helper()
	o, l := fakeFS(t, "0::/"+unit+"\n")
	if !delegated {
		os.Chmod(filepath.Join(o.FS, unit), 0o555)
		t.Cleanup(func() { os.Chmod(filepath.Join(o.FS, unit), 0o755) })
	}
	write(t, filepath.Join(o.FS, slice, "memory.current"), "1000")
	o.AgentsSlice = "fyr-agents.slice"
	o.SystemdRun = "/run/sw/bin/systemd-run"
	o.oomAdj = filepath.Join(t.TempDir(), "oom_score_adj")
	write(t, o.oomAdj, "-500")
	o.ScopeProbe = func(prefix []string) (string, string, error) {
		unit := strings.TrimPrefix(prefix[slices.IndexFunc(prefix, func(a string) bool { return strings.HasPrefix(a, "--unit=") })], "--unit=")
		return "/" + slice + "/" + unit, "1", nil
	}
	var mu sync.Mutex
	calls := &[]string{}
	o.scopeExec = func(name string, args ...string) error {
		mu.Lock()
		defer mu.Unlock()
		*calls = append(*calls, name+" "+strings.Join(args, " "))
		return nil
	}
	return o, l, calls
}

func TestAgentsUnitScopeLayoutWhenTheSliceWorks(t *testing.T) {
	o, l, _ := scopeFS(t, false)
	tr := Setup(o)
	if tr == nil || tr.Layout() != LayoutScope {
		t.Fatalf("layout %v: %v", tr, l.lines)
	}
	if tr.AgentsDir() != filepath.Join(o.FS, slice) || tr.legacy != "" {
		t.Fatalf("agents %q legacy %q", tr.AgentsDir(), tr.legacy)
	}
	if !strings.Contains(strings.Join(l.lines, "\n"), "layout scope") {
		t.Fatalf("log %v", l.lines)
	}
	dir, wrap := tr.Launch("t_a")
	if dir != "" {
		t.Fatalf("a scope thread starts in no directory, got %q", dir)
	}
	want := []string{"/run/sw/bin/systemd-run", "--scope", "--quiet", "--no-ask-password", "--collect", "--unit=fyr-agent-t_a.scope",
		"--slice=fyr-agents.slice", "--property=Delegate=yes", "--property=OOMPolicy=kill"}
	if !slices.Equal(wrap[:len(want)], want) {
		t.Fatalf("wrap %q", wrap)
	}
	for _, p := range []string{"--property=PartOf=fyr.service", "--property=CPUWeight=100", "--"} {
		if !slices.Contains(wrap, p) {
			t.Errorf("wrap lacks %s: %q", p, wrap)
		}
	}
	// fyrd runs at a negative oom_score_adj; the agents go back to 0.
	if tail := strings.Join(wrap[len(wrap)-3:], " "); !strings.Contains(tail, "oom_score_adj") {
		t.Errorf("no oom_score_adj reset: %q", wrap)
	}
	if tr.Thread("t_b") != "" || tr.Deploy() != "" {
		t.Fatal("no directory in the scope layout")
	}
	if w := tr.DeployWrap(); w == nil || !strings.HasPrefix(w()[5], "--unit="+deployPrefix) {
		t.Fatalf("deploy wrap nil=%v", w == nil)
	}
}

func TestAgentsUnitFallsBackToTheUnitLayout(t *testing.T) {
	cases := map[string]func(o *Options){
		"no slice configured": func(o *Options) { o.AgentsSlice = "" },
		"polkit says no": func(o *Options) {
			o.ScopeProbe = func([]string) (string, string, error) {
				return "", "", errors.New("Interactive authentication required.")
			}
		},
		"probe ran elsewhere": func(o *Options) {
			o.ScopeProbe = func([]string) (string, string, error) { return "/system.slice/fyr.service", "0", nil }
		},
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			o, l, _ := scopeFS(t, true)
			mut(&o)
			tr := Setup(o)
			if tr == nil || tr.Layout() != LayoutUnit {
				t.Fatalf("layout %v: %v", tr, l.lines)
			}
			if d, wrap := tr.Launch("t_a"); d != filepath.Join(o.FS, unit, "agents", "t_a", CLIDir) || wrap != nil {
				t.Fatalf("launch %q %q", d, wrap)
			}
			if o.AgentsSlice != "" && !strings.Contains(strings.Join(l.lines, "\n"), "keeping the unit layout") {
				t.Fatalf("log %v", l.lines)
			}
		})
	}
	// Not delegated and no scopes: off, as without cgroups.
	o, l, _ := scopeFS(t, false)
	o.ScopeProbe = func([]string) (string, string, error) { return "", "", errors.New("no") }
	if tr := Setup(o); tr != nil {
		t.Fatalf("on: %v", l.lines)
	}
}

func TestAgentsUnitScopeNames(t *testing.T) {
	for base, want := range map[string]string{
		"fyr-agent-t_a.scope":     "t_a 0",
		"fyr-agent-t_a.2.scope":   "t_a 2",
		"fyr-deploy-12-3.scope":   DeployDir + " 0",
		"fyr-agent-.scope":        "",
		"fyr-agent-t_a.x.scope":   "",
		"other.scope":             "",
		"fyr-agent-t_a.service":   "",
		"fyr-agent-probe-1.scope": "probe-1 0",
	} {
		n, g, ok := scopeThread(base)
		got := ""
		if ok {
			got = n + " " + strconv.Itoa(g)
		}
		if got != want {
			t.Errorf("%s: %q, want %q", base, got, want)
		}
	}
	if scopeUnit("t_a", 0) != "fyr-agent-t_a.scope" || scopeUnit("t_a", 1) != "fyr-agent-t_a.1.scope" {
		t.Fatal(scopeUnit("t_a", 1))
	}
	for path, want := range map[string]string{
		"/system.slice/fyr.service":                                       "fyr.service",
		"/system.slice/fyr.service/server":                                "fyr.service",
		"/user.slice/user-1000.slice/user@1000.service/app.slice/x.scope": "",
	} {
		if got := unitOf(path); got != want {
			t.Errorf("unitOf(%s) = %q", path, got)
		}
	}
}

// A second process of a thread while the first still runs (a handover) gets
// a scope of its own; the thread's cgroup is the newest.
func TestAgentsUnitSecondProcessGetsTheNextScope(t *testing.T) {
	o, _, _ := scopeFS(t, false)
	tr := Setup(o)
	unitOfWrap := func(w []string) string {
		return strings.TrimPrefix(w[slices.IndexFunc(w, func(a string) bool { return strings.HasPrefix(a, "--unit=") })], "--unit=")
	}
	_, w1 := tr.Launch("t_a")
	_, w2 := tr.Launch("t_a") // before the first scope exists
	if unitOfWrap(w1) != "fyr-agent-t_a.scope" || unitOfWrap(w2) != "fyr-agent-t_a.1.scope" {
		t.Fatalf("%s %s", unitOfWrap(w1), unitOfWrap(w2))
	}
	first := filepath.Join(o.FS, slice, "fyr-agent-t_a.scope")
	second := filepath.Join(o.FS, slice, "fyr-agent-t_a.1.scope")
	write(t, filepath.Join(first, "cgroup.procs"), "10\n")
	write(t, filepath.Join(second, "cgroup.procs"), "20\n")
	if d := tr.threadDir("t_a"); d != second {
		t.Fatalf("thread dir %s", d)
	}
	tr.Release("t_a")
	tr.Release("t_a")
	// After a restart (no memory of the newest): the highest generation.
	tr2 := Setup(o)
	if d := tr2.threadDir("t_a"); d != second {
		t.Fatalf("thread dir after restart %s", d)
	}
	g := &JobGroups{Tree: tr2}
	if g.threadDir("t_a") != second || g.dir("t_a") != filepath.Join(second, jobDir) {
		t.Fatalf("job groups %s", g.dir("t_a"))
	}
}

// The switch: the unit was split by the fyrd before, its threads run on in
// agents/<thread> (the runner keeps them, decision 0023), new ones get scopes.
func TestAgentsUnitSwitchKeepsRunningThreads(t *testing.T) {
	o, l, _ := scopeFS(t, true)
	o.ScopeProbe = func(prefix []string) (string, string, error) {
		return "/" + slice + "/" + scopePrefix + "probe-" + strconv.Itoa(o.PID) + scopeSuffix, "1", nil
	}
	old := Setup(Options{Budget: o.Budget, FS: o.FS, Self: o.Self, PID: o.PID, Probe: o.Probe, Logf: l.logf}) // the fyrd before
	// V0.job-limits: the CLI starts in its cli/ leaf.
	legacyDir := filepath.Dir(old.Thread("t_old"))
	write(t, filepath.Join(legacyDir, CLIDir, "cgroup.procs"), "100\n")
	o.Self = filepath.Join(t.TempDir(), "self")
	write(t, o.Self, "0::/"+unit+"/server\n") // the new fyrd starts where the old one was
	tr := Setup(o)
	if tr == nil || tr.Layout() != LayoutScope || tr.legacy != filepath.Join(o.FS, unit, "agents") {
		t.Fatalf("tree %+v: %v", tr, l.lines)
	}
	tr.Hold("t_old") // the runner's process from before
	if d := tr.threadDir("t_old"); d != legacyDir {
		t.Fatalf("old thread at %s, want %s", d, legacyDir)
	}
	g := &JobGroups{Tree: tr}
	if g.dir("t_old") != filepath.Join(legacyDir, jobDir) {
		t.Fatal(g.dir("t_old"))
	}
	if _, w := tr.Launch("t_new"); w == nil {
		t.Fatal("a new thread gets a scope")
	}
	write(t, filepath.Join(o.FS, slice, "fyr-agent-t_new.scope", "cgroup.procs"), "200\n")
	var names []string
	for _, c := range tr.threadCgroups() {
		names = append(names, c.Name+"@"+filepath.Base(filepath.Dir(c.Dir)))
	}
	if got := strings.Join(names, ","); got != "t_new@fyr-agents.slice,t_old@agents" {
		t.Fatalf("thread cgroups %s", got)
	}
	tr.Release("t_old")
}

// Writes on a scope's own files (cpu.weight, memory.high) go through the
// system manager: the files are root's.
func TestAgentsUnitScopeWeightsGoThroughSystemctl(t *testing.T) {
	o, _, calls := scopeFS(t, false)
	tr := Setup(o)
	tr.Launch("t_a")
	write(t, filepath.Join(o.FS, slice, "fyr-agent-t_a.scope", "cgroup.procs"), "10\n")
	tr.Focus("dev", "t_a", time.Now())
	deadline := time.Now().Add(2 * time.Second)
	for {
		if got := strings.Join(*calls, "\n"); strings.Contains(got, "set-property --runtime fyr-agent-t_a.scope CPUWeight="+focusWeight) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("calls %q", *calls)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := tr.setAttr(filepath.Join(o.FS, slice, "fyr-agent-t_a.scope"), "memory.high", "max"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[len(*calls)-1]; !strings.HasSuffix(got, "fyr-agent-t_a.scope MemoryHigh=infinity") || !strings.HasPrefix(got, "/run/sw/bin/systemctl ") {
		t.Fatalf("call %q", got)
	}
	// job/ inside the scope is fyrd's own: written directly.
	job := filepath.Join(o.FS, slice, "fyr-agent-t_a.scope", jobDir)
	os.MkdirAll(job, 0o755)
	n := len(*calls)
	if err := tr.setAttr(job, "memory.high", "1000"); err != nil || len(*calls) != n || read(t, filepath.Join(job, "memory.high")) != "1000" {
		t.Fatalf("job write: %v %d", err, len(*calls))
	}
}

func TestAgentsUnitLoadShowsTheLayoutAndReadsTheSlice(t *testing.T) {
	o, _, _ := scopeFS(t, false)
	tr := Setup(o)
	tr.Launch("t_a")
	sd := filepath.Join(o.FS, slice, "fyr-agent-t_a.scope")
	write(t, filepath.Join(sd, "memory.current"), "300")
	write(t, filepath.Join(o.FS, slice, "fyr-agent-t_a.1.scope", "memory.current"), "200")
	write(t, filepath.Join(o.FS, slice, "fyr-deploy-1-1.scope", "memory.current"), "50")
	write(t, filepath.Join(o.FS, slice, "memory.max"), "14G")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "loadavg"), "0.1 0.2 0.3 1/2 3\n")
	write(t, filepath.Join(proc, "stat"), "cpu  1 2 3 4\n")
	l := (&Sampler{Tree: tr, Proc: proc, Cores: 4}).get()
	if l.CgroupLayout != LayoutScope || l.Agents == nil || l.Agents.Memory != 1000 || l.Agents.MemoryMax != 14<<30 {
		t.Fatalf("load %+v agents %+v", l, l.Agents)
	}
	got := map[string]uint64{}
	for _, th := range l.Threads {
		got[th.ID] = th.Memory
	}
	if fmt.Sprint(got) != fmt.Sprint(map[string]uint64{"t_a": 500, DeployDir: 50}) {
		t.Fatalf("threads %v", got)
	}
}
