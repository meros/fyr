package cgroup

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// psRow is one line of `ps -A -o` TreePS: rss in KB, age and CPU in seconds.
func psRow(pid, ppid, rss, age int, cpu float64, args string) string {
	return fmt.Sprintf("%5d %5d %8d %02d:%02d %d:%05.2f S %s\n", pid, ppid, rss, age/60, age%60, int(cpu)/60, cpu-float64(int(cpu)/60*60), args)
}

func fixedPS(out *string) func() (string, error) {
	return func() (string, error) { return *out, nil }
}

type memKV struct {
	mu sync.Mutex
	m  map[string]string
}

func (k *memKV) GetKV(key string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.m[key], nil
}

func (k *memKV) SetKV(key, v string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.m == nil {
		k.m = map[string]string{}
	}
	k.m[key] = v
	return nil
}

func TestV207PsTableSkipsZombiesAndKeepsTheCommand(t *testing.T) {
	now := boot.Add(time.Hour)
	out := " 10 1 2048 01-00:00:10 1:02.50 Ss claude --print\n 11 10 0 00:05 0:00.00 Z+ (sh)\n"
	tb := parseTreePS(out, now)
	r, ok := tb[10]
	if !ok || len(tb) != 1 {
		t.Fatalf("%+v", tb)
	}
	if r.rss != 2048*1024 || r.cpu != 62.5 || r.args != "claude --print" || !r.at.Equal(now.Add(-86410*time.Second)) {
		t.Fatalf("%+v", r)
	}
}

func TestV207ACPUCountGoesOnWhenAChildEnds(t *testing.T) {
	out := psRow(10, 1, 1000, 600, 1, "claude") + psRow(20, 10, 4000, 60, 50, "cc a.c")
	r := &TreeReader{PS: fixedPS(&out)}
	clis := map[string]int{"t_a": 10}
	now := boot
	if rd := r.Read(now, clis); len(rd) != 1 || rd[0].CPU != nil || rd[0].Used != 0 || rd[0].Work != 5000*1024 {
		t.Fatalf("first read %+v", rd)
	}
	// The child gave 5 s more and ended; a new one used 5 s since the last read.
	out = psRow(10, 1, 1000, 610, 1, "claude") + psRow(21, 10, 2000, 5, 5, "cc b.c")
	now = now.Add(5 * time.Second)
	rd := r.Read(now, clis)
	if len(rd) != 1 || rd[0].CPU == nil || *rd[0].CPU != 1 || rd[0].Used != 5*time.Second {
		t.Fatalf("after a child ended %+v", rd)
	}
	// The CLI is away and back: the count goes on.
	out = psRow(30, 1, 1000, 1, 0, "other")
	now = now.Add(5 * time.Second)
	if rd := r.Read(now, clis); len(rd) != 0 {
		t.Fatalf("no CLI %+v", rd)
	}
	out = psRow(10, 1, 1000, 620, 2, "claude")
	now = now.Add(5 * time.Second)
	if rd := r.Read(now, clis); len(rd) != 1 || rd[0].Used < 6*time.Second {
		t.Fatalf("back %+v", rd)
	}
}

// The numbers of a Mac: a held job is measured from the tree under the CLI,
// reads as running, and its class learns what it used.
func TestV207AHeldJobIsMeasuredFromTheProcessTree(t *testing.T) {
	now := time.Now() // the pool stamps a grant with the clock
	out := psRow(10, 1, 100000, 600, 1, "claude")
	p := slots.New(2)
	j := &Jobs{}
	s := &Sampler{Proc: t.TempDir(), Cores: 8, Slots: p, Jobs: j, Trees: &TreeReader{PS: fixedPS(&out)},
		CLIs: func() map[string]int { return map[string]int{"t_a": 10} }, Now: func() time.Time { return now }}
	s.Tick()
	p.Acquire("t_a", slots.Test)
	j.Saw("t_a", "make test")
	now = now.Add(5 * time.Second)
	s.Tick()
	for i := 1; i <= 12; i++ { // a minute at two cores, 1 GB over the CLI
		now = now.Add(5 * time.Second)
		out = psRow(10, 1, 100000, 600, 1, "claude") + psRow(20, 10, 1<<20, 5*i, float64(10*i), "make test")
		s.Tick()
	}
	l := s.Get()
	if len(l.Threads) != 1 || l.Threads[0].ID != "t_a" || l.Threads[0].CPU == nil || *l.Threads[0].CPU != 2 || l.Agents == nil || l.Agents.Memory == 0 || l.Cgroups {
		t.Fatalf("the load shows the thread: %+v %+v", l.Threads, l.Agents)
	}
	if at, ok := j.Active("t_a"); !ok || !at.Equal(now.UTC()) {
		t.Fatalf("a running job reads as idle: %v", at)
	}
	if c, grew, ok := j.LiveUse("t_a"); !ok || c < 1.5 || grew != 1<<30 {
		t.Fatalf("live use %v %d %v", c, grew, ok)
	}
	if u, ok := s.TreeCPUTime("t_a"); !ok || u != 120*time.Second {
		t.Fatalf("cpu time %v %v", u, ok)
	}
	p.Release("t_a")
	now = now.Add(5 * time.Second)
	s.Tick()
	rs := j.Records()
	if len(rs) != 1 || rs[0].Cores < 1.5 || rs[0].Peak != 1<<30 {
		t.Fatalf("%+v", rs)
	}
	if l, ok := j.Learned("make test"); !ok || l.Demand < 1.5 || l.Peak != 1<<30 {
		t.Fatalf("learned %+v %v", l, ok)
	}
}

