package cgroup

// V0.176: leftover processes. A thread's cgroup (agents/<thread>/ and its
// subgroups) holds its CLI and everything the CLI starts. A process there that
// is not the CLI or a descendant of it, or any process in the cgroup of a
// thread with no live CLI, is a leftover: a Chrome a layout run left, a
// mockup server, a port-forward of a thread that settled. Fyr shows them and
// ends them by policy (SPEC V0.176):
//
//   - a settled or archived thread's: at once;
//   - a cgroup no thread of this host has: after LeftoverGrace;
//   - a live thread's, once its run has ended: after LeftoverIdle, unless the
//     agent declared it (keep_process) or it runs under `timeout` (it keeps
//     its timeout);
//   - the owner's End ends one now, the owner's Keep holds one until it expires,
//     through a settle too.
//
// Nothing outside agents/ is read, self-deploy/ is never looked at, the CLI's
// tree is never a leftover, and a pid whose start time changed is never
// signalled. On a host without the cgroup tree (macOS) the tracker remembers
// each live CLI's descendants from the process table, and a remembered
// process that outlives its place under the CLI is that thread's leftover.

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	// LeftoverGrace: a process is shown as a leftover (and an orphan's is
	// ended) only when it has been one this long.
	LeftoverGrace = 2 * time.Minute
	// LeftoverIdle: a live thread's leftover ends this long after its run ended.
	LeftoverIdle = 30 * time.Minute
	// MaxKeep bounds a keep: the agent's and the owner's.
	MaxKeep = 7 * 24 * time.Hour
	// LeftoverTick is how often the host looks; a run's end and a settle look at once.
	LeftoverTick = time.Minute
)

// Leftover is one leftover process and its descendants (a root).
type Leftover struct {
	// ID is "<pid>@<start ticks>": it names this process and no later one with the pid.
	ID     string `json:"id"`
	Thread string `json:"thread"` // the cgroup's name under agents/: a thread id
	// Orphan: no thread of this host has this cgroup.
	Orphan  bool      `json:"orphan,omitempty"`
	PID     int       `json:"pid"`
	Name    string    `json:"name"`    // short: "chromium", "http.server"
	Command string    `json:"command"` // the root's command line, cut
	Procs   int       `json:"procs"`   // the root and its descendants
	RSS     uint64    `json:"rss"`     // their resident memory, bytes
	Started time.Time `json:"started"`
	Seen    time.Time `json:"seen"` // first seen as a leftover
	// Until is the end its own `timeout` sets.
	Until *time.Time `json:"until,omitempty"`
	Kept  *Keep      `json:"kept,omitempty"`
	// EndsAt is when the policy ends it, as it stands now; nil while it holds.
	EndsAt *time.Time `json:"endsAt,omitempty"`

	pids   []int
	starts map[int]uint64
}

// Keep holds a leftover: the agent's declaration (keep_process) or the
// owner's Keep. PID names a process (its root or one inside), Match a text in
// the root's command line.
type Keep struct {
	Thread string    `json:"thread"`
	PID    int       `json:"pid,omitempty"`
	Match  string    `json:"match,omitempty"`
	Until  time.Time `json:"until"`
	Why    string    `json:"why,omitempty"`
	By     string    `json:"by"` // "agent" or "owner"
}

func (k Keep) holds(l Leftover) bool {
	if k.Thread != l.Thread {
		return false
	}
	if k.PID != 0 && slices.Contains(l.pids, k.PID) {
		return true
	}
	return k.Match != "" && strings.Contains(l.Command, k.Match)
}

// ThreadState is what the tracker needs to know of a thread.
type ThreadState struct {
	Known   bool // a thread of this host has this id
	Asleep  bool // settled or archived
	Running bool // a run is live
}

// pinfo is one process of the table.
type pinfo struct {
	pid, ppid int
	start     uint64 // ticks after boot (a process table from ps: unix seconds)
	at        time.Time
	rss       uint64
	argv      []string
}

// readArgv is the command line; a process with none (a kernel thread, one
// caught in exec) has its comm.
func readArgv(proc string, pid int) []string {
	b, _ := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "cmdline"))
	if a := strings.FieldsFunc(string(b), func(r rune) bool { return r == 0 }); len(a) > 0 {
		return a
	}
	c, _ := os.ReadFile(filepath.Join(proc, strconv.Itoa(pid), "comm"))
	if s := strings.TrimSpace(string(c)); s != "" {
		return []string{s}
	}
	return nil
}

