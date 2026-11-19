package engine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// V0.change-worktree: change_open {workspace: true} makes a worktree per
// change, so one thread runs two changes without sharing a working tree;
// the same key finds the same worktree; after starts the branch from the
// change it builds on; the change carries the workspace with MadeByFyr.
func TestChangeOpenMakesAWorktreePerChange(t *testing.T) {
	h := newHarness(t, time.Minute)
	repo := filepath.Join(t.TempDir(), "repo")
	git := func(d string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	os.MkdirAll(repo, 0o755)
	git(repo, "init", "-q", "-b", "main")
	git(repo, "commit", "-q", "--allow-empty", "-m", "base")
	r := h.cmd("thread.create", "", core.Args{"cwd": filepath.Dir(repo), "text": "x"})
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	id := r.ThreadID
	h.wait(id, "done", state(core.Done))
	call := func(a map[string]any) (string, bool) { return callAs(t, h.e, id, "change_open", a) }

	if out, isErr := call(map[string]any{"title": "A", "workspace": true}); !isErr || !strings.Contains(out, "branch") {
		t.Fatalf("no branch: %s", out)
	}
	if out, isErr := call(map[string]any{"title": "A", "branch": "feat/a", "workspace": true}); !isErr || !strings.Contains(out, "not in a git repo") {
		t.Fatalf("the thread's folder holds repos but is not one: %s", out)
	}
	out, isErr := call(map[string]any{"title": "A", "branch": "feat/a", "repo": "repo", "workspace": true})
	pathA := filepath.Join(repo, ".claude", "worktrees", "feat-a")
	if isErr || !strings.Contains(out, "Opened change") || !strings.Contains(out, pathA) {
		t.Fatalf("open A: %s", out)
	}
	a := changeOf(h, id, "feat/a")
	if a == nil || a.Workspace == nil || a.Workspace.Path != pathA || !a.Workspace.MadeByFyr || a.Branch != "feat/a" || len(a.Targets) != 1 {
		t.Fatalf("change A: %+v", a)
	}
	git(pathA, "commit", "-q", "--allow-empty", "-m", "a1")

	if out, _ := call(map[string]any{"title": "A", "branch": "feat/a", "repo": "repo", "workspace": true}); !strings.Contains(out, "Found") || !strings.Contains(out, pathA) {
		t.Fatalf("again: %s", out)
	}
	out, isErr = call(map[string]any{"key": "b", "title": "B", "branch": "feat/b", "repo": repo, "after": []any{"feat/a"}, "workspace": true})
	pathB := filepath.Join(repo, ".claude", "worktrees", "feat-b")
	if isErr || !strings.Contains(out, pathB) || !strings.Contains(out, "from feat/a") {
		t.Fatalf("open B: %s", out)
	}
	if git(pathB, "rev-parse", "HEAD") != git(pathA, "rev-parse", "HEAD") {
		t.Fatal("B did not start from A's branch")
	}
	if b := changeOf(h, id, "b"); b == nil || b.Workspace == nil || b.Workspace.Path != pathB || len(b.After) != 1 {
		t.Fatalf("change B: %+v", b)
	}
	if out, isErr := call(map[string]any{"key": "c", "title": "C", "branch": "feat/c", "repo": repo, "after": []any{"nope"}, "workspace": true}); !isErr {
		t.Fatalf("after an unknown change: %s", out)
	}
}

// V0.change-worktree (3): a worktree that went (V0.worktree-sweep) is made
// again on the same key, and the change points at it; a kind that is not
// code makes no worktree.
func TestChangeOpenMakesAGoneWorktreeAgain(t *testing.T) {
	h := newHarness(t, time.Minute)
	repo := filepath.Join(t.TempDir(), "repo")
	git := func(d string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	os.MkdirAll(repo, 0o755)
	git(repo, "init", "-q", "-b", "main")
	git(repo, "commit", "-q", "--allow-empty", "-m", "base")
	r := h.cmd("thread.create", "", core.Args{"cwd": repo, "text": "x"})
	id := r.ThreadID
	h.wait(id, "done", state(core.Done))
	call := func(a map[string]any) (string, bool) { return callAs(t, h.e, id, "change_open", a) }
	if out, isErr := call(map[string]any{"title": "I", "kind": "infra", "branch": "feat/i", "workspace": true}); !isErr {
		t.Fatalf("infra: %s", out)
	}
	if _, err := os.Stat(filepath.Join(repo, ".claude", "worktrees", "feat-i")); err == nil {
		t.Fatal("a refused change made a worktree")
	}
	path := filepath.Join(repo, ".claude", "worktrees", "feat-a")
	if out, isErr := call(map[string]any{"title": "A", "branch": "feat/a", "workspace": true}); isErr {
		t.Fatalf("open: %s", out)
	}
	git(repo, "worktree", "remove", path)
	if out, isErr := call(map[string]any{"title": "A", "branch": "feat/a", "workspace": true}); isErr || !strings.Contains(out, "Fyr made its worktree at "+path) {
		t.Fatalf("again: %s", out)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("not made again")
	}
	if c := changeOf(h, id, "feat/a"); c.Workspace == nil || c.Workspace.Path != path || !c.Workspace.MadeByFyr {
		t.Fatalf("change: %+v", c.Workspace)
	}
}
