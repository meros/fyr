package cgroup

// Decision 0029 slice S5 (#189): a slot holder's heavy job in a cgroup of its
// own, agents/<thread>/job/, so the memory guard can freeze it without
// freezing the agent's CLI. The command is never rewritten (a rewrite at the
// gate would need an "allow" decision, which would skip auto mode's own
// check of the whole command): fyrd finds the job's processes in the thread's
// cgroup by their command line and moves them. A thread's cgroup enables no
// controllers for its children, so a child may hold processes beside the
// CLI; cgroup.freeze and cpu.stat work in any cgroup.

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/meros/fyr/internal/slots"
)

// jobDir is the name of a thread's job cgroup.
const jobDir = "job"

// JobGroups keeps the slot holders' jobs in their own cgroups. With a nil Tree
// nothing is moved; given Trees and CLIs it pauses a job with SIGSTOP instead
// of a freeze (V0.process-tree-hosts, procstop.go).
type JobGroups struct {
	Tree *Tree
	Proc string // default /proc

	Trees *TreeReader
	CLIs  func() map[string]int
	KV    slots.KV                                // keeps the stopped pids across a restart
	Kill  func(pid int, sig syscall.Signal) error // default syscall.Kill

	stopMu  sync.Mutex // orders the stops and their KV writes; taken without mu
	stopped map[string][]int

	mu      sync.Mutex
	frozen  map[string]bool
	needles map[string]string // thread → its newest heavy command's text
	held    map[string]bool   // the holders at the last Tick
	// V0.164: the undeclared work frozen per thread, and the Bash commands
	// the gate saw per thread (oldest first, at most maxBashes).
	ufrozen map[string]bool
	bashes  map[string][]string
	// V0.guard-swap: when each thread's frozen work was last moved to swap
	// (zero while a reclaim runs).
	reclaimed map[string]time.Time
}

// undeclaredDir is the name of a thread's undeclared-work cgroup, beside job/
// so a declared and an undeclared job never mix.
const undeclaredDir = "undeclared"

const maxBashes = 10

func (g *JobGroups) proc() string {
	if g.Proc == "" {
		return "/proc"
	}
	return g.Proc
}

func (g *JobGroups) dir(thread string) string {
	return filepath.Join(g.threadDir(thread), jobDir)
}

func (g *JobGroups) threadDir(thread string) string {
	return g.Tree.threadDir(unsafe.ReplaceAllString(thread, "_"))
}

