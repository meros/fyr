// Package cgroup gives every process Fyr starts a cgroup of its own under
// one budget for agent work (SPEC 0.5 V0.80), as T3's launcher did for its
// server (t3code.nix in the machine's NixOS config):
//
//	fyr.service/          Delegate=cpu memory; holds no process once split
//	  server/             the launcher and fyrd: cpu.weight 1000, memory.low
//	  agents/             the budget: cpu.max, memory.high/max/swap.max
//	    <thread id>/      a thread: no process of its own (V0.job-limits)
//	      cli/            its agent CLI and everything it starts
//	      job/, undeclared/  its heavy work, moved out of cli/ by fyrd
//	    deploy/           a self-deploy's build and tests
//
// A child starts directly in its cgroup (clone3 CLONE_INTO_CGROUP, see
// proc.IntoCgroup), so nothing sweeps PIDs. Without a writable delegated
// cgroup v2 Setup returns nil, a nil *Tree hands out no directories, and
// every process runs in the daemon's cgroup as before.
//
// With the module's agents slice the threads run in scopes of their own
// outside fyrd's unit instead (V0.agents-unit, layout.go).
package cgroup

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Budget is the module's cgroup settings (config.json "cgroups").
type Budget struct {
	Enable bool `json:"enable"`
	// ServerMemoryLow: memory.low of server/; the unit needs a MemoryLow
	// at least this large for it to count.
	ServerMemoryLow string `json:"serverMemoryLow"`
	// AgentsCPUMax: cpu.max of agents/, "400000 100000" is 4 cores.
	AgentsCPUMax        string `json:"agentsCpuMax"`
	AgentsMemoryHigh    string `json:"agentsMemoryHigh"`
	AgentsMemoryMax     string `json:"agentsMemoryMax"`
	AgentsMemorySwapMax string `json:"agentsMemorySwapMax"`
	// ThreadMemoryHighPercent: each thread's memory.high as a share of the
	// agents' budget, 0 for none (decision 0032 slice 3).
	ThreadMemoryHighPercent int `json:"threadMemoryHighPercent"`
	// AgentsSlice: the slice the module made for the agents, with their
	// budget and oomd rule (V0.agents-unit); each thread runs in a scope
	// under it. Empty keeps the V0.80 layout.
	AgentsSlice string `json:"agentsSlice,omitempty"`
	// SystemdRun and Systemctl: the binaries; default systemd-run on PATH
	// and the systemctl beside it.
	SystemdRun string `json:"systemdRun,omitempty"`
	Systemctl  string `json:"systemctl,omitempty"`
}

// Options are the Budget and where to find things; tests point FS and Self
// at a plain directory.
type Options struct {
	Budget
	FS   string // the cgroup2 mount, default /sys/fs/cgroup
	Self string // default /proc/self/cgroup
	PID  int    // default os.Getpid()
	Logf func(string, ...any)
	// Probe starts a process in dir to prove the kernel accepts
	// CLONE_INTO_CGROUP there; default runs `true`.
	Probe func(dir string) error
	// ScopeProbe starts one process in a scope of the agents' slice; default
	// runs systemd-run (V0.agents-unit).
	ScopeProbe ScopeProbe
	// OomdLimit reads oomd's memory pressure limit on a unit, in percent (0:
	// none), V0.oomd-guard; nil reads none. fyrd passes UnitOomdLimit, which
	// asks systemctl; tests leave it nil.
	OomdLimit func(unit string) float64
	move      func(file, pid string) error // tests watch the moves
	userBus   bool                         // tests: scopes of the user manager
	scopeExec func(name string, args ...string) error
	oomAdj    string // default /proc/self/oom_score_adj
}

const (
	serverWeight = "1000"
	agentsWeight = "100"
	threadWeight = "100"
	// focusWeight is the cpu.weight of a thread a device shows (V0.85).
	focusWeight = "300"
	// The weights of a thread of a high and a low project
	// (V0.project-priority); a focused thread has focusWeight either way.
	highWeight  = "200"
	lowWeight   = "50"
	controllers = "+cpu +memory"
)

