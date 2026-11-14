package hostfs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func mk(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func setup(t *testing.T) (*FS, string, string) {
	root := t.TempDir()
	home := filepath.Join(root, "home", "u")
	outside := filepath.Join(root, "etc")
	mk(t, home, outside)
	return &FS{Home: home}, home, outside
}

func names(ds []Dir) []string {
	out := []string{}
	for _, d := range ds {
		out = append(out, d.Name)
	}
	return out
}

// SPEC 0.5 V0.42: every path stays inside the home folder: "..", absolute
// paths elsewhere, a relative path and a symlink that leads out are refused;
// the listing leaves out links that lead out.
func TestConfinement(t *testing.T) {
	f, home, outside := setup(t)
	mk(t, filepath.Join(home, "src", "app"), filepath.Join(home, ".config"))
	os.WriteFile(filepath.Join(home, "notes.txt"), nil, 0o644)
	os.Symlink(outside, filepath.Join(home, "escape"))
	os.Symlink(filepath.Join(home, "src"), filepath.Join(home, "code"))
	for _, p := range []string{"~/..", filepath.Join(home, ".."), "~/src/../../..", outside, "/", "src", "~/escape", "~/escape/x", "~/missing", "~/notes.txt"} {
		if _, err := f.List(p); !errors.Is(err, ErrOutside) {
			t.Errorf("List(%q): %v", p, err)
		}
	}
	l, err := f.List("~")
	if err != nil {
		t.Fatal(err)
	}
	if l.Path != home || l.Parent != "" || !reflect.DeepEqual(names(l.Dirs), []string{".config", "code", "src"}) {
		t.Fatalf("home: %+v", l)
	}
	l, err = f.List("~/code")
	if err != nil || l.Path != filepath.Join(home, "code") || l.Parent != home || !reflect.DeepEqual(names(l.Dirs), []string{"app"}) || l.Dirs[0].Path != filepath.Join(home, "code", "app") {
		t.Fatalf("a link inside home: %+v %v", l, err)
	}
	if _, err := f.Mkdir("~/escape", "x"); !errors.Is(err, ErrOutside) {
		t.Fatalf("mkdir through a link out: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "x")); err == nil {
		t.Fatal("made a folder outside")
	}
}

// SPEC 0.5 V0.42: the scan goes two levels deep, counts a folder with .git
// (a file too: a worktree) and does not descend into it; hidden folders,
// node_modules and symlinks are skipped; roots outside are ignored.
func TestScan(t *testing.T) {
	f, home, outside := setup(t)
	git := filepath.Join(home, "git")
	mk(t,
		filepath.Join(git, "fyr", ".git"),
		filepath.Join(git, "fyr", "vendor", "dep", ".git"), // inside a repo: not found
		filepath.Join(git, "org", "web", ".git"),           // level 2
		filepath.Join(git, "org", "deep", "x", ".git"),     // level 3: too deep
		filepath.Join(git, ".hidden", "r", ".git"),
		filepath.Join(git, "node_modules", "pkg", ".git"),
		filepath.Join(outside, "r", ".git"),
	)
	mk(t, filepath.Join(git, "wt"))
	os.WriteFile(filepath.Join(git, "wt", ".git"), []byte("gitdir: x"), 0o644)
	os.Symlink(filepath.Join(git, "fyr"), filepath.Join(git, "link"))
	got := f.Scan([]string{"~/git", outside, "~/git"})
	want := []string{filepath.Join(git, "fyr"), filepath.Join(git, "org", "web"), filepath.Join(git, "wt")}
	var paths []string
	for _, d := range got {
		paths = append(paths, d.Path)
	}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("scan: %v", paths)
	}
	if r := f.Scan([]string{filepath.Join(git, "fyr")}); len(r) != 1 || r[0].Name != "fyr" {
		t.Fatalf("a root that is a repo: %+v", r)
	}
}

// SPEC 0.5 V0.42: Repos caches the scan of the project folders and the scan
// roots; a stale cache is returned and refreshed in the background.
func TestReposCache(t *testing.T) {
	f, home, _ := setup(t)
	mk(t, filepath.Join(home, "git", "a", ".git"), filepath.Join(home, "proj", ".git"))
	now := time.Unix(0, 0)
	f.Now = func() time.Time { return now }
	f.ScanRoots = []string{filepath.Join(home, "git")}
	f.Folders = func() []string { return []string{filepath.Join(home, "proj")} }
	if r := f.Repos(); !reflect.DeepEqual(names(r), []string{"a", "proj"}) {
		t.Fatalf("first scan: %v", names(r))
	}
	mk(t, filepath.Join(home, "git", "b", ".git"))
	if r := f.Repos(); len(r) != 2 {
		t.Fatalf("not cached: %v", names(r))
	}
	now = now.Add(6 * time.Minute)
	if r := f.Repos(); len(r) != 2 {
		t.Fatalf("stale cache not returned at once: %v", names(r))
	}
	for i := 0; i < 100 && len(f.Repos()) != 3; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if r := f.Repos(); len(r) != 3 {
		t.Fatalf("not refreshed: %v", names(r))
	}
}

// SPEC 0.5 V0.42: a new folder's name has no "/", is not empty and is not
// taken.
func TestMkdir(t *testing.T) {
	f, home, _ := setup(t)
	mk(t, filepath.Join(home, "src", "taken"))
	for _, n := range []string{"", "  ", "a/b", "..", ".", "taken"} {
		if _, err := f.Mkdir("~/src", n); err == nil {
			t.Errorf("Mkdir(%q) worked", n)
		}
	}
	p, err := f.Mkdir("~/src", "nytt")
	if err != nil || p != filepath.Join(home, "src", "nytt") {
		t.Fatalf("%q %v", p, err)
	}
	if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
		t.Fatal("not made")
	}
}

// SPEC 0.5 V0.42: Recent is the distinct existing folders inside home, in
// the order given (newest first).
func TestRecent(t *testing.T) {
	f, home, outside := setup(t)
	a, b := filepath.Join(home, "a"), filepath.Join(home, "b")
	mk(t, a, b, filepath.Join(a, ".git"))
	f.Recent = func() []string { return []string{b, a, b, outside, filepath.Join(home, "gone")} }
	r := f.RecentDirs()
	if !reflect.DeepEqual(names(r), []string{"b", "a"}) || !r[1].Repo || r[0].Repo {
		t.Fatalf("%+v", r)
	}
}