// ppid reads a process's parent from /proc/<pid>/stat; 0 when gone.
func (g *JobGroups) ppid(pid int) int {
	b, err := os.ReadFile(filepath.Join(g.proc(), strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0
	}
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 {
		s = s[i+1:]
	}
	f := strings.Fields(s)
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}

func (g *JobGroups) cmdline(pid int) string {
	b, _ := os.ReadFile(filepath.Join(g.proc(), strconv.Itoa(pid), "cmdline"))
	return strings.ReplaceAll(string(b), "\x00", " ")
}

func pids(file string) []int {
	b, _ := os.ReadFile(file)
	var out []int
	for _, f := range strings.Fields(string(b)) {
		if n, err := strconv.Atoi(f); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// Sync moves thread's job into its job cgroup: the topmost processes of the
// thread's cgroup whose command line holds needle (the heavy command), and
// every process descended from them; the processes they start later are
// born there. Processes already moved are not read again. Call it on every
// tick while the thread holds a slot.
func (g *JobGroups) Sync(thread, needle string) {
	if g == nil || g.Tree == nil || strings.TrimSpace(needle) == "" {
		return
	}
	td := g.threadDir(thread)
	in := pids(filepath.Join(td, "cgroup.procs"))
	if len(in) == 0 {
		return
	}
	here := map[int]bool{}
	for _, p := range in {
		here[p] = true
	}
	parent := map[int]int{}
	for _, p := range in {
		parent[p] = g.ppid(p)
	}
	jd := g.dir(thread)
	inJob := map[int]bool{}
	for _, p := range pids(filepath.Join(jd, "cgroup.procs")) {
		inJob[p] = true
	}
	// A process belongs to the job when it or an ancestor (in this cgroup,
	// or already in the job) runs the command.
	matches := map[int]bool{}
	var belongs func(p, depth int) bool
	belongs = func(p, depth int) bool {
		if depth > 64 || p <= 1 {
			return false
		}
		if inJob[p] {
			return true
		}
		if m, ok := matches[p]; ok {
			return m
		}
		m := strings.Contains(g.cmdline(p), needle)
		if !m {
			if pp, ok := parent[p]; ok && (here[pp] || inJob[pp]) {
				m = belongs(pp, depth+1)
			}
		}
		matches[p] = m
		return m
	}
	var move []int
	for _, p := range in {
		if belongs(p, 0) {
			move = append(move, p)
		}
	}
	if len(move) == 0 {
		return
	}
	if err := os.Mkdir(jd, 0o755); err != nil && !os.IsExist(err) {
		g.Tree.logf("cgroups: job of %s: %v", thread, err)
		return
	}
	for _, p := range move {
		// A process that ended meanwhile cannot move; the next tick tries the rest.
		os.WriteFile(filepath.Join(jd, "cgroup.procs"), []byte(strconv.Itoa(p)), 0o644)
	}
}

// Has reports a job cgroup for thread.
func (g *JobGroups) Has(thread string) bool {
	if g == nil || g.Tree == nil {
		return false
	}
	_, err := os.Stat(g.dir(thread))
	return err == nil
}

// Freeze freezes thread's job (not its CLI); false without a job cgroup.
func (g *JobGroups) Freeze(thread string) bool {
	if g.canStop() {
		if !g.stopJobOf(thread) {
			return false
		}
	} else if !g.Has(thread) {
		return false
	} else if err := put(g.dir(thread), "cgroup.freeze", "1"); err != nil {
		return false
	}
	g.mu.Lock()
	if g.frozen == nil {
		g.frozen = map[string]bool{}
	}
	g.frozen[thread] = true
	g.mu.Unlock()
	g.ReclaimFrozen(thread) // V0.guard-swap; nothing without the tree
	return true
}

// Thaw lets thread's job run again.
func (g *JobGroups) Thaw(thread string) {
	if g.canStop() {
		g.cont(stopJob + thread)
	} else if g == nil || g.Tree == nil {
		return
	} else if g.Has(thread) {
		put(g.dir(thread), "cgroup.freeze", "0")
	}
	g.mu.Lock()
	delete(g.frozen, thread)
	g.mu.Unlock()
}

// Frozen reports thread's job frozen by Freeze, or its undeclared work by
// FreezeUndeclared.
func (g *JobGroups) Frozen(thread string) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.frozen[thread] || g.ufrozen[thread]
}

// Bash keeps a Bash command the gate hook saw from thread (any command: the
// undeclared work's command is found among them).
func (g *JobGroups) Bash(thread, command string) {
	if g == nil || strings.TrimSpace(command) == "" {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.bashes == nil {
		g.bashes = map[string][]string{}
	}
	b := append(g.bashes[thread], command)
	if len(b) > maxBashes {
		b = b[len(b)-maxBashes:]
	}
	g.bashes[thread] = b
}

// Bashes are thread's newest Bash commands, oldest first.
func (g *JobGroups) Bashes(thread string) []string {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.bashes[thread]...)
}

func (g *JobGroups) udir(thread string) string {
	return filepath.Join(g.threadDir(thread), undeclaredDir)
}

// HasUndeclared reports an undeclared-work cgroup for thread.
func (g *JobGroups) HasUndeclared(thread string) bool {
	if g == nil || g.Tree == nil {
		return false
	}
	_, err := os.Stat(g.udir(thread))
	return err == nil
}

// UndeclaredEmpty reports that thread's undeclared-work cgroup holds no
// process any more, or is gone: what was frozen in it ended, so the freeze
// holds nothing (decision 0032 slice 1). False while a process is in it.
func (g *JobGroups) UndeclaredEmpty(thread string) bool {
	if g.canStop() {
		return g.stopGone(stopUndeclared + thread)
	}
	if g == nil || g.Tree == nil {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(g.udir(thread), "cgroup.events"))
	if err != nil {
		return os.IsNotExist(err)
	}
	return strings.Contains(string(raw), "populated 0")
}

// FreezeUndeclared moves the roots and all their descendants into
// agents/<thread>/undeclared/ and freezes it (V0.164 section 7); the CLI, its
// MCP servers and any resident stay. A process that ends during the move is
// skipped. False without a tree, without roots, or when nothing could be moved.
func (g *JobGroups) FreezeUndeclared(thread string, roots []Root) bool {
	if g.canStop() && g.stopRoots(thread, roots) {
		g.mu.Lock()
		if g.ufrozen == nil {
			g.ufrozen = map[string]bool{}
		}
		g.ufrozen[thread] = true
		g.mu.Unlock()
		return true
	}
	if g == nil || g.Tree == nil || len(roots) == 0 {
		return false
	}
	in := pids(filepath.Join(g.threadDir(thread), "cgroup.procs"))
	children := map[int][]int{}
	here := map[int]bool{}
	for _, p := range in {
		here[p] = true
		if pp := g.ppid(p); pp > 0 {
			children[pp] = append(children[pp], p)
		}
	}
	seen := map[int]bool{}
	var move []int
	var walk func(p int)
	walk = func(p int) {
		if seen[p] {
			return
		}
		seen[p] = true
		move = append(move, p)
		kids := children[p]
		sort.Ints(kids)
		for _, c := range kids {
			walk(c)
		}
	}
	rs := make([]int, 0, len(roots))
	for _, r := range roots {
		rs = append(rs, r.PID)
	}
	sort.Ints(rs)
	for _, r := range rs {
		if here[r] {
			seen[r] = true
			move = append(move, r)
		}
	}
	for _, r := range rs {
		for _, c := range children[r] {
			walk(c)
		}
	}
	ud := g.udir(thread)
	// A thaw leaves the work in ud: a later freeze of the episode has nothing
	// to move and still has work to hold (V0.191).
	if len(move) == 0 && !holdsWork(ud) {
		return false
	}
	if err := os.Mkdir(ud, 0o755); err != nil && !os.IsExist(err) {
		g.Tree.logf("cgroups: undeclared work of %s: %v", thread, err)
		return false
	}
	for _, p := range move {
		// A process that ended meanwhile cannot move; the rest does.
		os.WriteFile(filepath.Join(ud, "cgroup.procs"), []byte(strconv.Itoa(p)), 0o644)
	}
	if err := put(ud, "cgroup.freeze", "1"); err != nil {
		return false
	}
	g.mu.Lock()
	if g.ufrozen == nil {
		g.ufrozen = map[string]bool{}
	}
	g.ufrozen[thread] = true
	g.mu.Unlock()
	g.ReclaimFrozen(thread) // V0.guard-swap
	return true
}

// ThawUndeclared lets thread's undeclared work run again.
func (g *JobGroups) ThawUndeclared(thread string) {
	if g.canStop() {
		g.cont(stopUndeclared + thread)
	} else if g == nil || g.Tree == nil {
		return
	} else if g.HasUndeclared(thread) {
		put(g.udir(thread), "cgroup.freeze", "0")
		for _, d := range unitDirs(g.udir(thread)) { // decision 0032 slice 2
			put(d, "cgroup.freeze", "0")
		}
	}
	g.mu.Lock()
	delete(g.ufrozen, thread)
	g.mu.Unlock()
}

// EndUndeclared thaws thread's undeclared work and removes its cgroup once
// empty (the kernel refuses while a process is left; a later call tries again).
func (g *JobGroups) EndUndeclared(thread string) {
	g.ThawUndeclared(thread)
	if g == nil || g.Tree == nil {
		return
	}
	ud := g.udir(thread)
	for _, d := range unitDirs(ud) {
		removeEmptyGroup(d)
	}
	removeEmptyGroup(ud)
}

// removeEmptyGroup removes a cgroup that holds no process. A plain directory
// (tests) holds the files fyrd wrote; the kernel's rmdir needs none of this.
func removeEmptyGroup(dir string) {
	if len(pids(filepath.Join(dir, "cgroup.procs"))) > 0 {
		return
	}
	for _, f := range []string{"cgroup.procs", "cgroup.freeze"} {
		os.Remove(filepath.Join(dir, f))
	}
	os.Remove(dir)
}

// End thaws thread's job and removes its cgroup once empty (the slot went
// back); the kernel refuses while a process is left, and a later End or the
// thread's Release tries again.
func (g *JobGroups) End(thread string) {
	g.Thaw(thread)
	if g == nil || g.Tree == nil {
		return
	}
	jd := g.dir(thread)
	if len(pids(filepath.Join(jd, "cgroup.procs"))) > 0 {
		return
	}
	// A plain directory (tests) holds the files fyrd wrote; the kernel's
	// rmdir needs none of this.
	for _, f := range []string{"cgroup.procs", "cgroup.freeze"} {
		os.Remove(filepath.Join(jd, f))
	}
	os.Remove(jd)
}

// Seen keeps the text of thread's newest heavy command, the needle Sync
// looks for.
func (g *JobGroups) Seen(thread, text string) {
	if g == nil || text == "" {
		return
	}
	g.mu.Lock()
	if g.needles == nil {
		g.needles = map[string]string{}
	}
	g.needles[thread] = text
	g.mu.Unlock()
}

// Tick syncs the holders' jobs (by their newest heavy command) and ends the
// jobs of the threads that no longer hold a slot. The sampler's Tick calls
// it, outside every lock.
func (g *JobGroups) Tick(held []string) {
	if g == nil || (g.Tree == nil && !g.canStop()) {
		return
	}
	now := map[string]bool{}
	g.mu.Lock()
	needles := make(map[string]string, len(g.needles))
	for k, v := range g.needles {
		needles[k] = v
	}
	was := g.held
	g.held = map[string]bool{}
	for _, h := range held {
		g.held[h] = true
	}
	g.mu.Unlock()
	for _, h := range held {
		now[h] = true
		if n := needles[h]; n != "" {
			g.Sync(h, n)
		}
	}
	for h := range was {
		if !now[h] {
			g.End(h)
			g.mu.Lock()
			delete(g.needles, h)
			g.mu.Unlock()
		}
	}
}
