package cli

import (
	"slices"
	"strings"
	"testing"
)

// V0.review-structure: the helpers both adapters share.
func TestSharedAdapterHelpers(t *testing.T) {
	if got := (Endpoint{ModelEnv: map[string]string{"B": "2", "A": "1"}}).Env(); !slices.Equal(got, []string{"A=1", "B=2"}) {
		t.Fatalf("Endpoint.Env %v", got)
	}
	if got := ErrText("  \n", "fallback"); got != "fallback" {
		t.Fatalf("ErrText empty %q", got)
	}
	if got := ErrText(strings.Repeat("é", 400)+"end", ""); !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "end") || len([]rune(got)) != 301 {
		t.Fatalf("ErrText long %q", got)
	}
	text := "Error: first\nvisit https://example.com/error\nall good\nInvalid code\n"
	if got := LastErrorLine(text, []string{"invalid", "error"}, strings.ToUpper); got != "INVALID CODE" {
		t.Fatalf("LastErrorLine %q", got)
	}
	if got := LastErrorLine("fine\n", []string{"error"}, strings.ToUpper); got != "" {
		t.Fatalf("LastErrorLine without one %q", got)
	}
	env := LoginEnv([]string{"PATH=/bin", "KEY=x", "DISPLAY=:0", "TERM=dumb"}, []string{"KEY"}, "DIR=/d")
	if !slices.Equal(env, []string{"PATH=/bin", "DIR=/d", "BROWSER=true", "TERM=xterm-256color", "NO_COLOR=1"}) {
		t.Fatalf("LoginEnv %v", env)
	}
}
