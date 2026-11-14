package core

import "testing"

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
	if DefaultColor("acme") != DefaultColor("acme") {
		t.Fatal("not stable")
	}
	seen := map[string]bool{}
	for _, n := range []string{"acme", "wayout", "nixos", "fyr", "recept", "utdelningar", "a", "b", "c", "d", "e", "f"} {
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
	cases := map[string]string{"acme": "A", "my-app": "MA", "fyr_web": "FW", "åsa": "Å", "": "?"}
	for in, want := range cases {
		if got := Monogram(in); got != want {
			t.Errorf("%q: got %q want %q", in, got, want)
		}
	}
}

// SPEC 0.5 V0.42: the main folder is the one marked main on the host, else
// the first folder there; SetMain moves the mark within that host only.
func TestMainFolder(t *testing.T) {
	p := Project{Sources: []Source{
		{Kind: "repo", URL: "github.com/me/app"},
		{Kind: "folder", Host: "b", Path: "/b/app"},
		{Kind: "folder", Host: "a", Path: "/a/one"},
		{Kind: "folder", Host: "a", Path: "/a/two"},
	}}
	if p.Folder("a") != "/a/one" || p.Folder("b") != "/b/app" || p.Folder("c") != "" || p.MainIndex("c") != -1 {
		t.Fatalf("default main: %q %q", p.Folder("a"), p.Folder("b"))
	}
	orig := p.Sources
	if !p.SetMain(3) || p.Folder("a") != "/a/two" || p.Folder("b") != "/b/app" || orig[3].Main {
		t.Fatalf("set main: %+v (original changed: %v)", p.Sources, orig[3].Main)
	}
	if !p.SetMain(2) || p.Sources[3].Main || !p.Sources[2].Main {
		t.Fatalf("move main: %+v", p.Sources)
	}
	if p.SetMain(0) || p.SetMain(4) || p.SetMain(-1) {
		t.Fatal("a repo or a bad index became main")
	}
}
