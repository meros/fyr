package agent

import (
	"testing"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

// V0.overload-retry: the CLI ends the run with the provider's passing failure
// as its error; the class is overloaded, so the thread waits and Fyr retries.
// A request the provider refuses every time stays a plain API failure.
func TestOverloadFailureFromAnErroredResult(t *testing.T) {
	for text, want := range map[string]string{
		"API Error: 529 Overloaded. This is a server-side issue, usually temporary — try again in a moment. If it persists, check https://status.example.com.": core.ClassOverloaded,
		"API Error: 500 Internal server error":      core.ClassOverloaded,
		"API Error: 400 invalid_request_error":      core.ClassAPI,
		"You've hit your weekly limit · resets 4am": core.ClassQuota,
	} {
		h := &qhHost{}
		m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "claude"} }})
		s := &session{thread: "t1", digest: newDigest(), live: true}
		m.handle(s, map[string]any{"type": "result", "subtype": "error", "is_error": true, "result": text})
		a := h.runEnded()
		if a == nil || a.Failure == nil || a.Failure.Class != want {
			t.Fatalf("%q: want class %q, got %+v", text, want, a)
		}
	}
}
