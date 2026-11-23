package cgroup

// V0.worktree-sweep: Fyr removes a worktree nobody works in once its work is
// kept elsewhere, and makes it again when a thread works there again.
//
// A worktree of a repository Fyr knows (a project's folder, a thread's
// folder) goes when all of these hold, checked again just before:
//   - no awake thread works in it (its folder, or one of its changes' branches),
//     and no live process has its working folder inside it;
//   - it is not locked (`git worktree lock` is how to keep one);
//   - nothing is uncommitted or untracked, and every ignored file is in a
//     folder a build makes again (node_modules, dist, ...);
//   - its HEAD is on a remote branch or inside a GitHub PR's head
//     (refs/pull/N/head, fetched into refs/fyr/pull/N; GitHub keeps those
//     also after the branch is deleted or history is rewritten);
//   - nothing moved its HEAD for WorktreeIdle.
//
// V0.change-worktree-cleanup: a worktree Fyr made for a change that landed
// or ended (TempThread.Done) is not in use by that change's branch, and goes
// DoneGrace after the end and after its HEAD last moved.
//
// Before it goes, its HEAD is pinned at refs/fyr/worktrees/<name>-<commit> and its
// branch is kept, so `git worktree add` gives it back; Restore does that when
// a thread starts in a folder that was removed.

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/meros/fyr/internal/proc"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// WorktreeIdle: a worktree whose HEAD moved more recently stays.
const WorktreeIdle = 24 * time.Hour

// DoneGrace: a worktree Fyr made for a change that landed or ended stays
// this long after the end and after its HEAD last moved
// (V0.change-worktree-cleanup).
const DoneGrace = time.Hour

// pullFetchEvery: how often a repository's PR heads are fetched.
const pullFetchEvery = time.Hour

const (
	removedKey = "worktrees-removed"
	fetchedKey = "worktrees-fetched"
)

// Removed is a worktree Fyr removed, as Restore needs it.
type Removed struct {
	Path   string    `json:"path"`
	Common string    `json:"common"` // the repository's git dir
	Branch string    `json:"branch,omitempty"`
	Pin    string    `json:"pin"` // refs/fyr/worktrees/<name>-<commit>
	Head   string    `json:"head"`
	Thread string    `json:"thread,omitempty"`
	At     time.Time `json:"at"`
}

type wtree struct {
	path, branch, head string
	main, locked       bool
}

// Folders a build makes again: an ignored file under one of these goes with
// the worktree; any other ignored file (an .env, notes) keeps it.
var regenerable = map[string]bool{
	"node_modules": true, "dist": true, "build": true, "target": true, "bin": true, "out": true,
	"result": true, ".direnv": true, "__pycache__": true, ".next": true, ".nuxt": true,
	".svelte-kit": true, ".cache": true, ".venv": true, "venv": true, "coverage": true,
	".turbo": true, ".gradle": true, ".pytest_cache": true, ".mypy_cache": true,
	".test-out": true, ".parcel-cache": true, ".tox": true, "vendor": true,
}

func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	// NoOptionalLocks: a look never rewrites the index, so it never counts
	// as work in the worktree. SSHBatch: a fetch never waits for a prompt.
	return proc.GitWith(ctx, dir, proc.GitOpts{NoOptionalLocks: true, SSHBatch: true}, args...)
}

// Bounds of the git calls on a worktree: a repository on a stalled disk or a
// lock another process holds must not hold up a sweep for good.
const (
	gitReadTimeout  = time.Minute     // status, list, update-ref, remove
	gitCheckoutWait = 5 * time.Minute // worktree add checks a tree out
)

// gitRun runs git in dir with a deadline and returns its stdout, or with
// combined its stdout and stderr together.
func gitRun(dir string, timeout time.Duration, combined bool, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := gitCmd(ctx, dir, args...)
	if combined {
		return cmd.CombinedOutput()
	}
	return cmd.Output()
}

func gitOut(dir string, args ...string) string {
	out, err := gitRun(dir, gitReadTimeout, false, args...)
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
			if h, ok := strings.CutPrefix(ln, "HEAD "); ok {
				w.head = h
			}
			if ln == "locked" || strings.HasPrefix(ln, "locked ") {
				w.locked = true
			}
		}
		w.main = i == 0
		if w.path != "" {
			out = append(out, w)
		}
	}
	return out
}

