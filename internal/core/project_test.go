package core

import "testing"

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

// SPEC 0.5 V0.55: a thread lands in the project whose folder is its folder
// or contains it; the longest folder wins, ties by id; a folder never
// covers a sibling with the same prefix.
func TestMatchFolder(t *testing.T) {
	ps := []*Project{
		{ID: "p_home", Folder: "/home/me"},
		{ID: "p_fyr", Folder: "/home/me/git/fyr"},
		{ID: "p_b", Folder: "/home/me/git/fyr/web/"},
		{ID: "p_a", Folder: "/home/me/git/fyr/web"},
		{ID: "p_label"},
	}
	cases := map[string]string{
		"/home/me/git/fyr":         "p_fyr",
		"/home/me/git/fyr/":        "p_fyr",
		"/home/me/git/fyr/cmd":     "p_fyr",
		"/home/me/git/fyr/web/src": "p_a",
		"/home/me/git/fyr-projcut": "p_home",
		"/home/me":                 "p_home",
		"/tmp/x":                   "",
		"":                         "",
	}
	for dir, want := range cases {
		if got := MatchFolder(ps, dir); got != want {
			t.Errorf("%q: got %q want %q", dir, got, want)
		}
	}
	if got := MatchFolder([]*Project{{ID: "p_root", Folder: "/"}}, "/srv/x"); got != "p_root" {
		t.Errorf("root folder: %q", got)
	}
}
