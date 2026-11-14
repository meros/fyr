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
