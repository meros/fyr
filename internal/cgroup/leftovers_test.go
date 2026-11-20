package cgroup

// V0.176: leftover processes. These tests start real processes (a fake CLI
// with a child, orphans whose parent exited, a `timeout` orphan) and list
// their pids in a scratch cgroupfs in a temp dir; the live tree is never read
// or written. /proc is the real one, so ppid, start time and argv are real.

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func needLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
}

// startCLI starts a fake CLI: a shell that keeps one child (sleep) and waits.
func startCLI(t *testing.T) (cli, child int) {
	t.Helper()
	cmd := exec.Command("sh", "-c", "sleep 300 & echo $!; wait")
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	line, _ := bufio.NewReader(out).ReadString('\n')
	child, _ = strconv.Atoi(strings.TrimSpace(line))
	t.Cleanup(func() { kill(child) })
	return cmd.Process.Pid, child
}

// orphan starts script's background job from a shell that exits at once, so
// the job is reparented away from the test: a real leftover. It returns the
// job's pid (the first line the script prints).
func orphan(t *testing.T, script string) int {
	t.Helper()
	out, err := exec.Command("sh", "-c", script+" >/dev/null 2>&1 & echo $!").Output()
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	t.Cleanup(func() { killTree(pid) })
	prog := strings.Fields(script)[0]
	waitFor(t, func() bool { a := readArgv("/proc", pid); return len(a) > 0 && a[0] == prog })
	return pid
}

func kill(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}

func killTree(pid int) {
	for _, c := range childrenOf(pid) {
		killTree(c)
	}
	kill(pid)
}

func childrenOf(pid int) []int {
	b, _ := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "task", strconv.Itoa(pid), "children"))
	var out []int
	for _, f := range strings.Fields(string(b)) {
		n, _ := strconv.Atoi(f)
		out = append(out, n)
	}
	return out
}

