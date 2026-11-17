package heavy

import "testing"

// A PR body in a heredoc inside $(...) names heavy commands in backticks; none of them runs.
func TestH174HeredocInsideCommandSubstitution(t *testing.T) {
	cmd := "gh pr create --title \"x\" --body \"$(cat <<'EOF'\n## Tests\n- Why: \"2.0 of 2.0 in use\".\n- Ran: `make test-go`: green.\nEOF\n)\"; git rev-parse HEAD"
	if got := Default().Class(cmd); got != "" {
		t.Fatalf("got %q", got)
	}
}
