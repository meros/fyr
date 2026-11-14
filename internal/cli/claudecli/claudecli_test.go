package claudecli

import (
	"slices"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/cli"
)

// SPEC 0.5 V0.47: the adapter sets what a wrapper used to set, per auth kind.
func TestEnvPerAuth(t *testing.T) {
	a := Adapter{}
	env, err := a.Env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin}})
	if err != nil || len(env) != 0 {
		t.Fatalf("main login: %v %v", env, err)
	}
	env, _ = a.Env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: "/x/.claude-norban"}})
	if !slices.Equal(env, []string{"CLAUDE_CONFIG_DIR=/x/.claude-norban"}) {
		t.Fatalf("account: %v", env)
	}
	env, _ = a.Env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: "/x/n"}, Key: "tok"})
	if !slices.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=tok") {
		t.Fatalf("login token: %v", env)
	}
	env, _ = a.Env(cli.Launch{Auth: cli.Auth{Kind: cli.APIKey, Secret: "router"}, Key: "sk-1",
		Endpoint: cli.Endpoint{BaseURL: "https://openrouter.ai/api", ModelEnv: map[string]string{"ANTHROPIC_DEFAULT_HAIKU_MODEL": "z", "ANTHROPIC_MODEL": "m"}}})
	want := []string{"ANTHROPIC_BASE_URL=https://openrouter.ai/api", "ANTHROPIC_AUTH_TOKEN=sk-1", "ANTHROPIC_API_KEY=", "ANTHROPIC_DEFAULT_HAIKU_MODEL=z", "ANTHROPIC_MODEL=m"}
	if !slices.Equal(env, want) {
		t.Fatalf("endpoint: %v", env)
	}
	env, _ = a.Env(cli.Launch{Auth: cli.Auth{Kind: cli.APIKey}, Key: "sk-ant"})
	if !slices.Equal(env, []string{"ANTHROPIC_API_KEY=sk-ant"}) {
		t.Fatalf("anthropic key: %v", env)
	}
	if _, err := a.Env(cli.Launch{Auth: cli.Auth{Kind: cli.APIKey}}); err == nil {
		t.Fatal("api-key without a key must fail")
	}
}

func TestArgs(t *testing.T) {
	got := strings.Join(Args(cli.Launch{Model: "claude-opus-5-5", Effort: "high", Session: "s", Resume: true, MCPConfig: "{}", AllowedTools: []string{"mcp__fyr__*"}}), " ")
	if got != "--model claude-opus-5-5 --effort high --resume s --permission-mode auto --mcp-config {} --allowedTools mcp__fyr__*" {
		t.Fatal(got)
	}
	if strings.Contains(strings.Join(Args(cli.Launch{Session: "s"}), " "), "--effort") {
		t.Fatal("no effort asked")
	}
}

func TestEffortFor(t *testing.T) {
	ms := []cli.Model{{ID: "opus", Efforts: Efforts}, {ID: "deepseek-chat"}}
	cases := []struct{ model, asked, def, want string }{
		{"opus", "", "high", "high"},
		{"opus", "max", "high", "max"},
		{"opus", "bogus", "medium", "medium"},
		{"deepseek-chat", "high", "high", ""},
		{"unknown", "", "high", ""},
		{"unknown", "low", "", "low"},
	}
	for _, c := range cases {
		if got := cli.EffortFor(ms, c.model, c.asked, c.def); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
}

func TestCaps(t *testing.T) {
	a := Adapter{}
	if !cli.Accepts(a, cli.APIKey) || !cli.Accepts(a, cli.CLILogin) || a.ID() != "claude" {
		t.Fatal("caps")
	}
}