func alive(pid int) bool {
	st, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	s := string(st)
	i := strings.LastIndexByte(s, ')')
	return i > 0 && !strings.HasPrefix(strings.TrimSpace(s[i+1:]), "Z")
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

// scratch is a split tree in a temp dir with thread t_a's cgroup.
func scratch(t *testing.T) (*Tree, string) {
	t.Helper()
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	if tr == nil {
		t.Fatal("no tree")
	}
	return tr, filepath.Join(o.FS, unit, "agents")
}

func putProcs(t *testing.T, dir string, ps ...int) {
	t.Helper()
	var b strings.Builder
	for _, p := range ps {
		b.WriteString(strconv.Itoa(p) + "\n")
	}
	write(t, filepath.Join(dir, "cgroup.procs"), b.String())
}

func byPID(ls []Leftover) map[int]Leftover {
	m := map[int]Leftover{}
	for _, l := range ls {
		m[l.PID] = l
	}
	return m
}

func TestV176ALeftoverIsWhatIsNotTheCLIsTree(t *testing.T) {
	needLinux(t)
	tr, agents := scratch(t)
	cli, child := startCLI(t)
	lone := orphan(t, "sleep 301")
	timed := orphan(t, "timeout 2h sleep 302")
	waitFor(t, func() bool { return len(childrenOf(timed)) == 1 })
	inner := childrenOf(timed)[0]
	putProcs(t, filepath.Join(agents, "t_a"), cli, child, lone, timed)
	putProcs(t, filepath.Join(agents, "t_a", "job"), inner) // a subgroup counts

	got := byPID(ScanLeftovers(tr, "", map[string]int{"t_a": cli}))
	if _, ok := got[cli]; ok {
		t.Fatal("the CLI is a leftover")
	}
	if _, ok := got[child]; ok {
		t.Fatal("the CLI's child is a leftover")
	}
	if l, ok := got[lone]; !ok || l.Thread != "t_a" || l.Name != "sleep" || l.Procs != 1 {
		t.Fatalf("lone orphan: %+v (all %+v)", l, got)
	}
	l, ok := got[timed]
	if !ok || l.Procs != 2 {
		t.Fatalf("timeout orphan with its child: %+v", l)
	}
	if _, ok := got[inner]; ok {
		t.Fatal("a leftover's child is listed on its own")
	}
	if l.Until == nil || l.Until.Sub(l.Started) != 2*time.Hour {
		t.Fatalf("the timeout's end: %+v", l.Until)
	}
	if len(got) != 2 {
		t.Fatalf("leftovers: %+v", got)
	}
}

func TestV176AThreadWithoutALiveCLILeavesEverythingAndAnUnknownPidLeavesNothing(t *testing.T) {
	needLinux(t)
	tr, agents := scratch(t)
	cli, child := startCLI(t)
	putProcs(t, filepath.Join(agents, "t_a"), cli, child)
	putProcs(t, filepath.Join(agents, "t_b"), cli+0) // same pids under another name: no CLI there
	if got := ScanLeftovers(tr, "", map[string]int{}); len(got) != 2 {
		t.Fatalf("no live CLI: every process is left over (roots): %+v", got)
	}
	// A CLI that is live or starting but whose pid is unknown: hands off.
	if got := ScanLeftovers(tr, "", map[string]int{"t_a": 0, "t_b": 0}); len(got) != 0 {
		t.Fatalf("unknown CLI pid: %+v", got)
	}
	// self-deploy is never looked at.
	putProcs(t, filepath.Join(agents, DeployDir), child)
	for _, l := range ScanLeftovers(tr, "", map[string]int{"t_a": 0, "t_b": 0}) {
		t.Fatalf("self-deploy's process listed: %+v", l)
	}
}

// fakeThreads answers the tracker's questions about threads.
type fakeThreads map[string]ThreadState

func (f fakeThreads) state(name string) ThreadState { return f[name] }

func newTracker(t *testing.T, tr *Tree, clis map[string]int, th fakeThreads, now *time.Time) (*Leftovers, *[]string) {
	var lines []string
	l := &Leftovers{Tree: tr, Now: func() time.Time { return *now }, CLIs: func() map[string]int { return clis },
		Thread: th.state, KV: kv{}, TermWait: 2 * time.Second,
		OnEnd: func(thread string, ended []Leftover, why string) {
			var names []string
			for _, e := range ended {
				names = append(names, e.Name)
			}
			lines = append(lines, thread+": "+why+": "+strings.Join(names, ","))
		}}
	return l, &lines
}

func TestV176BSettledThreadsLeftoversEndAtOnceAndLeaveALine(t *testing.T) {
	needLinux(t)
	tr, agents := scratch(t)
	cli, child := startCLI(t)
	lone := orphan(t, "sleep 303")
	putProcs(t, filepath.Join(agents, "t_a"), cli, child, lone)
	now := time.Now()
	l, lines := newTracker(t, tr, map[string]int{"t_a": cli}, fakeThreads{"t_a": {Known: true, Asleep: true}}, &now)
	l.Tick()
	waitFor(t, func() bool { return !alive(lone) })
	if !alive(cli) || !alive(child) {
		t.Fatal("the CLI or its child was ended")
	}
	if len(*lines) != 1 || !strings.Contains((*lines)[0], "t_a: ") || !strings.Contains((*lines)[0], "sleep") {
		t.Fatalf("line: %v", *lines)
	}
}

func TestV176BLiveThreadKeepsLeftoversUntilIdleLimitUnlessKeptOrTimed(t *testing.T) {
	needLinux(t)
	tr, agents := scratch(t)
	cli, child := startCLI(t)
	lone := orphan(t, "sleep 304")
	kept := orphan(t, "sleep 305")
	timed := orphan(t, "timeout 3h sleep 306")
	putProcs(t, filepath.Join(agents, "t_a"), cli, child, lone, kept, timed)
	now := time.Now()
	th := fakeThreads{"t_a": {Known: true, Running: true}}
	l, lines := newTracker(t, tr, map[string]int{"t_a": cli}, th, &now)
	var smu sync.Mutex
	signalled := map[int]syscall.Signal{}
	l.Signal = func(p int, sig syscall.Signal) error {
		smu.Lock()
		signalled[p] = sig
		smu.Unlock()
		return syscall.Kill(p, sig)
	}
	l.Tick()
	if n := len(l.View()); n != 0 {
		t.Fatalf("shown inside the grace: %d", n)
	}
	now = now.Add(LeftoverGrace)
	l.Tick()
	if n := len(l.View()); n != 3 {
		t.Fatalf("shown after the grace: %+v", l.View())
	}
	if err := l.Declare("t_a", kept, "", 4*time.Hour, "dev server the owner asked for", "agent"); err != nil {
		t.Fatal(err)
	}
	// A running thread: nothing ends, however old.
	now = now.Add(2 * LeftoverIdle)
	l.Tick()
	if !alive(lone) || len(*lines) != 0 {
		t.Fatal("ended during a run")
	}
	// The run ended: the idle limit counts from then.
	th["t_a"] = ThreadState{Known: true}
	l.Tick()
	now = now.Add(LeftoverIdle - time.Minute)
	l.Tick()
	if !alive(lone) {
		t.Fatal("ended before the idle limit")
	}
	now = now.Add(2 * time.Minute)
	l.Tick()
	waitFor(t, func() bool { return !alive(lone) })
	// What the tracker signalled, not whether they still run: the processes are
	// real and live in this host's cgroups, where something outside the test
	// (fyrd's guard, another test binary) may end one; that once failed this
	// test 1 in 600 beside other packages (V0.fix-flakes).
	for _, p := range []int{kept, timed, cli, child} {
		smu.Lock()
		sig, ok := signalled[p]
		smu.Unlock()
		if ok {
			t.Fatalf("the tracker signalled a kept, timed or CLI process: pid %d (%v; kept %d, timed %d, CLI %d, its child %d); lines %v",
				p, sig, kept, timed, cli, child, *lines)
		}
	}
	if len(*lines) != 1 {
		t.Fatalf("lines: %v", *lines)
	}
	// The agent's declaration ends at settle; the timeout does not hold then either.
	th["t_a"] = ThreadState{Known: true, Asleep: true}
	l.Tick()
	waitFor(t, func() bool { return !alive(kept) && !alive(timed) })
}

func TestV176BOwnerKeepHoldsThroughSettleAndOwnerEndEndsAtOnce(t *testing.T) {
	needLinux(t)
	tr, agents := scratch(t)
	a := orphan(t, "sleep 307")
	b := orphan(t, "sleep 308")
	putProcs(t, filepath.Join(agents, "t_a"), a, b)
	now := time.Now()
	th := fakeThreads{"t_a": {Known: true, Running: true}}
	l, lines := newTracker(t, tr, map[string]int{}, th, &now)
	l.Tick()
	now = now.Add(LeftoverGrace)
	l.Tick()
	v := byPID(l.View())
	if err := l.Keep(v[a].ID, 24*time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := l.End(v[b].ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !alive(b) })
	if len(*lines) != 1 || !strings.Contains((*lines)[0], "owner") {
		t.Fatalf("owner's end leaves a line: %v", *lines)
	}
	th["t_a"] = ThreadState{Known: true, Asleep: true}
	l.Tick()
	if !alive(a) {
		t.Fatal("the owner's Keep did not hold through settle")
	}
	if got := byPID(l.View())[a]; got.Kept == nil || got.Kept.By != "owner" {
		t.Fatalf("view: %+v", got)
	}
	if err := l.End("999999@1"); err == nil {
		t.Fatal("end of an unknown leftover succeeds")
	}
}

func TestV176BOrphanCgroupEndsAfterTheGraceAndPidReuseIsRefused(t *testing.T) {
	needLinux(t)
	tr, agents := scratch(t)
	a := orphan(t, "sleep 309")
	putProcs(t, filepath.Join(agents, "t_gone"), a)
	now := time.Now()
	l, lines := newTracker(t, tr, map[string]int{}, fakeThreads{}, &now)
	l.Tick()
	if !alive(a) {
		t.Fatal("ended inside the grace")
	}
	if v := l.Pending(); len(v) != 1 || !v[0].Orphan {
		t.Fatalf("pending orphan: %+v", v)
	}
	now = now.Add(LeftoverGrace)
	l.Tick()
	waitFor(t, func() bool { return !alive(a) })
	if len(*lines) != 1 || !strings.HasPrefix((*lines)[0], "t_gone: ") {
		t.Fatalf("lines: %v", *lines)
	}
	// An id whose start time does not match the pid's is never signalled.
	self := os.Getpid()
	if endPIDs("", []int{self}, map[int]uint64{self: 1}, time.Second, nil) != 0 {
		t.Fatal("signalled a pid whose start time changed")
	}
}

func TestV176EProcessTreeModeFollowsTheCLIsDescendantsAfterItEnds(t *testing.T) {
	needLinux(t)
	cli, child := startCLI(t)
	now := time.Now()
	th := fakeThreads{"t_a": {Known: true, Asleep: true}}
	clis := map[string]int{"t_a": cli}
	l, lines := newTracker(t, nil, clis, th, &now)
	l.Tick() // the CLI's tree is seen: child is its
	if len(l.View()) != 0 {
		t.Fatal("a live CLI's child is a leftover")
	}
	// The CLI ends; its child lives on (reparented): a leftover of t_a.
	p, _ := os.FindProcess(cli)
	p.Signal(os.Kill)
	waitFor(t, func() bool { st, ok := readProcStat("/proc", child); return ok && st.ppid != cli })
	delete(clis, "t_a")
	l.Tick()
	waitFor(t, func() bool { return !alive(child) })
	if len(*lines) != 1 || !strings.HasPrefix((*lines)[0], "t_a: ") {
		t.Fatalf("lines: %v", *lines)
	}
}

func TestV176TimeoutDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"timeout 12h kubectl port-forward x": 12 * time.Hour,
		"timeout -s INT 90 sleep 1":          90 * time.Second,
		"timeout --signal=KILL 1.5m x":       90 * time.Second,
		"timeout -k 5 2d x":                  48 * time.Hour,
		"sleep 12h":                          0,
		"timeout":                            0,
	} {
		if got := timeoutOf(strings.Fields(in)); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
}