// worktreeSafe: nothing uncommitted, untracked or ignored outside a build
// folder, and HEAD kept on the remote (a branch or a PR's head).
func worktreeSafe(path string) (bool, string) {
	st, err := gitRun(path, gitReadTimeout, false, "status", "--porcelain", "--ignored=matching")
	if err != nil {
		return false, "not a git worktree"
	}
	var dirty int
	var kept []string
	for _, ln := range strings.Split(strings.TrimRight(string(st), "\n"), "\n") {
		if ln == "" {
			continue
		}
		if p, ok := strings.CutPrefix(ln, "!! "); ok {
			if !slices.ContainsFunc(strings.Split(strings.Trim(p, "/"), "/"), func(s string) bool { return regenerable[s] }) &&
				!strings.HasSuffix(p, ".pyc") {
				kept = append(kept, strings.TrimSuffix(p, "/"))
			}
			continue
		}
		dirty++
	}
	if dirty > 0 {
		return false, fmt.Sprintf("%d uncommitted files", dirty)
	}
	if len(kept) > 0 {
		if len(kept) > 2 {
			kept = append(kept[:2], "…")
		}
		return false, "ignored files: " + strings.Join(kept, ", ")
	}
	if gitOut(path, "for-each-ref", "--count=1", "--contains", "HEAD", "--format=%(refname)", "refs/remotes/", "refs/fyr/pull/") == "" {
		n := gitOut(path, "rev-list", "--count", "HEAD", "--not", "--remotes", "--glob=refs/fyr/pull/*")
		return false, n + " commits not pushed"
	}
	return true, ""
}

// lastMove is when the worktree's HEAD last moved (a commit, checkout,
// reset), or it was made. The index is not read: a `git status` rewrites it.
func lastMove(path string) time.Time {
	gd := gitOut(path, "rev-parse", "--path-format=absolute", "--git-dir")
	var t time.Time
	for _, f := range []string{gd, filepath.Join(gd, "HEAD"), filepath.Join(gd, "logs", "HEAD")} {
		if st, err := os.Stat(f); err == nil && st.ModTime().After(t) {
			t = st.ModTime()
		}
	}
	return t
}

