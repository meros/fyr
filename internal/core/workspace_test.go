package core

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// SPEC 0.5 V0.28: matching is one function; the longest folder cover wins,
// any folder before a repo, siblings and other hosts do not match, and the
// home folder has no rule of its own.
func TestMatchProject(t *testing.T) {
	f := func(path string) []Source { return []Source{{Kind: "folder", Host: "h1", Path: path}} }
	ps := []Project{
		{ID: "acme", Sources: f("/home/m/locked/acme")},
		{ID: "web", Sources: f("/home/m/locked/acme/web")},
		{ID: "nix", Sources: append(f("/home/m/nixos"), Source{Kind: "folder", Host: "h2", Path: "/home/m/elsewhere"})},
		{ID: "fyr", Sources: []Source{{Kind: "repo", URL: "https://github.com/meros/fyr.git"}}},
	}
	fr := func(p string) []Ref { return []Ref{FolderRef("h1", p)} }
	cases := []struct {
		name string
		refs []Ref
		want string
	}{
		{"below a root", fr("/home/m/locked/acme/api"), "acme"},
		{"longest root", fr("/home/m/locked/acme/web"), "web"},
		{"worktree below", fr("/home/m/locked/acme/web/.claude/worktrees/fix"), "web"},
		{"exact", fr("/home/m/nixos"), "nix"},
		{"sibling prefix", fr("/home/m/nixos-old"), ""},
		{"home folder", fr("/home/m"), ""},
		{"another host", fr("/home/m/elsewhere"), ""},
		{"other host's own folder", []Ref{FolderRef("h2", "/home/m/elsewhere/x")}, "nix"},
		{"repo on any host", []Ref{FolderRef("h9", "/tmp/clone"), RepoRef("git@github.com:meros/fyr.git")}, "fyr"},
		{"folder before repo", []Ref{FolderRef("h1", "/home/m/nixos/x"), RepoRef("github.com/meros/fyr")}, "nix"},
		{"scratch: no refs", []Ref{}, ""},
	}
	for _, c := range cases {
		if got := MatchProject(ps, c.refs); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
	home := []Project{{ID: "home", Sources: f("/home/m")}}
	if got := MatchProject(home, fr("/home/m/notes")); got != "home" {
		t.Errorf("a project with the home folder must match below it: %q", got)
	}
	tie := []Project{{ID: "b", Sources: f("/x")}, {ID: "a", Sources: f("/x")}}
	if got := MatchProject(tie, fr("/x/y")); got != "a" {
		t.Errorf("ties go by id: %q", got)
	}
}

// fakeGit answers like git for a few known folders.
func fakeGit(repos map[string][3]string) Git {
	return func(dir string, args ...string) (string, error) {
		r, ok := repos[dir]
		if !ok {
			return "", errors.New("not a git repository")
		}
		switch strings.Join(args, " ") {
		case "rev-parse --path-format=absolute --git-common-dir":
			return r[0], nil
		case "remote get-url origin":
			if r[1] == "" {
				return "", errors.New("no origin")
			}
			return r[1], nil
		case "rev-parse --abbrev-ref HEAD":
			return r[2], nil
		}
		return "", errors.New("unexpected")
	}
}

// SPEC 0.5 V0.28: providers identify a folder in order: scratch (no refs),
// clone (main root, own folder, origin), folder (its own ref).
func TestIdentifyProviders(t *testing.T) {
	git := fakeGit(map[string][3]string{
		"/src/fyr":            {"/src/fyr/.git", "git@github.com:meros/fyr.git", "main"},
		"/src/fyr/web":        {"/src/fyr/.git", "git@github.com:meros/fyr.git", "main"},
		"/src/fyr-workspaces": {"/src/fyr/.git", "git@github.com:meros/fyr.git", "workspaces"},
		"/src/local":          {"/src/local/.git", "", "HEAD"},
		"/s/scratch/2026-x":   {"/s/scratch/2026-x/.git", "git@github.com:meros/x.git", "main"},
	})
	ps := []Provider{ScratchProvider{Roots: []string{"/s/scratch", "/f/scratch/"}}, CloneProvider{Git: git}, FolderProvider{}}
	cases := []struct {
		dir, provider, branch string
		refs                  []Ref
	}{
		{"/s/scratch/2026-x", "scratch", "", []Ref{}},
		{"/f/scratch/a/b", "scratch", "", []Ref{}},
		{"/s/scratchy", "folder", "", []Ref{FolderRef("h", "/s/scratchy")}},
		{"/src/fyr", "clone", "main", []Ref{FolderRef("h", "/src/fyr"), RepoRef("github.com/meros/fyr")}},
		{"/src/fyr/web", "clone", "main", []Ref{FolderRef("h", "/src/fyr"), FolderRef("h", "/src/fyr/web"), RepoRef("github.com/meros/fyr")}},
		{"/src/fyr-workspaces", "clone", "workspaces", []Ref{FolderRef("h", "/src/fyr"), FolderRef("h", "/src/fyr-workspaces"), RepoRef("github.com/meros/fyr")}},
		{"/src/local", "clone", "", []Ref{FolderRef("h", "/src/local")}},
		{"/home/m/", "folder", "", []Ref{FolderRef("h", "/home/m")}},
	}
	for _, c := range cases {
		w := Identify(ps, "h", c.dir)
		if w.Provider != c.provider || w.Branch != c.branch || !reflect.DeepEqual(w.Refs, c.refs) {
			t.Errorf("%s: got %s %q %v", c.dir, w.Provider, w.Branch, w.Refs)
		}
	}
}

// SPEC 0.5 V0.28: a workspace with a repo ref suggests a project named
// after the repository, with its main folder and the repo as sources.
func TestSuggest(t *testing.T) {
	w := Workspace{Refs: []Ref{FolderRef("h", "/src/fyr"), FolderRef("h", "/src/fyr-ws"), RepoRef("github.com/meros/fyr")}}
	got := Suggest(w)
	want := &Suggestion{Name: "fyr", Sources: []Source{{Kind: "folder", Host: "h", Path: "/src/fyr"}, {Kind: "repo", URL: "github.com/meros/fyr"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	if Suggest(Workspace{Refs: []Ref{FolderRef("h", "/src/local")}}) != nil {
		t.Fatal("a folder without a remote got a suggestion")
	}
	if Suggest(Workspace{Refs: []Ref{}}) != nil {
		t.Fatal("a scratch workspace got a suggestion")
	}
}

// SPEC 0.5 V0.28: sources stay in their wire form and convert to refs.
func TestSourceRefRoundTrip(t *testing.T) {
	for _, s := range []Source{{Kind: "folder", Host: "h", Path: "/a/b"}, {Kind: "repo", URL: "github.com/meros/fyr"}} {
		if got := SourceOf(s.Ref()); got != s {
			t.Errorf("%+v -> %+v", s, got)
		}
	}
	p := Project{Sources: []Source{{Kind: "folder", Host: "h", Path: "/w"}}}
	if w, ok := (FolderProvider{}).Prepare(p, "h"); !ok || w.Path != "/w" {
		t.Errorf("prepare: %+v", w)
	}
	if _, ok := (FolderProvider{}).Prepare(p, "other"); ok {
		t.Error("prepare on a host without a folder")
	}
}

// SPEC 0.5 V0.30: what "New project from this thread" starts with. A clone
// with a remote gives the suggestion; a folder gives its name and itself; a
// scratch place, the home folder and / give nothing; a source an existing
// project already has is left out.
func TestDraft(t *testing.T) {
	none := func(Source) bool { return false }
	clone := Workspace{Provider: "clone", Refs: []Ref{FolderRef("h", "/src/fyr"), FolderRef("h", "/src/fyr/web"), RepoRef("git@github.com:meros/fyr.git")}}
	if got, want := Draft(clone, "/home/m", none), Suggest(clone); !reflect.DeepEqual(got, want) {
		t.Fatalf("clone: %+v, want %+v", got, want)
	}
	plain := Workspace{Provider: "folder", Refs: []Ref{FolderRef("h", "/home/m/Garden")}}
	want := &Suggestion{Name: "Garden", Sources: []Source{{Kind: "folder", Host: "h", Path: "/home/m/Garden"}}}
	if got := Draft(plain, "/home/m", none); !reflect.DeepEqual(got, want) {
		t.Fatalf("folder: %+v", got)
	}
	local := Workspace{Provider: "clone", Refs: []Ref{FolderRef("h", "/src/local")}}
	if got := Draft(local, "/home/m", none); got == nil || got.Name != "local" || len(got.Sources) != 1 {
		t.Fatalf("clone without a remote: %+v", got)
	}
	for name, w := range map[string]Workspace{
		"scratch": {Provider: "scratch", Refs: []Ref{}},
		"home":    {Provider: "folder", Refs: []Ref{FolderRef("h", "/home/m")}},
		"root":    {Provider: "folder", Refs: []Ref{FolderRef("h", "/")}},
		"no refs": {Provider: "folder"},
	} {
		if got := Draft(w, "/home/m", none); got != nil {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	// A dotfiles clone in the home folder: the repo, never the home folder.
	dot := Workspace{Provider: "clone", Refs: []Ref{FolderRef("h", "/home/m"), RepoRef("github.com/m/dotfiles")}}
	if got := Draft(dot, "/home/m", none); got == nil || got.Name != "dotfiles" || len(got.Sources) != 1 || got.Sources[0].Kind != "repo" {
		t.Fatalf("dotfiles: %+v", got)
	}
	repoTaken := func(s Source) bool { return s.Kind == "repo" }
	if got := Draft(clone, "/home/m", repoTaken); got == nil || got.Name != "fyr" || len(got.Sources) != 1 || got.Sources[0].Kind != "folder" {
		t.Fatalf("repo taken: %+v", got)
	}
	if got := Draft(plain, "/home/m", func(Source) bool { return true }); got != nil {
		t.Fatalf("all taken: %+v", got)
	}
}
