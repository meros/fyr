package gitdiff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// repo makes a repository on main with a few files and one commit.
func repo(t *testing.T, branch string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", branch)
	write(t, dir, "keep.txt", "a\nb\nc\n")
	write(t, dir, "edit.txt", "one\ntwo\nthree\n")
	write(t, dir, "gone.txt", "bye\n")
	write(t, dir, "old/name.txt", "line 1\nline 2\nline 3\nline 4\nline 5\n")
	write(t, dir, ".gitignore", "*.log\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "base")
	return dir
}

func byPath(c Changes) map[string]File {
	m := map[string]File{}
	for _, f := range c.Files {
		m[f.Path] = f
	}
	return m
}

// SPEC 0.5 V0.25: a folder outside git has no changes view.
func TestNotGit(t *testing.T) {
	c, err := Summary(context.Background(), t.TempDir())
	if err != nil || c.Git {
		t.Fatalf("%+v %v", c, err)
	}
	c, err = Summary(context.Background(), filepath.Join(t.TempDir(), "missing"))
	if err != nil || c.Git {
		t.Fatalf("missing folder: %+v %v", c, err)
	}
}

// SPEC 0.5 V0.25: merge base to working tree: commits, staged, unstaged,
// untracked (as added), deleted, renamed, binary; ignored files stay out.
func TestBranchDiffAgainstMain(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "committed.txt", "x\ny\n")
	git(t, dir, "add", "committed.txt")
	git(t, dir, "commit", "-q", "-m", "feature work")
	os.MkdirAll(filepath.Join(dir, "new"), 0o755)
	git(t, dir, "mv", "old/name.txt", "new/name.txt")
	write(t, dir, "staged.txt", "s\n")
	git(t, dir, "add", "staged.txt")
	write(t, dir, "edit.txt", "one\nTWO\nthree\nfour\n")
	os.Remove(filepath.Join(dir, "gone.txt"))
	write(t, dir, "untracked/new.txt", "u1\nu2\nu3\n")
	write(t, dir, "debug.log", "ignored\n")
	os.WriteFile(filepath.Join(dir, "img.bin"), []byte{0, 1, 2, 0, 255}, 0o644)

	// From a subfolder: paths stay relative to the repository root.
	os.MkdirAll(filepath.Join(dir, "untracked"), 0o755)
	c, err := Summary(context.Background(), filepath.Join(dir, "untracked"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Git || c.Branch != "feature" || c.Base != "main" || c.MergeBase == "" || c.Commits != 1 || c.Truncated {
		t.Fatalf("%+v", c)
	}
	root, _ := filepath.EvalSymlinks(dir)
	if c.Root != root {
		t.Fatalf("root %q, want %q", c.Root, root)
	}
	f := byPath(c)
	want := map[string]File{
		"committed.txt":     {Path: "committed.txt", Status: "added", Additions: 2},
		"staged.txt":        {Path: "staged.txt", Status: "added", Additions: 1},
		"edit.txt":          {Path: "edit.txt", Status: "modified", Additions: 2, Deletions: 1},
		"gone.txt":          {Path: "gone.txt", Status: "deleted", Deletions: 1},
		"new/name.txt":      {Path: "new/name.txt", OldPath: "old/name.txt", Status: "renamed"},
		"untracked/new.txt": {Path: "untracked/new.txt", Status: "added", Additions: 3},
		"img.bin":           {Path: "img.bin", Status: "added", Binary: true},
	}
	if len(f) != len(want) {
		t.Fatalf("files %+v", c.Files)
	}
	for k, w := range want {
		got := f[k]
		if got.Status != "deleted" && got.Blob == "" {
			t.Errorf("%s: no blob id (V0.65)", k)
		}
		got.Blob = ""
		if got != w {
			t.Errorf("%s: got %+v want %+v", k, got, w)
		}
	}
	for i := 1; i < len(c.Files); i++ {
		if c.Files[i-1].Path > c.Files[i].Path {
			t.Fatalf("not sorted: %+v", c.Files)
		}
	}
}

// SPEC 0.5 V0.25: origin/HEAD is the base when there is one.
func TestBaseIsOriginHead(t *testing.T) {
	up := repo(t, "trunk")
	dir := filepath.Join(t.TempDir(), "clone")
	git(t, filepath.Dir(dir), "clone", "-q", up, dir)
	git(t, dir, "checkout", "-q", "-b", "work")
	write(t, dir, "keep.txt", "a\nb\nc\nd\n")
	c, err := Summary(context.Background(), dir)
	if err != nil || c.Base != "origin/trunk" || len(c.Files) != 1 || c.Files[0].Additions != 1 {
		t.Fatalf("%+v %v", c, err)
	}
}

// SPEC 0.5 V0.25: without a base the diff is the uncommitted work; a
// repository without commits diffs against the empty tree.
func TestNoBaseAndNoCommits(t *testing.T) {
	dir := repo(t, "trunk")
	write(t, dir, "keep.txt", "a\n")
	c, err := Summary(context.Background(), dir)
	if err != nil || c.Base != "" || c.Commits != 0 || len(c.Files) != 1 || c.Files[0].Deletions != 2 {
		t.Fatalf("%+v %v", c, err)
	}

	empty := t.TempDir()
	git(t, empty, "init", "-q", "-b", "main")
	write(t, empty, "first.txt", "hi\n")
	c, err = Summary(context.Background(), empty)
	if err != nil || !c.Git || c.MergeBase != "" || len(c.Files) != 1 || c.Files[0].Status != "added" {
		t.Fatalf("empty repo: %+v %v", c, err)
	}
}

// SPEC 0.5 V0.25: at most maxFiles files, then truncated.
func TestTooManyFiles(t *testing.T) {
	dir := repo(t, "main")
	old := maxFiles
	maxFiles = 3
	t.Cleanup(func() { maxFiles = old })
	for _, n := range []string{"a", "b", "c", "d", "e"} {
		write(t, dir, n+".txt", n+"\n")
	}
	c, err := Summary(context.Background(), dir)
	if err != nil || len(c.Files) != 3 || !c.Truncated {
		t.Fatalf("%+v %v", c, err)
	}
}

// SPEC 0.5 V0.25: one file's unified diff, for tracked, renamed, untracked
// and binary files; nothing outside the repository or ignored is read.
func TestFilePatch(t *testing.T) {
	dir := repo(t, "main")
	git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "edit.txt", "one\nTWO\nthree\n")
	os.MkdirAll(filepath.Join(dir, "new"), 0o755)
	git(t, dir, "mv", "old/name.txt", "new/name.txt")
	write(t, dir, "new/name.txt", "line 1\nline 2\nline 3\nline 4\nline five\n")
	write(t, dir, "fresh.txt", "hello\n")
	write(t, dir, "secret.log", "ignored\n")
	os.WriteFile(filepath.Join(dir, "img.bin"), []byte{0, 1, 2, 0, 255}, 0o644)
	outside := filepath.Join(filepath.Dir(dir), "outside.txt")
	os.WriteFile(outside, []byte("do not read\n"), 0o644)
	ctx := context.Background()

	p, err := FilePatch(ctx, dir, "edit.txt")
	if err != nil || !strings.Contains(p.Patch, "-two\n+TWO\n") || p.Binary || p.Truncated {
		t.Fatalf("modified: %+v %v", p, err)
	}
	p, _ = FilePatch(ctx, dir, "new/name.txt")
	if !strings.Contains(p.Patch, "rename from old/name.txt") || !strings.Contains(p.Patch, "+line five") {
		t.Fatalf("renamed: %q", p.Patch)
	}
	p, _ = FilePatch(ctx, dir, "fresh.txt")
	if !strings.Contains(p.Patch, "+hello") || !strings.Contains(p.Patch, "new file") {
		t.Fatalf("untracked: %q", p.Patch)
	}
	p, _ = FilePatch(ctx, dir, "img.bin")
	if !p.Binary {
		t.Fatalf("binary: %+v", p)
	}
	for _, bad := range []string{"../outside.txt", outside, "secret.log", "keep.txt", ":(glob)*", "*.txt"} {
		p, err = FilePatch(ctx, dir, bad)
		if err != nil || p.Patch != "" {
			t.Fatalf("%s: want empty patch, got %q %v", bad, p.Patch, err)
		}
	}
}

// SPEC 0.5 V0.25: a long patch is cut at a line end.
func TestPatchCap(t *testing.T) {
	dir := repo(t, "main")
	old := maxPatch
	maxPatch = 200
	t.Cleanup(func() { maxPatch = old })
	write(t, dir, "edit.txt", strings.Repeat("a long changed line\n", 50))
	p, err := FilePatch(context.Background(), dir, "edit.txt")
	if err != nil || !p.Truncated || len(p.Patch) > 200 || !strings.HasSuffix(p.Patch, "\n") {
		t.Fatalf("%d %+v %v", len(p.Patch), p.Truncated, err)
	}
}
