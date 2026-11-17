package cgroup

// V0.176 (3): temporary files threads leave. A layout run's Chrome profile
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
	"os/exec"
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
}

// TempThread is what the sweep needs of a thread.
type TempThread struct {
	ID, SessionID, Cwd string
	Asleep             bool
	LiveCLI            bool
	Branches           []string // its changes' branches
}

// Temps sweeps and lists. Sweep runs on one goroutine; View never waits.
type Temps struct {
	Dir      string // the system temp folder; default os.TempDir()
	Now      func() time.Time
	Threads  func() []TempThread // the threads to look at (asleep ones recently, and the rest)
	Cmdlines func() []string     // every process's command line (a profile in use is named there)
	Logf     func(string, ...any)

	mu  sync.Mutex
	pub atomic.Pointer[[]Temp]
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
// lists the settled threads' worktrees again (git runs: call it seldom).
func (tp *Temps) Sweep(worktrees bool) {
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
	if !worktrees {
		return
	}
	var out []Temp
	repos := map[string][]wtree{}
	tops := map[string]string{} // a folder → its repository, one git call per folder
	for _, th := range threads {
		if !th.Asleep || th.Cwd == "" {
			continue
		}
		top, ok := tops[th.Cwd]
		if !ok {
			top = gitOut(th.Cwd, "rev-parse", "--path-format=absolute", "--git-common-dir")
			tops[th.Cwd] = top
		}
		if top == "" {
			continue
		}
		wts, ok := repos[top]
		if !ok {
			wts = worktreesOf(th.Cwd)
			repos[top] = wts
		}
		for _, w := range wts {
			if w.main || !(w.path == th.Cwd || (w.branch != "" && slices.Contains(th.Branches, w.branch))) {
				continue
			}
			x := Temp{Path: w.path, Kind: "worktree", Thread: th.ID, Bytes: size(w.path)}
			x.Safe, x.Why = worktreeSafe(w.path)
			out = append(out, x)
		}
	}
	tp.pub.Store(&out)
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
	cmd := exec.Command("git", "-C", path, "worktree", "remove", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s", strings.TrimSpace(string(out)))
	}
	tp.logf("leftovers: removed the worktree %s of %s", path, x.Thread)
	tp.mu.Lock()
	v := slices.DeleteFunc(tp.View(), func(t Temp) bool { return t.Path == path })
	tp.pub.Store(&v)
	tp.mu.Unlock()
	return nil
}

type wtree struct {
	path, branch string
	main         bool
}

func gitOut(dir string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func worktreesOf(dir string) []wtree {
	var out []wtree
	for i, blk := range strings.Split(gitOut(dir, "worktree", "list", "--porcelain"), "\n\n") {
		var w wtree
		for _, ln := range strings.Split(blk, "\n") {
			if p, ok := strings.CutPrefix(ln, "worktree "); ok {
				w.path = p
			}
			if b, ok := strings.CutPrefix(ln, "branch refs/heads/"); ok {
				w.branch = b
			}
		}
		w.main = i == 0
		if w.path != "" {
			out = append(out, w)
		}
	}
	return out
}

// worktreeSafe: no uncommitted or untracked file, and HEAD on a remote branch.
func worktreeSafe(path string) (bool, string) {
	st, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	if err != nil {
		return false, "not a git worktree"
	}
	if strings.TrimSpace(string(st)) != "" {
		return false, fmt.Sprintf("%d uncommitted files", len(strings.Split(strings.TrimSpace(string(st)), "\n")))
	}
	if gitOut(path, "branch", "-r", "--contains", "HEAD") == "" {
		n := gitOut(path, "rev-list", "--count", "HEAD", "--not", "--remotes")
		return false, n + " commits not pushed"
	}
	return true, ""
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
