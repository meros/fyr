package agent

import (
	"strings"
	"testing"
)

// V0.changes-code-only (6): the Fyr block no longer says to open a change
// before the first write to something Fyr cannot see; a write the owner must
// approve is a Run card.
func TestPromptCodeOnly(t *testing.T) {
	for _, old := range []string{"Before the first write", "cannot see (a cluster", "a document or a design), open a change", "Ask the owner with change_request"} {
		if strings.Contains(FyrPrompt, old) {
			t.Errorf("the prompt still says %q", old)
		}
	}
	if !strings.Contains(ChangesPrompt, "run_request") || !strings.Contains(ChangesPrompt, "Run card") {
		t.Fatalf("the changes paragraph does not name the Run card: %s", ChangesPrompt)
	}
}