func TestV207AHoldThatMeasuredNothingTeachesNothing(t *testing.T) {
	kv := &memKV{}
	kv.SetKV(costsKey, `{"fyr: make test":{"demand":0,"peak":0,"walls":[60],"runs":9},"fyr: yarn build":{"demand":2,"peak":5,"walls":[60],"runs":3}}`)
	j := &Jobs{KV: kv}
	if _, ok := j.Learned("fyr: make test"); ok {
		t.Fatal("a stored class without measures still counts")
	}
	if l, ok := j.Learned("fyr: yarn build"); !ok || l.Runs != 3 {
		t.Fatalf("%+v %v", l, ok)
	}
	p := slots.New(1)
	p.Acquire("t_a", slots.Test)
	t0 := time.Now()
	j.tick(nil, nil, p.Snapshot(), nil, nil, t0)
	j.tick(nil, nil, p.Snapshot(), nil, nil, t0.Add(time.Minute))
	p.Release("t_a")
	j.tick(nil, nil, p.Snapshot(), nil, nil, t0.Add(2*time.Minute))
	if _, ok := j.Learned("test"); ok {
		t.Fatal("a hold without readings taught its class")
	}
	if len(j.Records()) != 1 {
		t.Fatalf("%+v", j.Records())
	}
}

func TestV207BUndeclaredWorkIsNamedFromTheProcessTree(t *testing.T) {
	now := boot.Add(time.Hour)
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal: 16000000 kB\nMemAvailable: 12000000 kB\n")
	cli := psRow(10, 1, 100000, 3600, 1, "claude") + psRow(11, 10, 90000, 3500, 1, "node mcp.js")
	out := cli
	g := &JobGroups{}
	g.Bash("t_a", "cd web && yarn pre-push:check")
	s := &Sampler{Proc: proc, Cores: 8, Groups: g, Detect: &Detector{}, Trees: &TreeReader{PS: fixedPS(&out)},
		CLIs: func() map[string]int { return map[string]int{"t_a": 10} }, Now: func() time.Time { return now },
		Project: func(string) string { return "Fyr" }}
	for i := 0; i < 4; i++ {
		s.Tick()
		now = now.Add(5 * time.Second)
	}
	wrapper := "/bin/zsh -c source /Users/u/.claude/shell-snapshots/snapshot-zsh-1.sh && eval 'cd web && yarn pre-push:check'"
	for i := 1; i <= 20; i++ {
		out = cli + psRow(20, 10, 4000, 5*i, 0, wrapper) + psRow(21, 20, 3<<20, 5*i, float64(20*i), "node check.js")
		s.Tick()
		now = now.Add(5 * time.Second)
	}
	fl := s.Detect.Flags()
	if len(fl) != 1 || fl[0].Command != "cd web && yarn pre-push:check" {
		t.Fatalf("%+v", fl)
	}
	if u := s.Undeclared(); len(u) != 1 || u[0].Name != "Fyr" {
		t.Fatalf("%+v", u)
	}
	if r := s.Roots("t_a", fl[0].FirstOver); len(r) != 1 || r[0].PID != 20 || r[0].RSS != (4000+3<<20)*1024 {
		t.Fatalf("roots %+v", r)
	}
}

type sentSignal struct {
	pid int
	sig syscall.Signal
}

func stopGroups(out *string, kv *memKV, sent *[]sentSignal) *JobGroups {
	return &JobGroups{Trees: &TreeReader{PS: fixedPS(out)}, KV: kv,
		CLIs: func() map[string]int { return map[string]int{"t_a": 10} },
		Kill: func(p int, s syscall.Signal) error { *sent = append(*sent, sentSignal{p, s}); return nil }}
}