func readP(proc string, pid int, boot time.Time) (pinfo, bool) {
	st, ok := readProcStat(proc, pid)
	if !ok {
		return pinfo{}, false
	}
	return pinfo{pid: pid, ppid: st.ppid, start: st.started, at: boot.Add(time.Duration(st.started) * time.Second / clkTck),
		rss: readRSS(proc, pid), argv: readArgv(proc, pid)}, true
}

// ScanLeftovers reads every thread's cgroup under agents/ and returns its
// leftovers, roots only. clis gives each thread's live CLI pid; 0 is a CLI
// that is live or starting with no pid known, and that thread is skipped.
func ScanLeftovers(tr *Tree, proc string, clis map[string]int) []Leftover {
	if tr == nil {
		return nil
	}
	if proc == "" {
		proc = "/proc"
	}
	boot, ok := bootTime(proc)
	if !ok {
		return nil
	}
	es, _ := os.ReadDir(filepath.Join(tr.root, "agents"))
	var out []Leftover
	for _, e := range es {
		name := e.Name()
		if !e.IsDir() || name == DeployDir || name == "probe" {
			continue
		}
		cli, live := clis[name]
		if live && cli == 0 {
			continue
		}
		var in []int
		filepath.WalkDir(filepath.Join(tr.root, "agents", name), func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && d.Name() == "cgroup.procs" {
				in = append(in, pids(p)...)
			}
			return nil
		})
		table := map[int]pinfo{}
		for _, p := range in {
			if pi, ok := readP(proc, p, boot); ok {
				table[p] = pi
			}
		}
		out = append(out, rootsOf(name, table, cli)...)
	}
	return out
}

