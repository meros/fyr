package agent

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/gitdiff"
)

// The git observer (#35): branch facts of a checkout.

// gitFacts reads the branch facts of a checkout after a run (SPEC 0.5 V0.2,
// V0.59 (3)): branch, base, commits ahead of the base, unpushed commits and
// changed files. Outside git, or on a timeout, only what was read is set.
func gitFacts(dir string) core.GitFacts {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// gitdiff's runner turns optional locks off, so `git status` never takes
	// the index lock while the agent runs git (review 2026-11-15).
	git := func(args ...string) (string, bool) {
		out, err := gitdiff.Output(ctx, dir, args...)
		return strings.TrimSpace(out), err == nil
	}
	var g core.GitFacts
	g.Branch, _ = git("branch", "--show-current")
	if g.Branch == "" {
		return g
	}
	g.Path, _ = git("rev-parse", "--show-toplevel")
	if u, ok := git("remote", "get-url", "origin"); ok {
		g.Repo = NormalizeRemote(u)
	} else {
		g.Repo = g.Path
	}
	g.Head, _ = git("rev-parse", "--short", "HEAD")
	for _, b := range []string{"origin/HEAD", "origin/main", "origin/master"} {
		if mb, ok := git("merge-base", b, "HEAD"); ok && mb != "" {
			g.Base = b
			if n, ok := git("rev-list", "--count", mb+"..HEAD"); ok {
				g.Ahead, _ = strconv.Atoi(n)
			}
			break
		}
	}
	if n, ok := git("rev-list", "--count", "@{upstream}..HEAD"); ok {
		g.Unpushed, _ = strconv.Atoi(n)
	} else {
		g.Unpushed = g.Ahead // no upstream: nothing of the branch is pushed
	}
	if st, ok := git("status", "--porcelain"); ok && st != "" {
		g.Dirty = len(strings.Split(st, "\n"))
	}
	return g
}

// Git is the host's git observer (#35): the facts of a checkout, and how a
// record's branch ended.
type Git struct{}

// Facts are the branch facts of the checkout at dir.
func (Git) Facts(dir string) core.GitFacts { return gitFacts(dir) }

// End reads whether branch in the checkout at dir is gone, and whether it
// is merged into base: its tip, or lastHead when it is gone, is an ancestor
// of base (origin/HEAD, origin/main or origin/master when base is ""). A
// branch Fyr saw no commit of (lastHead "") is never merged. known is false
// when dir is not a git checkout.
func (Git) End(dir, branch, base, lastHead string) (gone, merged, known bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	git := func(args ...string) (string, bool) {
		out, err := gitdiff.Output(ctx, dir, args...)
		return strings.TrimSpace(out), err == nil
	}
	if _, ok := git("rev-parse", "--git-dir"); !ok {
		return false, false, false
	}
	tip, _ := git("rev-parse", "--verify", "--quiet", "refs/heads/"+branch)
	gone = tip == ""
	if lastHead == "" {
		return gone, false, true
	}
	commit := tip
	if gone {
		commit = lastHead
	}
	bases := []string{base}
	if base == "" {
		bases = []string{"origin/HEAD", "origin/main", "origin/master"}
	}
	for _, b := range bases {
		if _, ok := git("merge-base", "--is-ancestor", commit, b); ok {
			return gone, true, true
		}
	}
	return gone, false, true
}

// NormalizeRemote turns a git remote into host/owner/name: git@github.com:o/r.git
// and https://github.com/o/r are both github.com/o/r.
func NormalizeRemote(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		u = strings.TrimPrefix(u, p)
	}
	if i := strings.Index(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	if i := strings.Index(u, ":"); i >= 0 && !strings.Contains(u[:i], "/") {
		u = u[:i] + "/" + u[i+1:]
	}
	if i := strings.Index(u, "/"); i > 0 {
		u = strings.ToLower(u[:i]) + u[i:]
	}
	return u
}
