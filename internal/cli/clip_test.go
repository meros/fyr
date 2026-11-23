package cli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// V0.review-robust (8): a cut line never splits a character.
func TestClipIsRuneSafe(t *testing.T) {
	s := strings.Repeat("é", 300)
	got := Clip(s, 240)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != 240 {
		t.Fatalf("Clip gave %d runes, valid %v", utf8.RuneCountInString(got), utf8.ValidString(got))
	}
	if Clip("short", 240) != "short" {
		t.Fatal("a short line changed")
	}
}
