package core

import "testing"

// SPEC 0.5 V0.23: the longest containing root wins; siblings do not match.
func TestProjectFor(t *testing.T) {
	f := func(path string) []Source { return []Source{{Kind: "folder", Host: "h1", Path: path}} }
	ps := []Project{
		{ID: "norban", Sources: f("/home/m/locked/norban")},
		{ID: "web", Sources: f("/home/m/locked/norban/web")},
		{ID: "nix", Sources: append(f("/home/m/nixos"), Source{Kind: "folder", Host: "h2", Path: "/home/m/elsewhere"})},
		{ID: "fyr", Sources: []Source{{Kind: "repo", URL: "github.com/meros/fyr"}}},
	}
	cases := map[string]string{
		"/home/m/locked/norban/api":                       "norban",
		"/home/m/locked/norban/web":                       "web",
		"/home/m/locked/norban/web/.claude/worktrees/fix": "web",
		"/home/m/nixos":                                   "nix",
		"/home/m/nixos-old":                               "",
		"/home/m":                                         "",
	}
	for cwd, want := range cases {
		if got := ProjectFor(ps, "h1", cwd, ""); got != want {
			t.Errorf("%s: got %q want %q", cwd, got, want)
		}
	}
	if got := ProjectFor(ps, "h1", "/home/m/elsewhere", ""); got != "" {
		t.Errorf("another host's folder matched: %q", got)
	}
	if got := ProjectFor(ps, "h1", "/tmp/clone-of-fyr", "github.com/meros/fyr"); got != "fyr" {
		t.Errorf("repo source did not match: %q", got)
	}
	if got := ProjectFor(ps, "h1", "/home/m/nixos/x", "github.com/meros/fyr"); got != "nix" {
		t.Errorf("a folder source must win over a repo source: %q", got)
	}
}

// SPEC 0.5 V0.23: remotes normalise to host/owner/repo.
func TestNormalizeRemote(t *testing.T) {
	cases := map[string]string{
		"git@github.com:meros/fyr.git":        "github.com/meros/fyr",
		"https://github.com/meros/fyr":        "github.com/meros/fyr",
		"ssh://git@github.com/Meros/Fyr.git/": "github.com/meros/fyr",
		"git+ssh://git@github.com/meros/fyr":  "github.com/meros/fyr",
		"":                                    "",
	}
	for in, want := range cases {
		if got := NormalizeRemote(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

// SPEC 0.5 V0.23: the default colour is stable for a name and never the beam.
func TestDefaultColor(t *testing.T) {
	if DefaultColor("norban") != DefaultColor("norban") {
		t.Fatal("not stable")
	}
	seen := map[string]bool{}
	for _, n := range []string{"norban", "wayout", "nixos", "fyr", "recept", "utdelningar", "a", "b", "c", "d", "e", "f"} {
		c := DefaultColor(n)
		if !ValidColor(c) || c == "amber" {
			t.Fatalf("%s → %q", n, c)
		}
		seen[c] = true
	}
	if len(seen) < 4 {
		t.Fatalf("palette barely used: %v", seen)
	}
	if ValidColor("amber") || ValidColor("#ff0000") {
		t.Fatal("amber and raw colours must be refused")
	}
}

func TestMonogram(t *testing.T) {
	cases := map[string]string{"norban": "N", "my-app": "MA", "fyr_web": "FW", "åsa": "Å", "": "?"}
	for in, want := range cases {
		if got := Monogram(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}
