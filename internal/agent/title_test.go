package agent

import (
	"testing"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/provider"
)

// V0.108: the title call runs on the provider's Haiku model when it has one,
// else on the endpoint's Haiku variable, else on the thread's own model.
func TestTitleModel(t *testing.T) {
	login := provider.Profile{Models: []string{"claude-opus-5-5", "claude-haiku-5-5"}}
	if m := titleModel(login, "claude-opus-5-5"); m != "claude-haiku-5-5" {
		t.Fatalf("login: %q", m)
	}
	endpoint := provider.Profile{Models: []string{"deepseek-chat"}, Endpoint: cli.Endpoint{BaseURL: "https://api.deepseek.com/anthropic", ModelEnv: map[string]string{"ANTHROPIC_DEFAULT_HAIKU_MODEL": "deepseek-chat-small"}}}
	if m := titleModel(endpoint, "deepseek-chat"); m != "deepseek-chat-small" {
		t.Fatalf("endpoint with a haiku variable: %q", m)
	}
	plain := provider.Profile{Models: []string{"glm-4.6"}, DefaultModel: "glm-4.6"}
	if m := titleModel(plain, ""); m != "glm-4.6" {
		t.Fatalf("no haiku, no thread model: %q", m)
	}
	if m := titleModel(plain, "glm-4.5"); m != "glm-4.5" {
		t.Fatalf("no haiku: %q", m)
	}
}
