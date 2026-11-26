package heavy

import "testing"

// V0.freeze-controller: a shell keyword or a bare interpreter is no heavy
// command to learn.
func TestSuggestSkipsKeywordsAndBareInterpreters(t *testing.T) {
	for _, cmd := range []string{
		"for f in a b; do yarn lint $f; done",
		"python3 <<'EOF'\nprint(1)\nEOF",
		"python3 -c 'print(1)'",
		"done",
		"bash",
		"cd x && node",
	} {
		if p, _ := Suggest(cmd); p != "" {
			// A for loop teaches its body, not `done`.
			if p == "done" || p == "python3" || Trivial(p) {
				t.Errorf("Suggest(%q) = %q, a trivial pattern", cmd, p)
			}
		}
	}
	for _, p := range []string{"done", "python3", `python3 <<'EOF'`, `python3 <<'`, "fi", "bash"} {
		if !Trivial(p) {
			t.Errorf("Trivial(%q) = false", p)
		}
	}
	for _, p := range []string{"yarn lint", "go test", "node scripts/x\\.mjs", "make check", "nix develop"} {
		if Trivial(p) {
			t.Errorf("Trivial(%q) = true", p)
		}
	}
}
