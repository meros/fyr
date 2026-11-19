package cgroup

// V0.176 (3), amended by V0.worktree-sweep (worktrees.go): temporary files threads leave. A layout run's Chrome profile
// (<tmp>/fyr-layout-*) whose Chrome is gone, and the CLI's scratch folder of
// a settled or archived thread with no live CLI (<tmp>/claude-<uid>/<project>/
// <session>/), are removed by the sweep. A worktree of a settled thread (its
// folder, or a worktree of its repository on one of its changes' branches) is
// listed, never removed by the sweep: a settled thread can wake in that
// folder. The owner's Remove removes one only when it holds no uncommitted
// work and its HEAD is on a remote branch, checked again at the press.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Temp is one listed temporary folder.
type Temp struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"` // "worktree"
	Thread string `json:"thread,omitempty"`
	Bytes  uint64 `json:"bytes"`
	// Safe: Remove may remove it; Why says what holds it otherwise.
	Safe bool   `json:"safe"`
	Why  string `json:"why,omitempty"`
	// RemoveAt: Fyr removes it then unless someone works in it (V0.worktree-sweep).
	RemoveAt *time.Time `json:"removeAt,omitempty"`
}

// TempThread is what the sweep needs of a thread.
type TempThread struct {
	ID, SessionID, Cwd string
	Asleep             bool
	LiveCLI            bool
	Branches           []string // its changes' branches
	// Done: worktrees Fyr made for its changes that landed or ended, and
	// when (V0.change-worktree-cleanup). Such a worktree may go DoneGrace
	// after that, and after its HEAD last moved, also while the thread is
	// awake.
	Done map[string]time.Time
}

// Temps sweeps and lists. Sweep runs on one goroutine; View never waits.
type Temps struct {
	Dir      string // the system temp folder; default os.TempDir()
	Now      func() time.Time
	Threads  func() []TempThread // the threads to look at (asleep ones recently, and the rest)
	Cmdlines func() []string     // every process's command line (a profile in use is named there)
	Logf     func(string, ...any)

	// V0.worktree-sweep. Repos: more folders whose repositories' worktrees
	// are looked at (the projects'). Auto: remove the ones that may go;
	// Cwds (every live process's working folder) must be known for it.
	Repos     func() []string
	Cwds      func() []string
	Auto      bool
	Idle      time.Duration // default WorktreeIdle
	NoFetch   bool          // tests: no fetch of PR heads
	OnRemoved func(thread, line string)
	KV        interface {
		GetKV(string) (string, error)
		SetKV(string, string) error
	}

	mu      sync.Mutex // the scratch and profile sweep
	wmu     sync.Mutex // the worktree sweep (fetched, sizes, pub)
	rmu     sync.Mutex // a removal or a restore, and the removed records
	pub     atomic.Pointer[[]Temp]
	fetched map[string]time.Time
	sizes   map[string]sized
}

// ProfileAge: a layout profile younger than this stays (its Chrome may be starting).
const ProfileAge = 30 * time.Minute

func (tp *Temps) dir() string {
	if tp.Dir != "" {
		return tp.Dir
	}
	return os.TempDir()
}

func (tp *Temps) logf(f string, a ...any) {
	if tp.Logf != nil {
		tp.Logf(f, a...)
	}
}

// View is the listed worktrees.
func (tp *Temps) View() []Temp {
	if p := tp.pub.Load(); p != nil {
		return slices.Clone(*p)
	}
	return nil
}

// Sweep removes the safe profiles and scratch folders; with worktrees it
// then runs SweepWorktrees (git runs: call it seldom).
func (tp *Temps) Sweep(worktrees bool) {
	tp.sweepFiles()
	if worktrees {
		tp.SweepWorktrees()
	}
}

