package agent

import (
	"testing"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/provider"
)

// V0.108, amended by V0.115 (4): a system job's default model is the
// provider's Haiku model, else the endpoint's Haiku variable, else none: it
// no longer falls back to the thread's own model.
func TestHaikuModel(t *testing.T) {
	login := provider.Profile{Models: []string{"claude-opus-5-5", "claude-haiku-5-5"}}
	if m := HaikuModel(login); m != "claude-haiku-5-5" {
		t.Fatalf("login: %q", m)
	}
	endpoint := provider.Profile{Models: []string{"deepseek-chat"}, Endpoint: cli.Endpoint{BaseURL: "https://api.deepseek.com/anthropic", ModelEnv: map[string]string{"ANTHROPIC_DEFAULT_HAIKU_MODEL": "deepseek-chat-small"}}}
	if m := HaikuModel(endpoint); m != "deepseek-chat-small" {
		t.Fatalf("endpoint with a haiku variable: %q", m)
	}
	plain := provider.Profile{Models: []string{"glm-4.6"}, DefaultModel: "glm-4.6"}
	if m := HaikuModel(plain); m != "" {
		t.Fatalf("no haiku: %q", m)
	}
}
