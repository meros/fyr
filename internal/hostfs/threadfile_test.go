package hostfs

import (
	"os"
	"path/filepath"
	"testing"
)

// V0.message-links-files: a path is served only from the thread's folders.
func TestThreadFile(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(root, "home")
	repo := filepath.Join(home, "src", "acme")
	tree := filepath.Join(home, "src", "acme-wt")
	tmp := filepath.Join(root, "tmp")
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(repo, "cmd", "main.go"), "package main\n")
	write(filepath.Join(repo, "logo.PNG"), "\x89PNG")
	write(filepath.Join(repo, "blob.bin"), "a\x00b")
	write(filepath.Join(repo, ".env"), "KEY=1\n")
	write(filepath.Join(repo, ".env.example"), "KEY=\n")
	write(filepath.Join(repo, ".git", "config"), "[core]\n")
	write(filepath.Join(tree, "only.go"), "package wt\n")
	write(filepath.Join(tmp, "out.log"), "done\n")
	write(filepath.Join(home, ".ssh", "id_ed25519"), "key\n")
	write(filepath.Join(home, "notes.md"), "# notes\n")
	if err := os.Symlink(filepath.Join(home, "notes.md"), filepath.Join(repo, "leak.md")); err != nil {
		t.Fatal(err)
	}
	dirs, extra := []string{repo, tree}, []string{tmp}

	for _, c := range []struct{ p, abs, kind string }{
		{"cmd/main.go", filepath.Join(repo, "cmd", "main.go"), "text"},
		{"./cmd/main.go", filepath.Join(repo, "cmd", "main.go"), "text"},
		{filepath.Join(repo, "cmd", "main.go"), filepath.Join(repo, "cmd", "main.go"), "text"},
		{"~/src/acme/cmd/main.go", filepath.Join(repo, "cmd", "main.go"), "text"},
		{"only.go", filepath.Join(tree, "only.go"), "text"}, // a worktree's file by its relative path
		{filepath.Join(tmp, "out.log"), filepath.Join(tmp, "out.log"), "text"},
		{"logo.PNG", filepath.Join(repo, "logo.PNG"), "image"},
		{"blob.bin", filepath.Join(repo, "blob.bin"), "other"},
		{".env.example", filepath.Join(repo, ".env.example"), "text"},
	} {
		f, err := ThreadFile(home, dirs, extra, c.p)
		if err != nil || f.Abs != c.abs || f.Kind != c.kind {
			t.Errorf("%s: %+v %v, want %s %s", c.p, f, err, c.abs, c.kind)
		}
	}
	for _, p := range []string{
		"", "nope.go", "cmd", // missing, a folder
		"../../notes.md", filepath.Join(home, "notes.md"), "~/notes.md", // outside the folders
		"~/.ssh/id_ed25519", "/etc/passwd",
		"leak.md",             // a link that leads outside
		".env", ".git/config", // names that hold secrets
		"out.log",                      // the temp folder is no base for a relative path
		"cmd/main.go\x00", "cmd/../..", // not a path of a file
	} {
		if f, err := ThreadFile(home, dirs, extra, p); err == nil {
			t.Errorf("%q is served: %+v", p, f)
		}
	}
	// A thread in the home folder serves nothing from it: only its other folders.
	if f, err := ThreadFile(home, []string{home}, extra, "notes.md"); err == nil {
		t.Errorf("the home folder is served: %+v", f)
	}
	if _, err := ThreadFile(home, []string{home, "/"}, extra, filepath.Join(tmp, "out.log")); err != nil {
		t.Errorf("the temp folder beside a home thread: %v", err)
	}
}
