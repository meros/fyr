package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// SPEC 0.5 V0.59 (3): the host reads branch, base, commits ahead, unpushed
// commits and changed files after a run.
func TestGitFacts(t *testing.T) {
	dir := t.TempDir()
	origin := filepath.Join(dir, "origin.git")
	work := filepath.Join(dir, "work")
	run := func(d string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v: %v %s", args, err, out)
		}
	}
	run(dir, "init", "-q", "--bare", "-b", "main", origin)
	run(dir, "clone", "-q", origin, work)
	run(work, "commit", "-q", "--allow-empty", "-m", "base")
	run(work, "push", "-q", "origin", "main")
	run(work, "checkout", "-q", "-b", "feat")
	run(work, "commit", "-q", "--allow-empty", "-m", "one")
	run(work, "commit", "-q", "--allow-empty", "-m", "two")
	os.WriteFile(filepath.Join(work, "a.txt"), []byte("x"), 0o644)
	g := gitFacts(work)
	if g.Branch != "feat" || g.Ahead != 2 || g.Unpushed != 2 || g.Dirty != 1 || g.Head == "" || g.Base == "" {
		t.Fatalf("facts before push: %+v", g)
	}
	run(work, "push", "-q", "-u", "origin", "feat")
	if g := gitFacts(work); g.Unpushed != 0 || g.Ahead != 2 {
		t.Fatalf("after push: %+v", g)
	}
	if g := gitFacts(dir); g.Branch != "" {
		t.Fatalf("outside git: %+v", g)
	}
}

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{
		"git@github.com:meros/fyr.git":   "github.com/meros/fyr",
		"https://GitHub.com/meros/fyr/":  "github.com/meros/fyr",
		"ssh://git@github.com/meros/fyr": "github.com/meros/fyr",
	} {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}