func TestV176NameIsShort(t *testing.T) {
	for in, want := range map[string]string{
		"/nix/store/abc-chromium/bin/chromium --headless --user-data-dir=/tmp/x": "chromium",
		"python3 -m http.server 8000":                                            "http.server",
		"timeout 12h kubectl port-forward svc/x 8080":                            "kubectl port-forward",
		"/tmp/go-build123/b001/engine.test -test.run=TestX":                      "engine.test",
		"node /home/x/node_modules/.bin/vite --port 5173":                        "vite",
		"setsid nohup timeout 4h cloud-sql-proxy inst --port 1":                  "cloud-sql-proxy",
	} {
		if got := shortName(strings.Fields(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestV176EPsTableKeepsAProcessIdentityAcrossReads(t *testing.T) {
	out := "  101     1  2048 01:02:03 /usr/bin/python3 -m http.server 8000\n  102   101   100 1-00:00:05 timeout 4h cloud-sql-proxy x\n"
	now := time.Now()
	a, b := psTable(out, now), psTable(out, now.Add(1500*time.Millisecond))
	if a[101].start != b[101].start || a[101].rss != 2048*1024 || a[102].ppid != 101 {
		t.Fatalf("%+v %+v", a, b)
	}
	if d := now.Sub(a[102].at); d < 24*time.Hour || d > 24*time.Hour+10*time.Second {
		t.Fatalf("etime with days: %v", d)
	}
	if got := shortName(a[101].argv); got != "http.server" {
		t.Fatal(got)
	}
}
