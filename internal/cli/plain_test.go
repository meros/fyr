package cli

import "testing"

func TestPlain(t *testing.T) {
	if got := Plain([]byte("Welcome\x1b[9Gto\x1b[1Bline2\r\n")); got != "Welcome to\nline2\n" {
		t.Fatalf("Plain cursor moves = %q", got)
	}
	osc := "\x1b]8;;https://example.com/auth\x07Click here\x1b]8;;\x07"
	if got := Plain([]byte(osc)); got != " https://example.com/auth " {
		t.Fatalf("Plain OSC 8 = %q", got)
	}
}
