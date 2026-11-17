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
	"strconv"
	"strings"
	"sync"
)

// jobDir is the name of a thread's job cgroup.
const jobDir = "job"

// JobGroups keeps the slot holders' jobs in their own cgroups. A nil Tree
// (no cgroups) does nothing.
type JobGroups struct {
	Tree *Tree
	Proc string // default /proc

	mu      sync.Mutex
	frozen  map[string]bool
	needles map[string]string // thread → its newest heavy command's text
	held    map[string]bool   // the holders at the last Tick
}

func (g *JobGroups) proc() string {
	if g.Proc == "" {
		return "/proc"
	}
	return g.Proc
}

func (g *JobGroups) dir(thread string) string {
	return filepath.Join(g.Tree.root, "agents", unsafe.ReplaceAllString(thread, "_"), jobDir)
}

func (g *JobGroups) threadDir(thread string) string {
	return filepath.Join(g.Tree.root, "agents", unsafe.ReplaceAllString(thread, "_"))
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
	if !g.Has(thread) {
		return false
	}
	if err := put(g.dir(thread), "cgroup.freeze", "1"); err != nil {
		return false
	}
	g.mu.Lock()
	if g.frozen == nil {
		g.frozen = map[string]bool{}
	}
	g.frozen[thread] = true
	g.mu.Unlock()
	return true
}

// Thaw lets thread's job run again.
func (g *JobGroups) Thaw(thread string) {
	if g == nil || g.Tree == nil {
		return
	}
	if g.Has(thread) {
		put(g.dir(thread), "cgroup.freeze", "0")
	}
	g.mu.Lock()
	delete(g.frozen, thread)
	g.mu.Unlock()
}

// Frozen reports thread's job frozen by Freeze.
func (g *JobGroups) Frozen(thread string) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.frozen[thread]
}

// End thaws thread's job and removes its cgroup once empty (the slot went
// back); the kernel refuses while a process is left, and a later End or the
// thread's Release tries again.
func (g *JobGroups) End(thread string) {
	if g == nil || g.Tree == nil {
		return
	}
	g.Thaw(thread)
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
	if g == nil || g.Tree == nil {
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
