package agent

import (
	"encoding/json"
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
	plain := provider.Profile{Models: []string{"glm-4.6"}, DefaultModel: "glm-4.6", Auth: cli.Auth{Kind: cli.APIKey}, Endpoint: cli.Endpoint{BaseURL: "https://api.z.ai/api/anthropic"}}
	if m := HaikuModel(plain); m != "" {
		t.Fatalf("no haiku: %q", m)
	}
}

// V0.117: the live Claude login (16 Nov) stores its model list without Haiku,
// as the registry builds it; the default system job must still find
// claude-haiku-5-5 on it (claudecli's own list), not fail. An endpoint keeps
// its own rule.
func TestHaikuModelOnAStoredClaudeLogin(t *testing.T) {
	const live = `{"id":"claudeAgent","name":"Claude","color":"#d97757","cli":"claude","auth":{"kind":"cli-login"},"endpoint":{},` +
		`"models":[{"id":"claude-opus-5-5","name":"Opus 5.5"},{"id":"claude-sonnet-5-5","name":"Sonnet 5.5"},{"id":"claude-fable-5-2","name":"Fable 5.2"}]}`
	var pv provider.Provider
	if err := json.Unmarshal([]byte(live), &pv); err != nil {
		t.Fatal(err)
	}
	p := pv.Profile(nil, "")
	if m := HaikuModel(p); m != "claude-haiku-5-5" {
		t.Fatalf("stored Claude login: %q", m)
	}
	token := p
	token.Auth.Secret = "claudeNorban.key" // a login by token is still a Claude login
	if m := HaikuModel(token); m != "claude-haiku-5-5" {
		t.Fatalf("token login: %q", m)
	}
	ep := provider.Profile{CLI: "claude", Models: []string{"glm-4.6"}, Auth: cli.Auth{Kind: cli.APIKey}, Endpoint: cli.Endpoint{BaseURL: "https://api.z.ai/api/anthropic"}}
	if m := HaikuModel(ep); m != "" {
		t.Fatalf("an endpoint got a Claude model: %q", m)
	}
}
