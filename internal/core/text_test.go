package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// #36: the truncators never cut inside a UTF-8 character, and a cut string
// fits the limit with its "…".
func TestTruncIsRuneSafe(t *testing.T) {
	s := strings.Repeat("åäö", 10) // 2 bytes a rune
	for n := 4; n <= len(s); n++ {
		h, tl := TruncHead(s, n), TruncTail(s, n)
		for _, x := range []string{h, tl} {
			if !utf8.ValidString(x) || len(x) > n {
				t.Fatalf("n=%d: %q (%d bytes)", n, x, len(x))
			}
		}
		if n < len(s) && (!strings.HasSuffix(h, "…") || !strings.HasPrefix(tl, "…")) {
			t.Fatalf("n=%d: a cut is not marked: %q %q", n, h, tl)
		}
		if !strings.HasPrefix(s, strings.TrimSuffix(h, "…")) || !strings.HasSuffix(s, strings.TrimPrefix(tl, "…")) {
			t.Fatalf("n=%d: not the head or the tail: %q %q", n, h, tl)
		}
	}
	if TruncHead("abc", 3) != "abc" || TruncTail("abc", 3) != "abc" {
		t.Fatal("a string that fits changed")
	}
	if got := TruncHead("abcdef", 4); got != "a…" {
		t.Fatalf("head: %q", got)
	}
	if got := TruncTail("abcdef", 4); got != "…f" {
		t.Fatalf("tail: %q", got)
	}
}

// #36: keepLast keeps the newest n, in order.
func TestKeepLast(t *testing.T) {
	if got := keepLast([]int{1, 2, 3, 4}, 2); len(got) != 2 || got[0] != 3 || got[1] != 4 {
		t.Fatalf("%v", got)
	}
	if got := keepLast([]int{1, 2}, 5); len(got) != 2 {
		t.Fatalf("%v", got)
	}
}