func TestV207CAPauseStopsTheJobNeverTheCLIAndResumesIt(t *testing.T) {
	out := psRow(10, 1, 1, 600, 1, "claude --print make test") + psRow(11, 10, 1, 500, 1, "node mcp.js") +
		psRow(20, 10, 1, 9, 1, `/bin/zsh -c eval 'cd web && make  test'`) + psRow(21, 20, 1, 8, 1, "go tool compile") +
		psRow(30, 1, 1, 9, 1, "make test")
	kv := &memKV{}
	var sent []sentSignal
	g := stopGroups(&out, kv, &sent)
	if g.Freeze("t_a") {
		t.Fatal("a pause without a known command")
	}
	g.Seen("t_a", "make test")
	if !g.Freeze("t_a") || !g.Frozen("t_a") {
		t.Fatal("not paused")
	}
	if fmt.Sprint(sent) != fmt.Sprint([]sentSignal{{20, syscall.SIGSTOP}, {21, syscall.SIGSTOP}}) {
		t.Fatalf("%v", sent)
	}
	if !strings.Contains(kv.m[stoppedKey], "20") {
		t.Fatalf("not stored: %q", kv.m[stoppedKey])
	}
	sent = nil
	g.Thaw("t_a")
	if g.Frozen("t_a") || len(sent) != 2 || sent[0].sig != syscall.SIGCONT || kv.m[stoppedKey] != "{}" {
		t.Fatalf("%v %q", sent, kv.m[stoppedKey])
	}
}

func TestV207CUndeclaredWorkStopsItsRootsAndEndsWhenTheyAreGone(t *testing.T) {
	out := psRow(10, 1, 1, 600, 1, "claude") + psRow(20, 10, 1, 9, 1, "sh job.sh") + psRow(21, 20, 1, 8, 1, "node x.js") + psRow(40, 1, 1, 9, 1, "elsewhere")
	var sent []sentSignal
	g := stopGroups(&out, &memKV{}, &sent)
	if g.FreezeUndeclared("t_a", []Root{{PID: 40}}) {
		t.Fatal("a root outside the CLI's tree was stopped")
	}
	if !g.FreezeUndeclared("t_a", []Root{{PID: 20}}) || len(sent) != 2 || !g.Frozen("t_a") {
		t.Fatalf("%v", sent)
	}
	if g.UndeclaredEmpty("t_a") {
		t.Fatal("its processes live")
	}
	out = psRow(10, 1, 1, 600, 1, "claude")
	if !g.UndeclaredEmpty("t_a") {
		t.Fatal("its processes are gone")
	}
	sent = nil
	g.EndUndeclared("t_a")
	if g.Frozen("t_a") || len(sent) != 2 || sent[0].sig != syscall.SIGCONT {
		t.Fatalf("%v", sent)
	}
}

func procState(t *testing.T, pid int) string {
	t.Helper()
	b, _ := exec.Command("ps", "-o", "state=", "-p", fmt.Sprint(pid)).Output()
	return strings.TrimSpace(string(b))
}

func waitState(t *testing.T, pid int, want func(string) bool, what string) {
	t.Helper()
	s := ""
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if s = procState(t, pid); want(s) {
			return
		}
	}
	t.Fatalf("pid %d is not %s: state %q", pid, what, s)
}

// Real processes: a stop holds them, and a new fyrd resumes what the last left.
func TestV207CAStopSurvivesARestartAndTheNextStartResumes(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Skip("no ps")
	}
	cli := exec.Command("sh", "-c", "sh -c 'sleep 30; :' & wait")
	if err := cli.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { syscall.Kill(cli.Process.Pid, syscall.SIGKILL); cli.Wait() }()
	reader := &TreeReader{PS: func() (string, error) {
		b, err := exec.Command("ps", "-A", "-o", TreePS).Output()
		return string(b), err
	}}
	pid := cli.Process.Pid
	var job []int
	for end := time.Now().Add(5 * time.Second); len(job) < 2 && time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		tb, ok := reader.Table(time.Now())
		if !ok {
			t.Fatal("no process table")
		}
		job = under(tb.kids(), pid)[1:]
	}
	if len(job) < 2 {
		t.Fatalf("the job did not start: %v", job)
	}
	defer func() {
		for _, p := range job {
			syscall.Kill(p, syscall.SIGKILL)
		}
	}()
	kv := &memKV{}
	clis := func() map[string]int { return map[string]int{"t_a": pid} }
	g := &JobGroups{Trees: reader, CLIs: clis, KV: kv}
	if !g.FreezeUndeclared("t_a", []Root{{PID: job[0]}}) {
		t.Fatal("not stopped")
	}
	stopped := func(s string) bool { return strings.HasPrefix(s, "T") }
	for _, p := range job {
		waitState(t, p, stopped, "stopped")
	}
	if s := procState(t, pid); stopped(s) {
		t.Fatalf("the CLI stopped: %q", s)
	}
	next := &JobGroups{Trees: reader, CLIs: clis, KV: kv}
	next.ResumeStopped()
	for _, p := range job {
		waitState(t, p, func(s string) bool { return s != "" && !stopped(s) }, "running")
	}
	if kv.m[stoppedKey] != "{}" {
		t.Fatalf("%q", kv.m[stoppedKey])
	}
}