// rootsOf groups the processes of one thread's cgroup (table) that are not
// cli's tree into roots: a process whose parent is not in the set.
func rootsOf(thread string, table map[int]pinfo, cli int) []Leftover {
	children := map[int][]int{}
	for p, pi := range table {
		children[pi.ppid] = append(children[pi.ppid], p)
	}
	mine := map[int]bool{}
	if cli > 0 {
		var walk func(int)
		walk = func(p int) {
			mine[p] = true
			for _, c := range children[p] {
				walk(c)
			}
		}
		walk(cli)
	}
	var out []Leftover
	for p, pi := range table {
		if mine[p] {
			continue
		}
		if _, ok := table[pi.ppid]; ok && !mine[pi.ppid] {
			continue // its parent is a leftover: part of that root
		}
		l := Leftover{Thread: thread, PID: p, Started: pi.at, Command: cut(strings.Join(pi.argv, " ")), Name: shortName(pi.argv),
			ID: fmt.Sprintf("%d@%d", p, pi.start), starts: map[int]uint64{}}
		var walk func(int)
		var until time.Duration
		var untilFrom time.Time
		walk = func(q int) {
			x := table[q]
			l.pids = append(l.pids, q)
			l.starts[q] = x.start
			l.RSS += x.rss
			if until == 0 {
				if d := timeoutOf(x.argv); d > 0 {
					until, untilFrom = d, x.at
				}
			}
			kids := children[q]
			sort.Ints(kids)
			for _, c := range kids {
				walk(c)
			}
		}
		walk(p)
		l.Procs = len(l.pids)
		if until > 0 {
			u := untilFrom.Add(until)
			l.Until = &u
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

// timeoutOf is the duration of a `timeout [opts] DURATION cmd` command line
// (coreutils: s, m, h, d, a fraction allowed), or 0.
func timeoutOf(argv []string) time.Duration {
	if len(argv) < 2 || filepath.Base(argv[0]) != "timeout" {
		return 0
	}
	i := 1
	for i < len(argv) && strings.HasPrefix(argv[i], "-") {
		a := argv[i]
		i++
		if (a == "-s" || a == "-k" || a == "--signal" || a == "--kill-after") && i < len(argv) {
			i++
		}
	}
	if i >= len(argv)-1 {
		return 0
	}
	s, unit := argv[i], time.Second
	switch {
	case strings.HasSuffix(s, "s"):
		s = strings.TrimSuffix(s, "s")
	case strings.HasSuffix(s, "m"):
		s, unit = strings.TrimSuffix(s, "m"), time.Minute
	case strings.HasSuffix(s, "h"):
		s, unit = strings.TrimSuffix(s, "h"), time.Hour
	case strings.HasSuffix(s, "d"):
		s, unit = strings.TrimSuffix(s, "d"), 24*time.Hour
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return time.Duration(f * float64(unit))
}

// shortName names a command line for a person: the program, past the
// wrappers (timeout, setsid, nohup, env), a Python module (-m), a node
// script, and a kubectl verb.
func shortName(argv []string) string {
	for len(argv) > 0 {
		b := filepath.Base(argv[0])
		switch b {
		case "setsid", "nohup", "env", "nice", "stdbuf":
			argv = argv[1:]
			for len(argv) > 0 && (strings.HasPrefix(argv[0], "-") || strings.Contains(argv[0], "=")) {
				argv = argv[1:]
			}
			continue
		case "timeout":
			i := 1
			for i < len(argv) && strings.HasPrefix(argv[i], "-") {
				a := argv[i]
				i++
				if (a == "-s" || a == "-k" || a == "--signal" || a == "--kill-after") && i < len(argv) {
					i++
				}
			}
			if i+1 > len(argv) {
				return b
			}
			argv = argv[i+1:]
			continue
		}
		if strings.HasPrefix(b, "python") {
			for i, a := range argv {
				if a == "-m" && i+1 < len(argv) {
					return argv[i+1]
				}
			}
			if len(argv) > 1 {
				return filepath.Base(argv[1])
			}
		}
		if b == "node" || b == "bun" || b == "deno" {
			for _, a := range argv[1:] {
				if !strings.HasPrefix(a, "-") {
					return filepath.Base(a)
				}
			}
		}
		if b == "kubectl" && len(argv) > 1 {
			return b + " " + argv[1]
		}
		return b
	}
	return ""
}

// ---- the tracker ----

// Leftovers tracks the host's leftovers, applies the policy and keeps the
// keeps. Tick is called by one goroutine; the views never wait.
type Leftovers struct {
	Tree *Tree // nil: the process-tree mode
	Proc string
	// PS gives `ps -A -o pid=,ppid=,rss=,etime=,args=` on a host without /proc.
	PS     func() (string, error)
	Now    func() time.Time
	CLIs   func() map[string]int
	Thread func(name string) ThreadState
	KV     interface {
		GetKV(string) (string, error)
		SetKV(string, string) error
	}
	TermWait time.Duration // SIGTERM to SIGKILL, default 5 s
	// OnEnd hears what was ended and why, per thread, outside the lock.
	OnEnd func(thread string, ended []Leftover, why string)
	Logf  func(string, ...any)

	tick      sync.Mutex // one Tick at a time
	mu        sync.Mutex
	seen      map[string]time.Time // leftover id → first seen
	idleSince map[string]time.Time // thread → when its run was last seen ended
	keeps     []Keep
	loaded    bool
	// The process-tree mode: each remembered process (pid@start) → its thread.
	owned map[string]string
	pub   atomic.Pointer[[]Leftover]
	all   atomic.Pointer[[]Leftover]
}

const keepsKey = "leftover-keeps"

func (l *Leftovers) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

func (l *Leftovers) logf(f string, a ...any) {
	if l.Logf != nil {
		l.Logf(f, a...)
	}
}

func (l *Leftovers) loadLocked() {
	if l.loaded {
		return
	}
	l.loaded = true
	if l.KV == nil {
		return
	}
	if raw, _ := l.KV.GetKV(keepsKey); raw != "" {
		json.Unmarshal([]byte(raw), &l.keeps)
	}
}

func (l *Leftovers) saveLocked() {
	if l.KV == nil {
		return
	}
	b, _ := json.Marshal(l.keeps)
	l.KV.SetKV(keepsKey, string(b))
}

// View is the leftovers past the grace, the oldest first.
func (l *Leftovers) View() []Leftover {
	if p := l.pub.Load(); p != nil {
		return slices.Clone(*p)
	}
	return nil
}

// Pending is every leftover of the last Tick, in its grace too.
func (l *Leftovers) Pending() []Leftover {
	if p := l.all.Load(); p != nil {
		return slices.Clone(*p)
	}
	return nil
}

// Declare records the agent's (or the owner's) keep of a process of thread:
// by pid, or by a text of its command line, for d (at most MaxKeep).
func (l *Leftovers) Declare(thread string, pid int, match string, d time.Duration, why, by string) error {
	match = strings.TrimSpace(match)
	if pid <= 0 && len(match) < 3 {
		return errors.New("name the process: its pid, or at least 3 characters of its command line")
	}
	if d <= 0 {
		return errors.New("a keep needs a duration")
	}
	d = min(d, MaxKeep)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loadLocked()
	l.keeps = append(l.keeps, Keep{Thread: thread, PID: pid, Match: match, Until: l.now().Add(d), Why: why, By: by})
	l.saveLocked()
	return nil
}

func (l *Leftovers) find(id string) (Leftover, bool) {
	for _, x := range l.Pending() {
		if x.ID == id {
			return x, true
		}
	}
	return Leftover{}, false
}

// Keep is the owner's Keep of a shown leftover, for d.
func (l *Leftovers) Keep(id string, d time.Duration) error {
	x, ok := l.find(id)
	if !ok {
		return errors.New("no such leftover process")
	}
	if err := l.Declare(x.Thread, x.PID, "", d, "kept by the owner", "owner"); err != nil {
		return err
	}
	l.republish()
	return nil
}

// End is the owner's End: it ends the leftover now and leaves a line.
func (l *Leftovers) End(id string) error {
	x, ok := l.find(id)
	if !ok {
		return errors.New("no such leftover process")
	}
	l.end(map[string][]Leftover{x.Thread: {x}}, "ended by the owner")
	l.Tick()
	return nil
}

// Forget drops the keeps of a thread's processes that ended, and expired keeps.
func (l *Leftovers) pruneLocked(now time.Time) {
	n := 0
	for _, k := range l.keeps {
		if k.Until.After(now) {
			l.keeps[n] = k
			n++
		}
	}
	if n != len(l.keeps) {
		l.keeps = l.keeps[:n]
		l.saveLocked()
	}
}

func (l *Leftovers) keepOf(x Leftover, now time.Time, owner bool) *Keep {
	for _, k := range l.keeps {
		if k.Until.After(now) && (k.By == "owner") == owner && k.holds(x) {
			k := k
			return &k
		}
	}
	return nil
}

// Tick scans, decides, ends and publishes. A run's end and a settle call it too.
func (l *Leftovers) Tick() {
	l.tick.Lock()
	defer l.tick.Unlock()
	now := l.now()
	clis := map[string]int{}
	if l.CLIs != nil {
		clis = l.CLIs()
	}
	var found []Leftover
	if l.Tree != nil {
		found = ScanLeftovers(l.Tree, l.Proc, clis)
	} else {
		found = l.scanTrees(clis, now)
	}
	state := func(name string) ThreadState {
		if l.Thread == nil {
			return ThreadState{Known: true}
		}
		return l.Thread(name)
	}
	states := map[string]ThreadState{}
	for _, x := range found {
		if _, ok := states[x.Thread]; !ok {
			states[x.Thread] = state(x.Thread)
		}
	}

	l.mu.Lock()
	l.loadLocked()
	l.pruneLocked(now)
	if l.seen == nil {
		l.seen, l.idleSince = map[string]time.Time{}, map[string]time.Time{}
	}
	ids := map[string]bool{}
	for i := range found {
		x := &found[i]
		ids[x.ID] = true
		if _, ok := l.seen[x.ID]; !ok {
			l.seen[x.ID] = now
		}
		x.Seen = l.seen[x.ID]
	}
	for id := range l.seen {
		if !ids[id] {
			delete(l.seen, id)
		}
	}
	for th, st := range states {
		switch {
		case st.Running:
			delete(l.idleSince, th)
		case l.idleSince[th].IsZero():
			l.idleSince[th] = now
		}
	}
	end := map[string][]Leftover{}
	why := map[string]string{}
	for i := range found {
		x := &found[i]
		st := states[x.Thread]
		x.Orphan = !st.Known
		if k := l.keepOf(*x, now, true); k != nil {
			x.Kept = k
			continue
		}
		var at time.Time
		reason := ""
		switch {
		case x.Orphan:
			at, reason = x.Seen.Add(LeftoverGrace), "in the cgroup of no thread"
		case st.Asleep:
			at, reason = x.Seen, "the thread settled"
		default:
			if k := l.keepOf(*x, now, false); k != nil {
				x.Kept = k
				continue
			}
			if st.Running || (x.Until != nil && x.Until.After(now)) {
				continue
			}
			from := l.idleSince[x.Thread]
			if x.Seen.After(from) {
				from = x.Seen
			}
			at, reason = from.Add(LeftoverIdle), fmt.Sprintf("idle %d min after the run ended", int(LeftoverIdle/time.Minute))
		}
		at2 := at
		x.EndsAt = &at2
		if !now.Before(at) {
			end[x.Thread] = append(end[x.Thread], *x)
			why[x.Thread] = reason
		}
	}
	l.mu.Unlock()

	for th, xs := range end {
		l.end(map[string][]Leftover{th: xs}, why[th])
	}
	ended := map[string]bool{}
	for _, xs := range end {
		for _, x := range xs {
			ended[x.ID] = true
		}
	}
	var all, shown []Leftover
	for _, x := range found {
		if ended[x.ID] {
			continue
		}
		all = append(all, x)
		if !now.Before(x.Seen.Add(LeftoverGrace)) {
			shown = append(shown, x)
		}
	}
	sort.SliceStable(shown, func(i, j int) bool { return shown[i].Seen.Before(shown[j].Seen) })
	l.all.Store(&all)
	l.pub.Store(&shown)
}

// republish refreshes the Kept marks of the published views after a keep.
func (l *Leftovers) republish() {
	now := l.now()
	l.mu.Lock()
	fix := func(xs []Leftover) []Leftover {
		for i := range xs {
			if k := l.keepOf(xs[i], now, true); k != nil {
				xs[i].Kept, xs[i].EndsAt = k, nil
			} else if k := l.keepOf(xs[i], now, false); k != nil && xs[i].Kept == nil {
				xs[i].Kept = k
			}
		}
		return xs
	}
	all, shown := fix(l.Pending()), fix(l.View())
	l.mu.Unlock()
	l.all.Store(&all)
	l.pub.Store(&shown)
}

func (l *Leftovers) end(byThread map[string][]Leftover, why string) {
	wait := l.TermWait
	if wait == 0 {
		wait = 5 * time.Second
	}
	proc := l.Proc
	if l.Tree == nil && l.PS != nil {
		proc = "-" // no /proc: signal by pid, no start-time check
	}
	for th, xs := range byThread {
		var done []Leftover
		for _, x := range xs {
			if endPIDs(proc, x.pids, x.starts, wait) > 0 {
				done = append(done, x)
			}
		}
		if len(done) == 0 {
			continue
		}
		var names []string
		for _, x := range done {
			names = append(names, x.Name)
		}
		l.logf("leftovers: %s: ended %s (%s)", th, strings.Join(names, ", "), why)
		if l.OnEnd != nil {
			l.OnEnd(th, done, why)
		}
	}
}

// endPIDs sends SIGTERM to each pid whose start time is still the one seen,
// waits up to wait for them to go, then SIGKILLs the rest. It returns how many
// it signalled. proc "-" skips the start-time check (no /proc).
func endPIDs(proc string, ps []int, starts map[int]uint64, wait time.Duration) int {
	if proc == "" {
		proc = "/proc"
	}
	same := func(p int) bool {
		if p <= 1 || p == os.Getpid() {
			return false
		}
		if proc == "-" {
			return syscall.Kill(p, 0) == nil
		}
		st, ok := readProcStat(proc, p)
		return ok && st.started == starts[p]
	}
	var sent []int
	for _, p := range ps {
		if same(p) && syscall.Kill(p, syscall.SIGTERM) == nil {
			sent = append(sent, p)
		}
	}
	n := len(sent)
	deadline := time.Now().Add(wait)
	for len(sent) > 0 {
		left := sent[:0]
		for _, p := range sent {
			if same(p) && !zombie(proc, p) {
				left = append(left, p)
			}
		}
		sent = left
		if len(sent) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, p := range sent {
		if same(p) {
			syscall.Kill(p, syscall.SIGKILL) // a frozen cgroup's process too
		}
	}
	return n
}

func zombie(proc string, p int) bool {
	if proc == "-" {
		return false
	}
	b, err := os.ReadFile(filepath.Join(proc, strconv.Itoa(p), "stat"))
	if err != nil {
		return true
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	return i < 0 || strings.HasPrefix(strings.TrimSpace(s[i+1:]), "Z")
}

// ---- the process-tree mode ----

// table reads the whole process table: /proc, or ps.
func (l *Leftovers) table(now time.Time) map[int]pinfo {
	if l.PS != nil {
		out, err := l.PS()
		if err != nil {
			return nil
		}
		return psTable(out, now)
	}
	proc := l.Proc
	if proc == "" {
		proc = "/proc"
	}
	boot, ok := bootTime(proc)
	if !ok {
		return nil
	}
	es, _ := os.ReadDir(proc)
	t := map[int]pinfo{}
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if pi, ok := readP(proc, pid, boot); ok && !zombie(proc, pid) {
			t[pid] = pi
		}
	}
	return t
}

// psTable reads `ps -A -o pid=,ppid=,rss=,etime=,args=` (rss in KB, etime
// [[dd-]hh:]mm:ss). A start read from etime moves by a second between reads,
// so a process's identity there is its pid and a hash of its command line.
func psTable(out string, now time.Time) map[int]pinfo {
	t := map[int]pinfo{}
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 {
			continue
		}
		pid, e1 := strconv.Atoi(f[0])
		ppid, e2 := strconv.Atoi(f[1])
		rss, e3 := strconv.ParseUint(f[2], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		et := f[3]
		var days int
		if d, rest, ok := strings.Cut(et, "-"); ok {
			days, _ = strconv.Atoi(d)
			et = rest
		}
		sec := 0
		for _, part := range strings.Split(et, ":") {
			v, _ := strconv.Atoi(part)
			sec = sec*60 + v
		}
		at := now.Add(-time.Duration(days*86400+sec) * time.Second)
		h := fnv.New64a()
		h.Write([]byte(strings.Join(f[4:], " ")))
		t[pid] = pinfo{pid: pid, ppid: ppid, rss: rss * 1024, at: at, start: h.Sum64() >> 1, argv: f[4:]}
	}
	return t
}

// scanTrees is ScanLeftovers without a cgroup tree: each live CLI's
// descendants are remembered as its thread's; a remembered process outside
// its live CLI's tree is a leftover.
func (l *Leftovers) scanTrees(clis map[string]int, now time.Time) []Leftover {
	t := l.table(now)
	if t == nil {
		return nil
	}
	children := map[int][]int{}
	for p, pi := range t {
		children[pi.ppid] = append(children[pi.ppid], p)
	}
	key := func(pi pinfo) string { return fmt.Sprintf("%d@%d", pi.pid, pi.start) }
	l.mu.Lock()
	if l.owned == nil {
		l.owned = map[string]string{}
	}
	under := map[int]string{}
	for th, cli := range clis {
		if _, ok := t[cli]; !ok || cli == 0 {
			continue
		}
		var walk func(int)
		walk = func(p int) {
			under[p] = th
			if p != cli {
				l.owned[key(t[p])] = th
			}
			for _, c := range children[p] {
				walk(c)
			}
		}
		walk(cli)
	}
	byThread := map[string]map[int]pinfo{}
	for k, th := range l.owned {
		pidS, _, _ := strings.Cut(k, "@")
		pid, _ := strconv.Atoi(pidS)
		pi, ok := t[pid]
		if !ok || key(pi) != k {
			delete(l.owned, k) // it ended
			continue
		}
		if cli, live := clis[th]; live && cli == 0 {
			continue
		}
		if under[pid] == th {
			continue
		}
		if byThread[th] == nil {
			byThread[th] = map[int]pinfo{}
		}
		var walk func(int)
		walk = func(p int) {
			if _, done := byThread[th][p]; done || under[p] != "" {
				return
			}
			byThread[th][p] = t[p]
			for _, c := range children[p] {
				walk(c)
			}
		}
		walk(pid)
	}
	l.mu.Unlock()
	var out []Leftover
	for th, tab := range byThread {
		out = append(out, rootsOf(th, tab, 0)...)
	}
	return out
}
