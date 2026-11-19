package agent

// V0.change-worktree: change_open {workspace: true} makes a worktree of one
// repo on the change's branch (decision 0019), so two changes never share a
// working tree.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/proc"
)

// WorktreeDir is where Fyr puts the worktrees it makes, under the main
// checkout of the repo.
const WorktreeDir = ".claude/worktrees"

// WorktreeReq asks for a worktree of the repo at Dir on Branch. From is the
// branch to start a new branch from (the change it builds on); "" starts it
// from the repo's default branch on origin.
type WorktreeReq struct {
	Dir, Branch, From string
}

var unsafeSlug = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// worktreeName is a folder name for branch: "feat/x" is "feat-x".
func worktreeName(branch string) string {
	s := strings.Trim(unsafeSlug.ReplaceAllString(strings.ReplaceAll(branch, "/", "-"), "-"), "-.")
	if s == "" {
		s = "change"
	}
	return s
}

// MakeWorktree gives the change a worktree of the repo at r.Dir on
// r.Branch. When the branch is already checked out in a worktree, that one
// is returned and Fyr did not make it (MadeByFyr false). Otherwise a new
// worktree goes at <main checkout>/.claude/worktrees/<branch>: on the branch
// when it exists, else on a new branch from r.From or origin's default
// branch, with no upstream so a plain push never goes to the base.
func MakeWorktree(ctx context.Context, r WorktreeReq) (core.Workspace, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	branch := strings.TrimSpace(r.Branch)
	if branch == "" {
		return core.Workspace{}, errors.New("a worktree needs a branch")
	}
	git := func(dir string, args ...string) (string, error) { return runGit(ctx, dir, args...) }
	if _, err := git(r.Dir, "check-ref-format", "--branch", branch); err != nil {
		return core.Workspace{}, fmt.Errorf("%q is not a valid branch name", branch)
	}
	common, err := git(r.Dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return core.Workspace{}, fmt.Errorf("%s is not in a git repo; pass repo: the folder of the repo this change is in", r.Dir)
	}
	if filepath.Base(common) != ".git" {
		return core.Workspace{}, fmt.Errorf("%s is a bare repo; pass repo: a checkout of it", r.Dir)
	}
	main := filepath.Dir(common)
	ws := core.Workspace{Repo: main, Branch: branch}
	if u, err := git(main, "remote", "get-url", "origin"); err == nil && u != "" {
		ws.Repo = NormalizeRemote(u)
	}
	if path := checkedOut(ctx, main, branch); path != "" {
		ws.Path = path
		return ws, nil
	}
	path := filepath.Join(main, WorktreeDir, worktreeName(branch))
	if _, err := os.Stat(path); err == nil {
		return core.Workspace{}, fmt.Errorf("%s already exists and is not a worktree on %s; pick another branch", path, branch)
	}
	exclude(common)
	if _, err := git(main, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		if _, err := git(main, "worktree", "add", path, branch); err != nil {
			return core.Workspace{}, fmt.Errorf("git worktree add: %v", err)
		}
		ws.Path, ws.MadeByFyr = path, true
		ws.Base = r.From
		return ws, nil
	}
	from := r.From
	if from != "" {
		if _, err := git(main, "rev-parse", "--verify", "--quiet", from+"^{commit}"); err != nil {
			return core.Workspace{}, fmt.Errorf("the change this one builds on is on %s, which this repo does not have", from)
		}
	} else {
		from = defaultBase(ctx, main)
	}
	if _, err := git(main, "worktree", "add", "--no-track", "-b", branch, path, from); err != nil {
		return core.Workspace{}, fmt.Errorf("git worktree add: %v", err)
	}
	ws.Path, ws.MadeByFyr, ws.Base = path, true, from
	return ws, nil
}

// checkedOut is the worktree of main's repo that has branch checked out,
// or "".
func checkedOut(ctx context.Context, main, branch string) string {
	out, err := runGit(ctx, main, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	path := ""
	for _, l := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(l, "worktree "); ok {
			path = p
		} else if l == "branch refs/heads/"+branch {
			return path
		}
	}
	return ""
}

// defaultBase fetches origin and returns its default branch (origin/HEAD,
// else origin/main or origin/master), or HEAD for a repo with no origin.
// A failed fetch starts from what was fetched last.
func defaultBase(ctx context.Context, main string) string {
	if _, err := runGit(ctx, main, "remote", "get-url", "origin"); err != nil {
		return "HEAD"
	}
	fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	runGit(fctx, main, "fetch", "--quiet", "origin")
	cancel()
	if d, err := runGit(ctx, main, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && d != "" {
		return d
	}
	for _, b := range []string{"origin/main", "origin/master"} {
		if _, err := runGit(ctx, main, "rev-parse", "--verify", "--quiet", b); err == nil {
			return b
		}
	}
	return "HEAD"
}

// exclude keeps Fyr's worktrees out of the main checkout's git status: it
// adds the folder to the repo's info/exclude once.
func exclude(common string) {
	f := filepath.Join(common, "info", "exclude")
	b, _ := os.ReadFile(f)
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l == "/"+WorktreeDir+"/" || l == "**/"+WorktreeDir+"/" || l == WorktreeDir+"/" {
			return
		}
	}
	os.MkdirAll(filepath.Dir(f), 0o755)
	s := string(b)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	os.WriteFile(f, []byte(s+"/"+WorktreeDir+"/\n"), 0o644)
}

// runGit runs git in dir with no prompt and returns its trimmed stdout; an
// error carries git's own line.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := proc.Command(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errb.String()); msg != "" {
			return "", errors.New(msg)
		}
		return "", err
	}
	return strings.TrimSpace(out.String()), nil
}