// Tree is the unit's split cgroup. A nil Tree is the off state.
type Tree struct {
	root string
	// layout: where the threads' cgroups are; agents: the cgroup above
	// them (agents/, or the agents' slice); legacy: agents/ of a split unit
	// in the scope layout, whose threads from before the switch run on.
	layout Layout
	agents string
	legacy string
	sc     *scopes
	// curMu guards cur: each thread's newest scope (V0.agents-unit).
	curMu     sync.Mutex
	cur       map[string]string
	pending   map[string]time.Time // under mu: scope names handed out
	deploySeq atomic.Int64
	logf      func(string, ...any)
	mu        sync.Mutex
	refs      map[string]int
	// focus: per device, the thread (cgroup name) it shows and the last
	// beat (V0.85); raised: the cgroups written at focusWeight.
	focus  map[string]focusBeat
	raised map[string]bool
	// rank is a thread's project priority (V0.project-priority), set by
	// SetRank; weights: what each cgroup was last written.
	rank    func(thread string) int
	weights map[string]string
	// Decision 0032 slice 3: the thread share, and whether a nearly full swap
	// has lifted it (budgetLifted under mu).
	threadPercent int
	budgetLifted  bool
	budgetSeen    map[string]budgetSeen // under mu: what the guard's log remembers
	budgetSummary time.Time             // under mu: the last summary line
	budgetNow     func() time.Time      // tests; nil is time.Now
	// V0.oomd-guard: oomd's pressure limit in percent (0: none) on the unit
	// it watches for the agents (fyr.service in the split layout, the agents'
	// slice in the scope layout), that unit's cgroup, and when the guard last
	// logged that its stall came near the limit.
	oomdLimit float64
	oomdDir   string
	oomdNoted time.Time
	// V0.job-limits: the thread cgroups whose split was refused and logged, under mu.
	splitFailed map[string]bool
}

type focusBeat struct {
	name string
	at   time.Time
}

