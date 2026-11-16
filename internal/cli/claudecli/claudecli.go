// Package claudecli is the adapter for the plain `claude` CLI (SPEC 0.5
// V0.50): it starts the binary on PATH, never a wrapper, and sets what a
// wrapper used to set (CLAUDE_CONFIG_DIR, ANTHROPIC_BASE_URL, the key, the
// model variables). The protocol is package claude's stream-json client.
package claudecli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/proc"
	"github.com/meros/fyr/internal/runner"
)

// ID is the adapter id providers name.
const ID = "claude"

// Efforts are the levels `claude --effort` takes.
var Efforts = []string{"low", "medium", "high", "xhigh", "max"}

// Models is the CLI's own list for a Claude login.
var Models = []cli.Model{
	{ID: "claude-opus-5-5", Name: "Opus 5.5", Efforts: Efforts},
	{ID: "claude-sonnet-5-5", Name: "Sonnet 5.5", Efforts: Efforts},
	{ID: "claude-fable-5-2", Name: "Fable 5.2", Efforts: Efforts},
	{ID: "claude-haiku-5-5", Name: "Haiku 5.5"},
}

// Adapter runs `claude`.
type Adapter struct{}

func (Adapter) ID() string { return ID }

func (Adapter) Caps() cli.Caps {
	return cli.Caps{Auth: []cli.AuthKind{cli.CLILogin, cli.APIKey, cli.NoAuth}, Efforts: Efforts, Models: Models}
}

// Env applies auth and endpoint:
//   - a config folder → CLAUDE_CONFIG_DIR (its own login and sessions);
//   - api-key with an endpoint → ANTHROPIC_BASE_URL and ANTHROPIC_AUTH_TOKEN,
//     and ANTHROPIC_API_KEY emptied so no other key goes to that endpoint;
//   - api-key without an endpoint → ANTHROPIC_API_KEY (Anthropic's API);
//   - a cli-login with a stored token → CLAUDE_CODE_OAUTH_TOKEN;
//   - the endpoint's model variables.
func (Adapter) Env(l cli.Launch) ([]string, error) {
	env := append([]string{}, l.Env...)
	if d := l.Auth.ConfigDir; d != "" {
		env = append(env, "CLAUDE_CONFIG_DIR="+hostfs.ExpandHome(d, ""))
	}
	switch l.Auth.Kind {
	case cli.APIKey:
		if l.Key == "" {
			return nil, fmt.Errorf("no API key")
		}
		if l.Endpoint.BaseURL != "" {
			env = append(env, "ANTHROPIC_BASE_URL="+l.Endpoint.BaseURL, "ANTHROPIC_AUTH_TOKEN="+l.Key, "ANTHROPIC_API_KEY=")
		} else {
			env = append(env, "ANTHROPIC_API_KEY="+l.Key)
		}
	case cli.CLILogin:
		if l.Key != "" {
			env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+l.Key)
		}
	case cli.NoAuth, "":
	default:
		return nil, fmt.Errorf("claude: unknown auth %q", l.Auth.Kind)
	}
	keys := make([]string, 0, len(l.Endpoint.ModelEnv))
	for k := range l.Endpoint.ModelEnv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+l.Endpoint.ModelEnv[k])
	}
	return env, nil
}

// Args are the flags of a launch (after the protocol flags).
func Args(l cli.Launch) []string {
	args := []string{}
	if l.Model != "" {
		args = append(args, "--model", l.Model)
	}
	if l.Effort != "" {
		args = append(args, "--effort", l.Effort)
	}
	if l.Resume {
		args = append(args, "--resume", l.Session)
	} else {
		args = append(args, "--session-id", l.Session)
	}
	for _, d := range l.AddDirs {
		args = append(args, "--add-dir", d)
	}
	mode := l.PermissionMode
	if mode == "" {
		mode = "auto"
	}
	args = append(args, "--permission-mode", mode)
	if l.MCPConfig != "" {
		args = append(args, "--mcp-config", l.MCPConfig)
	}
	if len(l.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(l.AllowedTools, ","))
	}
	if l.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", l.SystemPrompt)
	}
	return args
}

func (a Adapter) Start(l cli.Launch, h cli.Handlers) (cli.Session, error) {
	env, err := a.Env(l)
	if err != nil {
		return nil, err
	}
	bin := l.Binary
	if bin == "" {
		bin = "claude"
	}
	c, err := claude.Start(claude.Spec{Binary: bin, Args: Args(l), Dir: l.Dir, Env: env, Log: l.Log, Cgroup: l.Cgroup,
		Runner: l.Runner, ID: l.ProcID, Key: l.RunnerKey, Meta: l.Meta})
	if err != nil {
		return nil, err
	}
	c.OnCanUseTool, c.OnCancel, c.OnHook = h.CanUseTool, h.Cancel, h.Hook
	return session{c}, nil
}

// PrintArgs are the flags of a one-shot print run (V0.108): no tools, no
// MCP servers, no settings files and no saved session, and system replaces
// Claude Code's system prompt, so the call reads little more than the prompt.
func PrintArgs(model, system string) []string {
	args := []string{"-p"}
	if model != "" {
		args = append(args, "--model", model)
	}
	return append(args, "--system-prompt", system, "--tools", "", "--strict-mcp-config", "--no-session-persistence",
		"--setting-sources", "", "--output-format", "text")
}

// Print runs `claude -p` once with the launch's auth and endpoint, in
// l.Dir, and returns its answer.
func (a Adapter) Print(ctx context.Context, l cli.Launch, system, prompt string) (string, error) {
	env, err := a.Env(l)
	if err != nil {
		return "", err
	}
	bin := l.Binary
	if bin == "" {
		bin = "claude"
	}
	cmd := proc.Command(ctx, bin, PrintArgs(l.Model, system)...)
	cmd.Dir = l.Dir
	cmd.Env = append(append(os.Environ(), "CLAUDE_CODE_ENTRYPOINT=sdk-go"), env...)
	cmd.Stdin = strings.NewReader(prompt)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("no answer in time")
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if r := []rune(msg); len(r) > 300 {
			msg = "…" + string(r[len(r)-300:])
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("claude: %s", msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// Reattach speaks to a process the runner kept across a restart (V0.90).
func (Adapter) Reattach(p runner.Pipe, log io.Writer, h cli.Handlers) (cli.Session, error) {
	c := claude.Open(p, log, func(c *claude.Client) {
		c.OnCanUseTool, c.OnCancel, c.OnHook = h.CanUseTool, h.Cancel, h.Hook
	})
	return session{c}, nil
}

type session struct{ *claude.Client }

func (s session) Out() <-chan cli.Msg { return s.Client.Out }

// SessionsDir: the auth's config folder, else the daemon's
// CLAUDE_CONFIG_DIR, else ~/.claude.
func (Adapter) SessionsDir(a cli.Auth) string {
	if a.ConfigDir != "" {
		return hostfs.ExpandHome(a.ConfigDir, "")
	}
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return hostfs.ExpandHome(v, "")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}

// LoggedIn: <config>/.credentials.json exists, or on macOS its Keychain item.
func (a Adapter) LoggedIn(au cli.Auth) bool {
	dir := a.SessionsDir(au)
	st, err := os.Stat(filepath.Join(dir, ".credentials.json"))
	return (err == nil && st.Mode().IsRegular()) || hasKeychainLogin(dir)
}

func (Adapter) SessionFile(dir, cwd, sid string) string { return SessionFile(dir, cwd, sid) }

func (Adapter) CopySession(src, fromDir, toDir, sid string) error {
	return CopySession(src, fromDir, toDir, sid)
}
