package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A git command through the default Runner must not prompt for credentials or
// print in the user's locale; every command builds with CGO off.
func TestExecRunnerGitEnv(t *testing.T) {
	out, err := execRunner(context.Background(), t.TempDir(), "git", "-c", "alias.e=!env", "e")
	if err != nil {
		t.Skip("git unavailable: ", err)
	}
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "CGO_ENABLED=0"} {
		if !strings.Contains(string(out), want+"\n") {
			t.Errorf("git env lacks %s", want)
		}
	}
	out, _ = execRunner(context.Background(), t.TempDir(), "env")
	if !strings.Contains(string(out), "CGO_ENABLED=0\n") || strings.Contains(string(out), "GIT_TERMINAL_PROMPT") {
		t.Errorf("non-git env: %s", out)
	}
}

func TestReplaceDir(t *testing.T) {
	root := t.TempDir()
	mk := func(name, file string) string {
		d := filepath.Join(root, name)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, file), nil, 0o644)
		return d
	}
	out := mk("out", "old")
	tmp := mk("tmp", "new")
	if err := replaceDir(tmp, out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "new")); err != nil {
		t.Fatal("new build not in place")
	}
	if ents, _ := os.ReadDir(root); len(ents) != 1 {
		t.Fatalf("leftovers: %v", ents)
	}
	// No build at out yet.
	os.RemoveAll(out)
	tmp = mk("tmp2", "x")
	if err := replaceDir(tmp, out); err != nil {
		t.Fatal(err)
	}
	// A failing swap puts the old build back.
	if err := replaceDir(filepath.Join(root, "missing"), out); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(filepath.Join(out, "x")); err != nil {
		t.Fatal("old build not restored")
	}
}