// Setup splits the unit's cgroup and sets the budget, or says once in the
// log why it does not and returns nil.
func Setup(o Options) *Tree {
	if o.FS == "" {
		o.FS = "/sys/fs/cgroup"
	}
	if o.Self == "" {
		o.Self = "/proc/self/cgroup"
	}
	if o.PID == 0 {
		o.PID = os.Getpid()
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	if o.Probe == nil {
		o.Probe = probe
	}
	if o.move == nil {
		o.move = func(file, pid string) error { return os.WriteFile(file, []byte(pid), 0o644) }
	}
	if o.ScopeProbe == nil {
		o.ScopeProbe = probeScope
	}
	if o.oomAdj == "" {
		o.oomAdj = "/proc/self/oom_score_adj"
	}
	self, selfErr := selfPath(o)
	t, err := setup(o, self, selfErr)
	if o.AgentsSlice != "" && selfErr == nil {
		st, serr := setupScopes(o, t, self)
		if serr == nil {
			o.Logf("cgroups: on, layout scope: fyrd in %s, each thread in %s/%s<thread>.scope (memory.oom.group=1), deploys in %s<n>.scope", self, st.agentsDir(), scopePrefix, deployPrefix)
			if st.legacy != "" {
				o.Logf("cgroups: threads started before the switch run on in %s/<thread>", st.legacy)
			}
			// V0.oomd-guard: fyrd's unit is root-owned with
			// ManagedOOMPreference=omit here; oomd's rule is on the slice.
			st.watchOomd(o, o.AgentsSlice, st.agentsDir())
			return st
		}
		o.Logf("cgroups: the agents' slice %s is not usable (%v); keeping the unit layout", o.AgentsSlice, serr)
	}
	if err != nil {
		o.Logf("cgroups: off (%v); every process runs in the daemon's cgroup", err)
		return nil
	}
	o.Logf("cgroups: on, layout unit, in %s (server/, agents/<thread>/, agents/self-deploy/)", t.root)
	// V0.oomd-guard: in the split unit oomd's rule on fyr.service may pick
	// server/, fyrd itself; oomd honours user.oomd_omit there (same owner).
	// Best effort: a file system without user xattrs only loses it. Said only
	// where fyrd reads oomd's limit (a test's directory may take no xattr).
	omitErr := omitFromOomd(filepath.Join(t.root, "server"))
	if o.OomdLimit != nil && omitErr != nil {
		o.Logf("cgroups: user.oomd_omit not set on server/ (%v); oomd may pick fyrd under this unit", omitErr)
	}
	t.watchOomd(o, filepath.Base(t.root), t.root)
	return t
}

// selfPath is fyrd's cgroup v2 path, from /proc/self/cgroup.
func selfPath(o Options) (string, error) {
	b, err := os.ReadFile(o.Self)
	if err != nil {
		return "", err
	}
	path, ok := "", false
	for _, line := range strings.Split(string(b), "\n") {
		if p, found := strings.CutPrefix(line, "0::"); found {
			path, ok = p, true
		}
	}
	if !ok {
		return "", errors.New("no cgroup v2")
	}
	return path, nil
}

func newTree(root string, o Options) *Tree {
	return &Tree{root: root, layout: LayoutUnit, agents: filepath.Join(root, "agents"), logf: o.Logf, refs: map[string]int{}, focus: map[string]focusBeat{}, raised: map[string]bool{}, weights: map[string]string{},
		cur: map[string]string{}, pending: map[string]time.Time{}, threadPercent: min(max(o.ThreadMemoryHighPercent, 0), 100)}
}

func setup(o Options, path string, err error) (*Tree, error) {
	if !o.Enable {
		return nil, errors.New("not configured")
	}
	if err != nil {
		return nil, err
	}
	// A restarted fyrd (a self-deploy) starts where the launcher is: server/.
	path = strings.TrimSuffix(path, "/server")
	// Only a service unit is split: a dev run's terminal scope stays as it is.
	if !strings.HasSuffix(path, ".service") {
		return nil, fmt.Errorf("not in a service unit (%s)", path)
	}
	root := filepath.Join(o.FS, path)
	ctl, err := os.ReadFile(filepath.Join(root, "cgroup.controllers"))
	if err != nil {
		return nil, err
	}
	have := strings.Fields(string(ctl))
	if !contains(have, "cpu") || !contains(have, "memory") {
		return nil, fmt.Errorf("controllers missing in %s: the unit needs Delegate=cpu memory", root)
	}
	server, agents := filepath.Join(root, "server"), filepath.Join(root, "agents")
	for _, d := range []string{server, agents} {
		if err := os.Mkdir(d, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("unit is not writable: %w", err)
		}
	}
	// A cgroup with controllers in subtree_control cannot hold processes:
	// every process of the unit (the launcher, fyrd) moves to server/ first.
	self := strconv.Itoa(o.PID)
	procs, _ := os.ReadFile(filepath.Join(root, "cgroup.procs"))
	for _, pid := range strings.Fields(string(procs)) {
		if pid != self {
			o.move(filepath.Join(server, "cgroup.procs"), pid) // a process that ended is gone
		}
	}
	if err := o.move(filepath.Join(server, "cgroup.procs"), self); err != nil {
		return nil, fmt.Errorf("move to server/: %w", err)
	}
	if err := put(root, "cgroup.subtree_control", controllers); err != nil {
		return nil, fmt.Errorf("enable controllers: %w", err)
	}
	t := newTree(root, o)
	var bad []string
	set := func(dir, file, v string) {
		if v == "" {
			return
		}
		if err := put(dir, file, v); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", file, err))
		}
	}
	set(server, "cpu.weight", serverWeight)
	set(server, "memory.low", o.ServerMemoryLow)
	set(agents, "cpu.weight", agentsWeight)
	set(agents, "memory.high", o.AgentsMemoryHigh)
	set(agents, "memory.max", o.AgentsMemoryMax)
	set(agents, "memory.swap.max", o.AgentsMemorySwapMax)
	// agents-lock-cap (the machine's QoS) rewrites this while the screen is locked.
	set(agents, "cpu.max", o.AgentsCPUMax)
	if err := put(agents, "cgroup.subtree_control", controllers); err != nil {
		return nil, fmt.Errorf("enable controllers in agents/: %w", err)
	}
	if len(bad) > 0 {
		o.Logf("cgroups: budget not fully set: %s", strings.Join(bad, "; "))
	}
	p := filepath.Join(agents, "probe")
	os.Mkdir(p, 0o755)
	err = o.Probe(p)
	os.Remove(p)
	if err != nil {
		return nil, fmt.Errorf("a child cannot start in agents/: %w", err)
	}
	// The cgroups of the last run's threads, when empty, with their empty
	// children (cli/, job/, undeclared/ and its parts); the kernel refuses
	// one that holds a process.
	removeEmptyTree(agents, 3)
	return t, nil
}

