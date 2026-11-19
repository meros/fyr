package cgroup

// Decision 0032 slice 2: a thread's undeclared work split into the parts that
// can take turns. A runner such as turbo is one root with several heavy
// children, so the unit is found by walking down from each root to where the
// memory fans out. Each unit gets agents/<thread>/undeclared/u<pid>/ and is
// frozen on its own; what is left (the runner, its shell) keeps running, since
// it only waits for its children. cgroup.freeze is hierarchical, so during a
// rotation undeclared/ itself stays thawed.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// unitShare is the part of a subtree's memory a child must hold to count as
// heavy work of its own: a share, so it fits a small job and a large one.
const unitShare = 0.10

// unitPrefix starts the name of a unit's cgroup under undeclared/.
const unitPrefix = "u"

// Unit is one part of a thread's undeclared work that can be frozen alone.
type Unit struct {
	PID     int
	Cmdline string
	RSS     uint64 // the subtree's resident memory, bytes
}

// unitDirs are the unit cgroups under dir.
func unitDirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), unitPrefix) {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// undeclaredPids are the processes of a thread's cgroup directory td: its own,
// those of undeclared/ and those of the units under it.
func undeclaredPids(td string) []int {
	ud := filepath.Join(td, undeclaredDir)
	in := append(pids(filepath.Join(td, "cgroup.procs")), pids(filepath.Join(ud, "cgroup.procs"))...)
	for _, d := range unitDirs(ud) {
		in = append(in, pids(filepath.Join(d, "cgroup.procs"))...)
	}
	return in
}

// holdsWork says a process is in undeclared directory ud or a unit under it.
func holdsWork(ud string) bool {
	if len(pids(filepath.Join(ud, "cgroup.procs"))) > 0 {
		return true
	}
	for _, d := range unitDirs(ud) {
		if len(pids(filepath.Join(d, "cgroup.procs"))) > 0 {
			return true
		}
	}
	return false
}

// family is the parent links and memory of a thread's processes.
type family struct {
	children map[int][]int
	here     map[int]bool
	rss      map[int]uint64
	memo     map[int]uint64
}

func (g *JobGroups) family(thread string) *family {
	f := &family{children: map[int][]int{}, here: map[int]bool{}, rss: map[int]uint64{}, memo: map[int]uint64{}}
	for _, p := range undeclaredPids(g.threadDir(thread)) {
		pp := g.ppid(p)
		if pp == 0 || f.here[p] {
			continue // ended meanwhile, or listed twice during a move
		}
		f.here[p] = true
		f.rss[p] = readRSS(g.proc(), p)
		f.children[pp] = append(f.children[pp], p)
	}
	for _, c := range f.children {
		sort.Ints(c)
	}
	return f
}

// subtree is the resident memory of p and its descendants.
func (f *family) subtree(p int) uint64 {
	if v, ok := f.memo[p]; ok {
		return v
	}
	t := f.rss[p]
	for _, c := range f.children[p] {
		t += f.subtree(c)
	}
	f.memo[p] = t
	return t
}

// descendants is p and everything under it, p first.
func (f *family) descendants(p int) []int {
	out := []int{p}
	for _, c := range f.children[p] {
		out = append(out, f.descendants(c)...)
	}
	return out
}

// split is the units under root: the walk goes down while one child holds the
// work and stops where two or more do. A process that itself holds more than
// its only heavy child is the unit. A process that already is a unit (pinned)
// stays one, so the units do not shift as their memory changes during a
// rotation.
func (f *family) split(root int, pinned func(int) bool) []int {
	n := root
	for depth := 0; depth < 64; depth++ {
		if pinned(n) {
			return []int{n}
		}
		total := f.subtree(n)
		var heavy []int
		for _, c := range f.children[n] {
			if pinned(c) || (total > 0 && float64(f.subtree(c)) >= unitShare*float64(total)) {
				heavy = append(heavy, c)
			}
		}
		switch {
		case len(heavy) >= 2:
			return heavy
		case len(heavy) == 1 && (pinned(heavy[0]) || f.rss[n] <= f.subtree(heavy[0])):
			n = heavy[0]
		default:
			return []int{n}
		}
	}
	return []int{n}
}

// Units are the parts of thread's undeclared work under roots, in pid order.
func (g *JobGroups) Units(thread string, roots []Root) []Unit {
	if g == nil || g.Tree == nil {
		return nil
	}
	f := g.family(thread)
	pinned := func(p int) bool {
		st, err := os.Stat(g.unitDir(thread, p))
		return err == nil && st.IsDir()
	}
	var out []Unit
	for _, r := range roots {
		if !f.here[r.PID] {
			continue
		}
		for _, p := range f.split(r.PID, pinned) {
			out = append(out, Unit{PID: p, Cmdline: g.cmdline(p), RSS: f.subtree(p)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PID < out[j].PID })
	return out
}

func (g *JobGroups) unitDir(thread string, pid int) string {
	return filepath.Join(g.udir(thread), fmt.Sprintf("%s%d", unitPrefix, pid))
}

// RotateUndeclared lets the unit with pid active run and freezes the others.
// A unit seen for the first time moves into its cgroup with its descendants;
// the processes it starts later are born there. The others are frozen before
// active is thawed, so two units never run together. False when no cgroup
// could be made or nothing is frozen.
func (g *JobGroups) RotateUndeclared(thread string, units []Unit, active int) bool {
	if g == nil || g.Tree == nil || len(units) == 0 {
		return false
	}
	ud := g.udir(thread)
	if err := os.Mkdir(ud, 0o755); err != nil && !os.IsExist(err) {
		g.Tree.logf("cgroups: undeclared work of %s: %v", thread, err)
		return false
	}
	f := g.family(thread)
	froze := false
	for _, u := range units {
		d := g.unitDir(thread, u.PID)
		err := os.Mkdir(d, 0o755)
		if err != nil && !os.IsExist(err) {
			g.Tree.logf("cgroups: undeclared unit of %s: %v", thread, err)
			continue
		}
		if err == nil {
			for _, p := range f.descendants(u.PID) {
				// A process that ended meanwhile cannot move; the rest does.
				os.WriteFile(filepath.Join(d, "cgroup.procs"), []byte(fmt.Sprint(p)), 0o644)
			}
		}
		if u.PID != active && put(d, "cgroup.freeze", "1") == nil {
			froze = true
		}
	}
	// A unit no longer listed (its first process ended) must not hold what is
	// left of it frozen.
	listed := map[string]bool{}
	for _, u := range units {
		listed[g.unitDir(thread, u.PID)] = true
	}
	for _, d := range unitDirs(ud) {
		if !listed[d] {
			put(d, "cgroup.freeze", "0")
		}
	}
	// A freeze of the whole work (FreezeUndeclared) would hold every unit.
	put(ud, "cgroup.freeze", "0")
	put(g.unitDir(thread, active), "cgroup.freeze", "0")
	if !froze {
		return false
	}
	g.mu.Lock()
	if g.ufrozen == nil {
		g.ufrozen = map[string]bool{}
	}
	g.ufrozen[thread] = true
	g.mu.Unlock()
	g.ReclaimFrozen(thread) // V0.guard-swap: the units that wait give up their RAM
	return true
}

// MajorFaultBytes is how much thread's cgroup has read back from swap and
// files so far: the counter a slice length is measured from.
func (g *JobGroups) MajorFaultBytes(thread string) uint64 {
	if g == nil || g.Tree == nil {
		return 0
	}
	n, _ := readKey(filepath.Join(g.threadDir(thread), "memory.stat"), "pgmajfault")
	return n * uint64(os.Getpagesize())
}
