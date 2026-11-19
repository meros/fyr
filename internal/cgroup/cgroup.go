// Package cgroup gives every process Fyr starts a cgroup of its own under
// one budget for agent work (SPEC 0.5 V0.80), as T3's launcher did for its
// server (t3code.nix in the machine's NixOS config):
//
//	fyr.service/          Delegate=cpu memory; holds no process once split
//	  server/             the launcher and fyrd: cpu.weight 1000, memory.low
//	  agents/             the budget: cpu.max, memory.high/max/swap.max
//	    <thread id>/      one agent CLI and everything it starts
//	    deploy/           a self-deploy's build and tests
//
// A child starts directly in its cgroup (clone3 CLONE_INTO_CGROUP, see
// proc.IntoCgroup), so nothing sweeps PIDs. Without a writable delegated
// cgroup v2 Setup returns nil, a nil *Tree hands out no directories, and
// every process runs in the daemon's cgroup as before.
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
	move  func(file, pid string) error // tests watch the moves
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
	logf func(string, ...any)
	mu   sync.Mutex
	refs map[string]int
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
	t, err := setup(o)
	if err != nil {
		o.Logf("cgroups: off (%v); every process runs in the daemon's cgroup", err)
		return nil
	}
	o.Logf("cgroups: on in %s (server/, agents/<thread>/, agents/self-deploy/)", t.root)
	return t
}

func setup(o Options) (*Tree, error) {
	if !o.Enable {
		return nil, errors.New("not configured")
	}
	b, err := os.ReadFile(o.Self)
	if err != nil {
		return nil, err
	}
	path, ok := "", false
	for _, line := range strings.Split(string(b), "\n") {
		if p, found := strings.CutPrefix(line, "0::"); found {
			path, ok = p, true
		}
	}
	if !ok {
		return nil, errors.New("no cgroup v2")
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
	t := &Tree{root: root, logf: o.Logf, refs: map[string]int{}, focus: map[string]focusBeat{}, raised: map[string]bool{}, weights: map[string]string{}, threadPercent: min(max(o.ThreadMemoryHighPercent, 0), 100)}
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
	// The cgroups of the last run's threads, when empty.
	if es, err := os.ReadDir(agents); err == nil {
		for _, e := range es {
			if e.IsDir() {
				os.Remove(filepath.Join(agents, e.Name()))
			}
		}
	}
	return t, nil
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

// Thread returns the directory a thread's process starts in, made when
// missing, or "" when off. Each Thread is matched by one Release: the
// directory goes when the last process that took it is gone.
func (t *Tree) Thread(id string) string {
	if t == nil {
		return ""
	}
	name := unsafe.ReplaceAllString(id, "_")
	t.mu.Lock()
	defer t.mu.Unlock()
	d, err := t.make(name)
	if err != nil {
		t.logf("cgroups: thread %s: %v", id, err)
		return ""
	}
	t.refs[name]++
	return d
}

// CPUTime is the CPU a thread's cgroup and its children (job/, undeclared/)
// used so far (V0.195); false when there is no such cgroup.
func (t *Tree) CPUTime(id string) (time.Duration, bool) {
	if t == nil {
		return 0, false
	}
	u, ok := readKey(filepath.Join(t.root, "agents", unsafe.ReplaceAllString(id, "_"), "cpu.stat"), "usage_usec")
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
	// Decision 0029 S5: its job's cgroup first (the kernel keeps a parent
	// with a child); a job still running keeps both until the next Release.
	job := filepath.Join(t.root, "agents", name, jobDir)
	for _, f := range []string{"cgroup.procs", "cgroup.freeze"} {
		os.Remove(filepath.Join(job, f)) // only a plain directory (tests) holds these
	}
	os.Remove(job)
	os.Remove(filepath.Join(t.root, "agents", name))
}

// DeployDir is the cgroup of a self-deploy's build and tests under agents/. Thread
// cgroups are named after thread ids (t_...), so the two never meet.
const DeployDir = "self-deploy"

// Deploy is where a self-deploy's build and tests run, or "" when off.
func (t *Tree) Deploy() string {
	if t == nil {
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
	d := filepath.Join(t.root, "agents", name)
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
	for name, was := range t.weights {
		w := t.weightOf(name, want[name])
		if w == was {
			continue
		}
		d := filepath.Join(t.root, "agents", name)
		if _, err := os.Stat(d); err != nil {
			continue
		}
		if err := put(d, "cpu.weight", w); err != nil {
			t.logf("cgroups: weight of %s: %v", name, err)
			continue
		}
		t.weights[name] = w
	}
	t.raised = want
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
