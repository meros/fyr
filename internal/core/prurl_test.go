package core

import "testing"

func TestParsePR(t *testing.T) {
	repo, n, ok := ParsePR("https://github.com/acme/web/pull/12345")
	if !ok || repo != "acme/web" || n != 12345 {
		t.Fatal(repo, n, ok)
	}
	if _, _, ok := ParsePR("https://github.com/a/b/issues/1"); ok {
		t.Fatal("issue parsed as PR")
	}
}

// review 2026-11-15: a PR URL is the whole value, not a URL found inside
// it, so a value like "--subject=x <url>" never reaches gh as arguments.
func TestParsePRAnchored(t *testing.T) {
	for _, bad := range []string{
		"--subject=x https://github.com/o/r/pull/1",
		"https://github.com/o/r/pull/1 --admin",
		" https://github.com/o/r/pull/1",
		"https://github.com/o/r/pull/1x",
		"xhttps://github.com/o/r/pull/1",
	} {
		if _, _, ok := ParsePR(bad); ok {
			t.Errorf("ParsePR(%q) accepted", bad)
		}
		if _, ok := CanonicalPR(bad); ok {
			t.Errorf("CanonicalPR(%q) accepted", bad)
		}
	}
	for in, want := range map[string]string{
		"https://github.com/o/r/pull/1":                      "https://github.com/o/r/pull/1",
		"https://github.com/o/r/pull/1/":                     "https://github.com/o/r/pull/1",
		"https://github.com/o/r/pull/12/files#diff-abc":      "https://github.com/o/r/pull/12",
		"https://github.com/O.k/r-x_y/pull/3?notification=1": "https://github.com/O.k/r-x_y/pull/3",
	} {
		if got, ok := CanonicalPR(in); !ok || got != want {
			t.Errorf("CanonicalPR(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
}
