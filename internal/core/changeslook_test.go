package core

import (
	"errors"
	"strings"
	"testing"
)

const (
	blobA = "1111111111111111111111111111111111111111"
	blobB = "2222222222222222222222222222222222222222"
	head1 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

// SPEC 0.5 V0.65: the look goes into the owner's View, not the thread.
func TestChangesSeenGoesToTheView(t *testing.T) {
	th, v := &Thread{}, &View{}
	if _, err := apply(t, "thread.changesSeen", th, v, Args{"head": head1, "files": map[string]any{"a.go": blobA, "gone.go": ""}}, at(3)); err != nil {
		t.Fatal(err)
	}
	l := v.ChangesLook
	if l == nil || l.Head != head1 || l.Files["a.go"] != blobA || l.Files["gone.go"] != "" || len(l.Files) != 2 || !l.At.Equal(at(3)) {
		t.Fatalf("%+v", l)
	}
	if len(th.Changes) != 0 {
		t.Fatal("the thread changed")
	}
}

// SPEC 0.5 V0.65: a Viewed mark holds the blob; it resets by itself when the
// content changes, and a look drops the marks of files no longer listed.
func TestFileViewedResetsOnContentChange(t *testing.T) {
	th, v := &Thread{}, &View{}
	if FileViewed(v, "a.go", blobA) {
		t.Fatal("viewed without a mark")
	}
	if _, err := apply(t, "thread.fileViewed", th, v, Args{"path": "a.go", "blob": blobA, "viewed": true}, at(1)); err != nil {
		t.Fatal(err)
	}
	if !FileViewed(v, "a.go", blobA) {
		t.Fatal("not viewed after the mark")
	}
	if FileViewed(v, "a.go", blobB) {
		t.Fatal("the mark must reset when the content changes")
	}
	apply(t, "thread.fileViewed", th, v, Args{"path": "b.go", "blob": "", "viewed": true}, at(1))
	if !FileViewed(v, "b.go", "") {
		t.Fatal("a deleted file can be marked viewed")
	}
	apply(t, "thread.fileViewed", th, v, Args{"path": "a.go", "blob": blobA, "viewed": false}, at(2))
	if FileViewed(v, "a.go", blobA) {
		t.Fatal("unmarking did not stick")
	}
	apply(t, "thread.fileViewed", th, v, Args{"path": "a.go", "blob": blobA, "viewed": true}, at(2))
	apply(t, "thread.changesSeen", th, v, Args{"head": head1, "files": map[string]any{"a.go": blobA}}, at(3))
	if !FileViewed(v, "a.go", blobA) || FileViewed(v, "b.go", "") {
		t.Fatalf("a look keeps the marks of listed files only: %+v", v.ChangesLook.Viewed)
	}
}

// SPEC 0.5 V0.65: ids are hex, paths are not empty, at most 1,000 files.
func TestChangesLookBadArgs(t *testing.T) {
	th := &Thread{}
	big := map[string]any{}
	for i := 0; i < 1001; i++ {
		big[strings.Repeat("x", 1+i%7)+string(rune('a'+i%26))+strings.Repeat("y", i/26)] = blobA
	}
	for name, a := range map[string]Args{
		"head not hex":  {"head": "HEAD", "files": map[string]any{}},
		"blob not hex":  {"head": head1, "files": map[string]any{"a": "main"}},
		"empty path":    {"head": head1, "files": map[string]any{"": blobA}},
		"files missing": {"head": head1},
		"too many":      {"head": head1, "files": big},
	} {
		if _, err := apply(t, "thread.changesSeen", th, &View{}, a, at(1)); !errors.Is(err, ErrBadArgs) {
			t.Errorf("%s: want ErrBadArgs, got %v", name, err)
		}
	}
	for name, a := range map[string]Args{
		"no path":     {"blob": blobA, "viewed": true},
		"bad blob":    {"path": "a", "blob": "x y", "viewed": true},
		"viewed type": {"path": "a", "blob": blobA, "viewed": "yes"},
	} {
		if _, err := apply(t, "thread.fileViewed", th, &View{}, a, at(1)); !errors.Is(err, ErrBadArgs) {
			t.Errorf("%s: want ErrBadArgs, got %v", name, err)
		}
	}
}
