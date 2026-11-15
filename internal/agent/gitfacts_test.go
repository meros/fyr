package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

// review 2026-11-15: gitFacts reads with optional locks off, as gitdiff
// does, so `git status` after a run never takes the index lock while the
// agent runs git itself.
func TestGitFactsNoOptionalLocks(t *testing.T) {
	real, err := exec.LookPath("git")
	if err != nil {
		t.Skip("no git")
	}
	sh, err := exec.LookPath("sh") // the nix build sandbox has no /usr/bin/env
	if err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	log := filepath.Join(dir, "env.log")
	os.MkdirAll(bin, 0o755)
	script := "#!" + sh + "\necho \"${GIT_OPTIONAL_LOCKS:-unset} $*\" >> " + log + "\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "work")
	if out, err := exec.Command(real, "init", "-q", "-b", "main", work).CombinedOutput(); err != nil {
		t.Skipf("git init: %v %s", err, out)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	gitFacts(work)
	b, err := os.ReadFile(log)
	if err != nil || len(b) == 0 {
		t.Fatalf("gitFacts ran no git: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if !strings.HasPrefix(line, "0 ") {
			t.Errorf("git without GIT_OPTIONAL_LOCKS=0: %s", line)
		}
	}
}