func inside(dir, root string) bool {
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

var unsafeRef = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func (tp *Temps) loadFetchedLocked(now time.Time) {
	if tp.fetchedLoaded {
		return
	}
	tp.fetchedLoaded = true
	if tp.KV == nil {
		return
	}
	raw, _ := tp.KV.GetKV(fetchedKey)
	if raw == "" {
		return
	}
	var m map[string]time.Time
	if json.Unmarshal([]byte(raw), &m) != nil {
		return
	}
	if tp.fetched == nil {
		tp.fetched = map[string]time.Time{}
	}
	for k, at := range m {
		if !at.IsZero() && now.Sub(at) < pullFetchEvery {
			tp.fetched[k] = at
		}
	}
}

func (tp *Temps) saveFetchedLocked(now time.Time) {
	if tp.KV == nil {
		return
	}
	out := map[string]time.Time{}
	for k, at := range tp.fetched {
		if !at.IsZero() && now.Sub(at) < pullFetchEvery {
			out[k] = at
		} else {
			delete(tp.fetched, k)
		}
	}
	if len(out) == 0 {
		tp.KV.SetKV(fetchedKey, "")
		return
	}
	if b, err := json.Marshal(out); err == nil {
		tp.KV.SetKV(fetchedKey, string(b))
	}
}

// fetchPulls fetches the PR heads of origin into refs/fyr/pull/*, at most
// once an hour per repository. A remote that is not GitHub has none.
func (tp *Temps) fetchPulls(common, dir string, now time.Time) {
	tp.loadFetchedLocked(now)
	if tp.NoFetch || now.Sub(tp.fetched[common]) < pullFetchEvery {
		return
	}
	if tp.fetched == nil {
		tp.fetched = map[string]time.Time{}
	}
	tp.fetched[common] = now
	tp.saveFetchedLocked(now)
	if gitOut(dir, "remote", "get-url", "origin") == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := gitCmd(ctx, dir, "fetch", "-q", "--no-tags", "--no-write-fetch-head", "origin", "+refs/pull/*/head:refs/fyr/pull/*")
	if out, err := cmd.CombinedOutput(); err != nil {
		tp.logf("worktrees: fetch the PR heads of %s: %v %s", dir, err, strings.TrimSpace(string(out)))
	}
}

// removeWorktree pins HEAD, removes the worktree (git refuses one with
// changes) and remembers how to make it again.
func (tp *Temps) removeWorktree(w wtree, common, thread string, now time.Time) error {
	tp.rmu.Lock()
	defer tp.rmu.Unlock()
	name := unsafeRef.ReplaceAllString(filepath.Base(w.path), "-")
	head := w.head
	if head == "" {
		head = gitOut(w.path, "rev-parse", "HEAD")
	}
	pin := "refs/fyr/worktrees/" + name + "-" + short(head) // two folders of one name never share a pin
	if out, err := gitRun(w.path, gitReadTimeout, true, "update-ref", "-m", "fyr: worktree removed", pin, "HEAD"); err != nil {
		return fmt.Errorf("pin HEAD: %s", strings.TrimSpace(string(out)))
	}
	if out, err := gitRun(w.path, gitReadTimeout, true, "worktree", "remove", w.path); err != nil {
		return fmt.Errorf("git worktree remove: %s", strings.TrimSpace(string(out)))
	}
	rs := tp.removed()
	rs = slices.DeleteFunc(rs, func(r Removed) bool { return r.Path == w.path })
	rs = append(rs, Removed{Path: w.path, Common: common, Branch: w.branch, Pin: pin, Head: head, Thread: thread, At: now})
	tp.saveRemoved(rs)
	return nil
}

func (tp *Temps) removed() []Removed {
	var rs []Removed
	if tp.KV != nil {
		if raw, _ := tp.KV.GetKV(removedKey); raw != "" {
			json.Unmarshal([]byte(raw), &rs)
		}
	}
	return rs
}

func (tp *Temps) saveRemoved(rs []Removed) {
	if tp.KV == nil {
		return
	}
	// A record older than 90 days is dropped; its pin stays in the repository.
	cut := time.Now().Add(-90 * 24 * time.Hour)
	rs = slices.DeleteFunc(rs, func(r Removed) bool { return r.At.Before(cut) })
	b, _ := json.Marshal(rs)
	tp.KV.SetKV(removedKey, string(b))
}

// Restore makes again the worktree Fyr removed that holds dir, on its branch
// (or at its pinned HEAD when the branch is gone or checked out elsewhere).
// It reports whether it made one.
func (tp *Temps) Restore(dir string) (bool, error) {
	tp.rmu.Lock()
	defer tp.rmu.Unlock()
	dir = filepath.Clean(dir)
	rs := tp.removed()
	i := slices.IndexFunc(rs, func(r Removed) bool { return inside(dir, r.Path) })
	if i < 0 {
		return false, nil
	}
	r := rs[i]
	if _, err := os.Stat(r.Path); err == nil {
		return false, nil
	}
	repo := r.Common
	var errs []string
	try := func(args ...string) bool {
		out, err := gitRun(repo, gitCheckoutWait, true, append([]string{"worktree", "add", "-q", r.Path}, args...)...)
		if err != nil {
			errs = append(errs, strings.TrimSpace(string(out)))
		}
		return err == nil
	}
	ok := r.Branch != "" && gitOut(repo, "rev-parse", "--verify", "-q", "refs/heads/"+r.Branch) != "" && try(r.Branch)
	if !ok {
		ok = try("--detach", r.Pin)
	}
	if !ok {
		return false, fmt.Errorf("make the worktree %s again: %s", r.Path, strings.Join(errs, "; "))
	}
	tp.saveRemoved(slices.Delete(rs, i, i+1))
	tp.logf("worktrees: made the worktree %s again", r.Path)
	return true, nil
}

// Cwds is every process's working folder: /proc, else lsof (macOS). Nil when
// neither can be read, and then no worktree is removed on its own.
func Cwds(proc string) []string {
	if proc == "" {
		proc = "/proc"
	}
	if es, err := os.ReadDir(proc); err == nil {
		out := []string{}
		for _, e := range es {
			if e.Name()[0] < '0' || e.Name()[0] > '9' {
				continue
			}
			if d, err := os.Readlink(filepath.Join(proc, e.Name(), "cwd")); err == nil {
				out = append(out, d)
			}
		}
		return out
	}
	b, err := exec.Command("lsof", "-a", "-d", "cwd", "-Fn", "-w").Output()
	if err != nil && len(b) == 0 {
		return nil
	}
	out := []string{}
	for _, ln := range strings.Split(string(b), "\n") {
		if d, ok := strings.CutPrefix(ln, "n"); ok {
			out = append(out, d)
		}
	}
	return out
}
