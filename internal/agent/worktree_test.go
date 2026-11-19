package agent

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRepo makes a bare origin with one commit on main and a clone of it.
func gitRepo(t *testing.T) (work string, run func(d string, args ...string) string) {
	t.Helper()
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.git")
	work = filepath.Join(dir, "work")
	run = func(d string, args ...string) string {
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
	run(dir, "init", "-q", "--bare", "-b", "main", origin)
	run(dir, "clone", "-q", origin, work)
	run(work, "commit", "-q", "--allow-empty", "-m", "base")
	run(work, "push", "-q", "origin", "main")
	run(work, "remote", "set-head", "origin", "main")
	return work, run
}

// V0.change-worktree (1), (2): a new branch goes in its own worktree under
// the main checkout, from origin's default branch with no upstream; the
// main checkout stays clean; a call from inside a worktree still puts it
// under the main checkout; a stacked change starts from its base's branch;
// a branch already checked out is returned as it is, not Fyr's.
func TestMakeWorktree(t *testing.T) {
	work, run := gitRepo(t)
	ctx := context.Background()
	run(work, "commit", "-q", "--allow-empty", "-m", "local only") // main is ahead of origin/main
	ws, err := MakeWorktree(ctx, WorktreeReq{Dir: work, Branch: "feat/a"})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(work, ".claude", "worktrees", "feat-a")
	if ws.Path != want || ws.Branch != "feat/a" || !ws.MadeByFyr || ws.Base != "origin/main" || !strings.HasSuffix(ws.Repo, "/origin") {
		t.Fatalf("worktree: %+v", ws)
	}
	if run(ws.Path, "branch", "--show-current") != "feat/a" {
		t.Fatal("not on feat/a")
	}
	if run(ws.Path, "rev-parse", "HEAD") != run(work, "rev-parse", "origin/main") {
		t.Fatal("feat/a did not start from origin/main")
	}
	if up := exec.Command("git", "-C", ws.Path, "rev-parse", "--abbrev-ref", "@{upstream}"); up.Run() == nil {
		t.Fatal("feat/a tracks a branch: a plain push would go there")
	}
	if st := run(work, "status", "--porcelain"); st != "" {
		t.Fatalf("main checkout is dirty: %q", st)
	}

	run(ws.Path, "commit", "-q", "--allow-empty", "-m", "a1")
	b, err := MakeWorktree(ctx, WorktreeReq{Dir: ws.Path, Branch: "feat/b", From: "feat/a"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Path != filepath.Join(work, ".claude", "worktrees", "feat-b") || b.Base != "feat/a" {
		t.Fatalf("stacked: %+v", b)
	}
	if run(b.Path, "rev-parse", "HEAD") != run(ws.Path, "rev-parse", "HEAD") {
		t.Fatal("feat/b did not start from feat/a")
	}

	again, err := MakeWorktree(ctx, WorktreeReq{Dir: work, Branch: "feat/a"})
	if err != nil || again.Path != ws.Path || again.MadeByFyr {
		t.Fatalf("checked out already: %+v %v", again, err)
	}
	if mine, err := MakeWorktree(ctx, WorktreeReq{Dir: work, Branch: "main"}); err != nil || mine.Path != work || mine.MadeByFyr {
		t.Fatalf("main: %+v %v", mine, err)
	}

	run(work, "branch", "feat/c")
	c, err := MakeWorktree(ctx, WorktreeReq{Dir: work, Branch: "feat/c"})
	if err != nil || !c.MadeByFyr || run(c.Path, "branch", "--show-current") != "feat/c" {
		t.Fatalf("existing branch: %+v %v", c, err)
	}

	if _, err := MakeWorktree(ctx, WorktreeReq{Dir: work, Branch: "bad..name"}); err == nil {
		t.Fatal("a bad branch name was accepted")
	}
	if _, err := MakeWorktree(ctx, WorktreeReq{Dir: work, Branch: "feat/d", From: "nope"}); err == nil {
		t.Fatal("an unknown base was accepted")
	}
	if _, err := MakeWorktree(ctx, WorktreeReq{Dir: t.TempDir(), Branch: "feat/e"}); err == nil || !strings.Contains(err.Error(), "not in a git repo") {
		t.Fatalf("outside git: %v", err)
	}
}