func (tp *Temps) sweepFiles() {
	tp.mu.Lock()
	defer tp.mu.Unlock()
	now := time.Now()
	if tp.Now != nil {
		now = tp.Now()
	}
	var threads []TempThread
	if tp.Threads != nil {
		threads = tp.Threads()
	}
	// Layout profiles.
	if ps, _ := filepath.Glob(filepath.Join(tp.dir(), "fyr-layout-*")); len(ps) > 0 {
		var cmds []string
		if tp.Cmdlines != nil {
			cmds = tp.Cmdlines()
		}
		for _, p := range ps {
			st, err := os.Stat(p)
			if err != nil || !st.IsDir() || now.Sub(st.ModTime()) < ProfileAge {
				continue
			}
			if slices.ContainsFunc(cmds, func(c string) bool { return strings.Contains(c, p) }) {
				continue
			}
			n := size(p)
			if os.RemoveAll(p) == nil {
				tp.logf("leftovers: removed the layout profile %s (%s)", p, mb(n))
			}
		}
	}
	// Scratch folders of asleep threads.
	sessions := map[string]TempThread{}
	for _, th := range threads {
		if th.SessionID != "" {
			sessions[th.SessionID] = th
		}
	}
	if dirs, _ := filepath.Glob(filepath.Join(tp.dir(), "claude-*", "*", "*")); len(dirs) > 0 {
		for _, d := range dirs {
			th, ok := sessions[filepath.Base(d)]
			if !ok || !th.Asleep || th.LiveCLI {
				continue
			}
			n := size(d)
			if os.RemoveAll(d) == nil {
				tp.logf("leftovers: removed %s's scratch folder %s (%s)", th.ID, d, mb(n))
			}
		}
	}
}