// removeEmptyTree removes the cgroups under dir, depth levels down, deepest
// first; dir itself stays.
func removeEmptyTree(dir string, depth int) {
	if depth == 0 {
		return
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range es {
		if e.IsDir() {
			d := filepath.Join(dir, e.Name())
			removeEmptyTree(d, depth-1)
			os.Remove(d)
		}
	}
}

func put(dir, file, v string) error { return os.WriteFile(filepath.Join(dir, file), []byte(v), 0o644) }

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// Root is the unit's cgroup directory.
func (t *Tree) Root() string {
	if t == nil {
		return ""
	}
	return t.root
}

var unsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// CLIDir is the leaf of a thread's cgroup its agent CLI runs in
// (V0.job-limits). The thread's cgroup enables cpu and memory for its
// children, so job/ and undeclared/ can carry limits of their own; the
// kernel then lets no process sit in the thread's cgroup itself.
const CLIDir = "cli"

// Thread returns the directory a thread's process starts in (its cli/ leaf in
// the unit layout), made when missing, or "" when off and in the scope layout
// (Launch). Each Thread is matched by one Release: the directories go when
// the last process that took them is gone.
func (t *Tree) Thread(id string) string {
	d, _ := t.Launch(id)
	return d
}

// Launch returns where a thread's process starts: the directory it starts in
// (made when missing; its cli/ leaf, V0.job-limits), or in the scope layout
// the command line that starts it in a new scope (V0.agents-unit); both empty
// when off. A scope starts with the CLI in its own cgroup: SplitThreads moves
// it into cli/ on the next tick. Each Launch is matched by one Release, as
// Thread.
func (t *Tree) Launch(id string) (dir string, wrap []string) {
	if t == nil {
		return "", nil
	}
	name := unsafe.ReplaceAllString(id, "_")
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.layout == LayoutScope {
		unit := t.freeUnit(name)
		t.pending[unit] = time.Now()
		w := t.weightOf(name, t.raised[name])
		t.weights[name] = w
		t.refs[name]++
		t.curMu.Lock()
		t.cur[name] = filepath.Join(t.agentsDir(), unit)
		t.curMu.Unlock()
		return "", t.sc.prefix(unit, w)
	}
	d, err := t.make(name)
	if err != nil {
		t.logf("cgroups: thread %s: %v", id, err)
		return "", nil
	}
	cli, ok := t.split(name, d)
	if !ok {
		return "", nil
	}
	t.refs[name]++
	return cli, nil
}

// Hold takes the hold of a thread's process that a previous fyrd started and
// the runner kept (decision 0023): Release then counts it, and nothing is
// made but, in the unit layout, the cli/ leaf the process moves into.
func (t *Tree) Hold(id string) {
	if t == nil {
		return
	}
	name := unsafe.ReplaceAllString(id, "_")
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.layout != LayoutScope {
		if d, err := t.make(name); err != nil {
			t.logf("cgroups: thread %s: %v", id, err)
		} else {
			t.split(name, d)
		}
	} else if _, ok := t.weights[name]; !ok {
		t.weights[name] = ""
	}
	t.refs[name]++
}

// split makes thread name's cli/ leaf in its cgroup d, moves the processes
// left in d itself (a CLI a scope or an older fyrd started there) into it,
// and enables cpu and memory for d's children (V0.job-limits). A refusal (a
// process forked during the move) is tried again at the next call and logged
// once: until then the thread's jobs run without limits of their own. false
// when cli/ cannot be made. Holds t.mu.
func (t *Tree) split(name, d string) (string, bool) {
	cli := filepath.Join(d, CLIDir)
	if err := os.Mkdir(cli, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		t.logf("cgroups: thread %s: %v", name, err)
		return "", false
	}
	if b, _ := os.ReadFile(filepath.Join(d, "cgroup.subtree_control")); strings.Contains(string(b), "memory") {
		return cli, true
	}
	for _, p := range pids(filepath.Join(d, "cgroup.procs")) {
		os.WriteFile(filepath.Join(cli, "cgroup.procs"), []byte(strconv.Itoa(p)), 0o644) // one that ended is gone
	}
	if err := put(d, "cgroup.subtree_control", controllers); err != nil {
		if t.splitFailed == nil {
			t.splitFailed = map[string]bool{}
		}
		if !t.splitFailed[d] {
			t.splitFailed[d] = true
			t.logf("guard: job limit off thread=%s: controllers for its children: %v", name, err)
		}
	} else {
		delete(t.splitFailed, d)
	}
	return cli, true
}

// SplitThreads gives every thread's cgroup its cli/ leaf (split): in the scope
// layout a scope starts with the CLI in its own cgroup, and a thread an older
// fyrd started in agents/<thread> has it there too. The sampler's Tick calls
// it.
func (t *Tree) SplitThreads() {
	if t == nil {
		return
	}
	for _, c := range t.threadCgroups() {
		if c.Name == DeployDir || c.Name == "probe" {
			continue
		}
		t.splitDir(c.Name, c.Dir)
	}
}

// splitDir is split under t.mu.
func (t *Tree) splitDir(name, d string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.split(name, d)
}

// threadPids are the processes of a thread's own cgroup td and of its cli/
// leaf: where a job starts before fyrd moves it.
func threadPids(td string) []int {
	return append(pids(filepath.Join(td, "cgroup.procs")), pids(filepath.Join(td, CLIDir, "cgroup.procs"))...)
}

// CPUTime is the CPU a thread's cgroup and its children (job/, undeclared/)
// used so far (V0.195); false when there is no such cgroup.
func (t *Tree) CPUTime(id string) (time.Duration, bool) {
	if t == nil {
		return 0, false
	}
	u, ok := readKey(filepath.Join(t.threadDir(unsafe.ReplaceAllString(id, "_")), "cpu.stat"), "usage_usec")
	return time.Duration(u) * time.Microsecond, ok
}

// Release ends one Thread's hold; the last removes the directory when no
// process is left in it (the kernel refuses otherwise).
func (t *Tree) Release(id string) {
	if t == nil {
		return
	}
	name := unsafe.ReplaceAllString(id, "_")
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.refs[name]--; t.refs[name] > 0 {
		return
	}
	delete(t.refs, name)
	delete(t.weights, name)
	dirs := []string{}
	if t.layout == LayoutScope {
		// A scope goes by itself once empty (--collect); a legacy directory
		// from before the switch is removed as before.
		t.curMu.Lock()
		delete(t.cur, name)
		t.curMu.Unlock()
		for u := range t.pending {
			if n, _, _ := scopeThread(u); n == name {
				delete(t.pending, u)
			}
		}
		if t.legacy != "" {
			dirs = append(dirs, filepath.Join(t.legacy, name))
		}
	} else {
		dirs = append(dirs, filepath.Join(t.agentsDir(), name))
	}
	for _, d := range dirs {
		delete(t.splitFailed, d)
		// Decision 0029 S5: its job's cgroup first (the kernel keeps a parent
		// with a child); a job still running keeps both until the next Release.
		// V0.job-limits: and its cli/ leaf.
		for _, sub := range []string{jobDir, CLIDir} {
			c := filepath.Join(d, sub)
			for _, f := range []string{"cgroup.procs", "cgroup.freeze", "memory.high", "cpu.max"} {
				os.Remove(filepath.Join(c, f)) // only a plain directory (tests) holds these
			}
			os.Remove(c)
		}
		os.Remove(filepath.Join(d, "cgroup.subtree_control")) // a plain directory
		os.Remove(d)
	}
}

// DeployDir is the cgroup of a self-deploy's build and tests under agents/. Thread
// cgroups are named after thread ids (t_...), so the two never meet.
const DeployDir = "self-deploy"

// Deploy is where a self-deploy's build and tests run, or "" when off and
// in the scope layout (DeployWrap starts them there).
func (t *Tree) Deploy() string {
	if t == nil || t.layout == LayoutScope {
		return ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	d, err := t.make(DeployDir)
	if err != nil {
		t.logf("cgroups: deploy: %v", err)
		return ""
	}
	return d
}

func (t *Tree) make(name string) (string, error) {
	d := filepath.Join(t.agentsDir(), name)
	if err := os.Mkdir(d, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	w := t.weightOf(name, t.raised[name])
	if err := put(d, "cpu.weight", w); err != nil {
		return "", err
	}
	t.weights[name] = w
	return d, nil
}

// FocusFor is how long a focus beat keeps a thread's weight raised: the
// window that keeps a thread watched (notify.Watch, V0.20).
const FocusFor = 45 * time.Second

// Focus records that device shows thread at now (V0.85): the thread's
// cgroup gets focusWeight until the beat is FocusFor old, and the thread
// the device showed before drops back at once.
func (t *Tree) Focus(device, thread string, now time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if name := unsafe.ReplaceAllString(thread, "_"); name == "" || name == DeployDir {
		delete(t.focus, device)
	} else {
		t.focus[device] = focusBeat{name, now}
	}
	t.reweight(now)
}

// Unfocus ends device's focus on thread, when it is still the one it shows.
func (t *Tree) Unfocus(device, thread string, now time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if b, ok := t.focus[device]; ok && b.name == unsafe.ReplaceAllString(thread, "_") {
		delete(t.focus, device)
	}
	t.reweight(now)
}

// Expire drops the beats older than FocusFor and puts their threads back
// to the plain weight; the host calls it every few seconds.
func (t *Tree) Expire(now time.Time) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reweight(now)
}

// SetRank gives the tree each thread's project priority (V0.project-priority):
// 1 high, 0 normal, -1 low. rank runs under the tree's lock, so it must read a
// published value. The weights follow at once and then on every Expire.
func (t *Tree) SetRank(rank func(thread string) int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rank = rank
	t.reweight(time.Now())
}

// weightOf is a thread cgroup's cpu.weight: focusWeight while a device shows
// it (V0.85), else its project's (V0.project-priority). Holds t.mu.
func (t *Tree) weightOf(name string, focused bool) string {
	if focused {
		return focusWeight
	}
	if t.rank == nil || name == DeployDir {
		return threadWeight
	}
	switch r := t.rank(name); {
	case r > 0:
		return highWeight
	case r < 0:
		return lowWeight
	}
	return threadWeight
}

// reweight writes cpu.weight for each thread cgroup whose weight changed: a
// focus that came or went, or its project's priority. A thread without a
// cgroup gets its weight when Thread makes one. Holds t.mu.
func (t *Tree) reweight(now time.Time) {
	want := map[string]bool{}
	for dev, b := range t.focus {
		if now.Sub(b.at) > FocusFor {
			delete(t.focus, dev)
			continue
		}
		want[b.name] = true
	}
	var later [][2]string
	for name, was := range t.weights {
		w := t.weightOf(name, want[name])
		if w == was {
			continue
		}
		d := t.threadDir(name)
		if _, err := os.Stat(d); err != nil {
			continue
		}
		if t.layout == LayoutScope && filepath.Dir(d) == t.agentsDir() {
			// Through the system manager: not under t.mu.
			later = append(later, [2]string{d, w})
			t.weights[name] = w
			continue
		}
		if err := put(d, "cpu.weight", w); err != nil {
			t.logf("cgroups: weight of %s: %v", name, err)
			continue
		}
		t.weights[name] = w
	}
	t.raised = want
	if len(later) > 0 {
		go func() {
			for _, x := range later {
				if err := t.setAttr(x[0], "cpu.weight", x[1]); err != nil {
					t.logf("cgroups: weight of %s: %v", filepath.Base(x[0]), err)
				}
			}
		}()
	}
}

// readKey reads "key value" lines (cpu.stat) and returns key's value.
func readKey(path, key string) (uint64, bool) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), " ")
		if k == key {
			n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// readBytes reads a memory file: bytes, or a size with K/M/G/T as written
// (a fake cgroupfs keeps the text), or 0 for "max" and errors.
func readBytes(path string) uint64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(b))
	mult := uint64(1)
	if n := len(s); n > 0 {
		switch s[n-1] {
		case 'K', 'k':
			mult = 1 << 10
		case 'M', 'm':
			mult = 1 << 20
		case 'G', 'g':
			mult = 1 << 30
		case 'T', 't':
			mult = 1 << 40
		}
		if mult > 1 {
			s = s[:n-1]
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return n * mult
}