// SweepWorktrees lists the worktrees nobody works in and removes the ones
// that may go (V0.worktree-sweep). It runs git for every worktree, so it has
// its own lock and returns at once when a sweep runs already.
func (tp *Temps) SweepWorktrees() {
	if !tp.wmu.TryLock() {
		return
	}
	defer tp.wmu.Unlock()
	now := time.Now()
	if tp.Now != nil {
		now = tp.Now()
	}
	var threads []TempThread
	if tp.Threads != nil {
		threads = tp.Threads()
	}
	idle := tp.Idle
	if idle == 0 {
		idle = WorktreeIdle
	}
	folders := []string{}
	for _, th := range threads {
		if th.Cwd != "" {
			folders = append(folders, th.Cwd)
		}
	}
	if tp.Repos != nil {
		folders = append(folders, tp.Repos()...)
	}
	var cwds []string
	auto := tp.Auto && tp.Cwds != nil
	if auto {
		cwds = tp.Cwds()
	}
	var out []Temp
	seen := map[string]bool{} // repositories, by git dir
	for _, f := range folders {
		common := gitOut(f, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if common == "" || seen[common] {
			continue
		}
		seen[common] = true
		wts := worktreesOf(f)
		if !slices.ContainsFunc(wts, func(w wtree) bool { return !w.main }) {
			continue
		}
		fetched := false
		for _, w := range wts {
			if w.main {
				continue
			}
			if _, err := os.Stat(w.path); err != nil {
				continue // prunable: its folder is gone already
			}
			// Whose: a thread working in it, or on its branch; an awake one wins.
			var owner *TempThread
			for i := range threads {
				th := &threads[i]
				if (th.Cwd != "" && inside(th.Cwd, w.path)) || (w.branch != "" && slices.Contains(th.Branches, w.branch)) {
					if owner == nil || (owner.Asleep && !th.Asleep) {
						owner = th
					}
				}
			}
			if owner != nil && !owner.Asleep {
				continue // in use: not listed
			}
			x := Temp{Path: w.path, Kind: "worktree"}
			if owner != nil {
				x.Thread = owner.ID
			}
			var doneAt time.Time // V0.change-worktree-cleanup
			for _, th := range threads {
				if at, ok := th.Done[w.path]; ok {
					doneAt = at
					if x.Thread == "" {
						x.Thread = th.ID
					}
				}
			}
			if auto && !fetched {
				tp.fetchPulls(common, f, now)
				fetched = true
			}
			x.Safe, x.Why = worktreeSafe(w.path)
			if x.Safe && w.locked {
				x.Safe, x.Why = false, "locked"
			}
			if x.Safe && auto {
				busy := slices.ContainsFunc(cwds, func(c string) bool { return inside(c, w.path) })
				at := lastMove(w.path).Add(idle)
				if !doneAt.IsZero() {
					at = lastMove(w.path).Add(DoneGrace)
					if d := doneAt.Add(DoneGrace); d.After(at) {
						at = d
					}
				}
				switch {
				case busy || (owner != nil && owner.LiveCLI):
					x.Why = "in use"
				case now.Before(at):
					x.RemoveAt = &at
				case !tp.stillIdle(w.path, w.branch):
					x.Why = "in use"
				default:
					n := tp.sizeOf(w.path, now)
					if err := tp.removeWorktree(w, common, x.Thread, now); err != nil {
						tp.logf("worktrees: kept %s: %v", w.path, err)
						x.Safe, x.Why = false, err.Error()
						break
					}
					tp.logf("worktrees: removed %s (%s), its HEAD %s is pinned", w.path, mb(n), short(w.head))
					if tp.OnRemoved != nil && x.Thread != "" {
						tp.OnRemoved(x.Thread, removedLine(w, n))
					}
					continue
				}
			}
			x.Bytes = tp.sizeOf(w.path, now)
			out = append(out, x)
		}
	}
	tp.pub.Store(&out)
}

// stillIdle looks again just before a removal: a thread may have started
// there while the sweep ran.
func (tp *Temps) stillIdle(path, branch string) bool {
	if tp.Cwds == nil || slices.ContainsFunc(tp.Cwds(), func(c string) bool { return inside(c, path) }) {
		return false
	}
	if tp.Threads != nil {
		for _, th := range tp.Threads() {
			if (th.Cwd != "" && inside(th.Cwd, path)) || (branch != "" && slices.Contains(th.Branches, branch)) {
				if !th.Asleep || th.LiveCLI {
					return false
				}
			}
		}
	}
	return true
}

func short(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// removedLine is the thread's line about a removed worktree.
func removedLine(w wtree, n uint64) string {
	on := "its commit is on the remote"
	if w.branch != "" {
		on = "the branch " + w.branch + " is kept and on the remote"
	}
	return fmt.Sprintf("Fyr removed the worktree %s (%s): nothing was uncommitted and %s. Fyr makes it again when this thread works there; to keep a worktree, run git worktree lock.", w.path, mb(n), on)
}

// sizeOf caches a worktree's size for an hour: node_modules make a walk slow.
func (tp *Temps) sizeOf(p string, now time.Time) uint64 {
	if c, ok := tp.sizes[p]; ok && now.Sub(c.at) < time.Hour {
		return c.n
	}
	if tp.sizes == nil {
		tp.sizes = map[string]sized{}
	}
	n := size(p)
	tp.sizes[p] = sized{n, now}
	return n
}

type sized struct {
	n  uint64
	at time.Time
}

// Remove is the owner's Remove of a listed worktree, when it is still safe.
func (tp *Temps) Remove(path string) error {
	var x *Temp
	for _, t := range tp.View() {
		if t.Path == path {
			x = &t
			break
		}
	}
	if x == nil {
		return errors.New("not a listed folder")
	}
	if ok, why := worktreeSafe(path); !ok {
		return fmt.Errorf("kept: %s", why)
	}
	common := gitOut(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	var w *wtree
	for _, c := range worktreesOf(path) {
		if c.path == path && !c.main {
			w = &c
			break
		}
	}
	if w == nil || common == "" {
		return errors.New("not a linked worktree")
	}
	if w.locked {
		return errors.New("kept: locked")
	}
	if err := tp.removeWorktree(*w, common, x.Thread, time.Now()); err != nil {
		return err
	}
	tp.logf("leftovers: removed the worktree %s of %s", path, x.Thread)
	tp.wmu.Lock() // a sweep that runs stores its own list
	v := slices.DeleteFunc(tp.View(), func(t Temp) bool { return t.Path == path })
	tp.pub.Store(&v)
	tp.wmu.Unlock()
	return nil
}

func size(p string) uint64 {
	var n uint64
	filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if i, err := d.Info(); err == nil {
				n += uint64(i.Size())
			}
		}
		return nil
	})
	return n
}

func mb(n uint64) string { return fmt.Sprintf("%.0f MB", float64(n)/(1<<20)) }

// Cmdlines is every process's command line from /proc.
func Cmdlines(proc string) []string {
	if proc == "" {
		proc = "/proc"
	}
	es, _ := os.ReadDir(proc)
	var out []string
	for _, e := range es {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if a := readArgv(proc, pid); len(a) > 0 {
			out = append(out, strings.Join(a, " "))
		}
	}
	return out
}
