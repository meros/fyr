// Package agycli is the adapter for Google's Antigravity CLI (`agy`).
//
// It speaks the Agent Client Protocol (`agy acp --stdio`) over newline-delimited
// JSON-RPC 2.0 on stdin/stdout and translates ACP events and callbacks into
// Fyr's stream-json observation format. Each wire message used here was proved
// against `agy 1.5.13`.
package agycli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/fsutil"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/runner"
)

// ID is the adapter id providers name.
const ID = "agy"

// Efforts are the reasoning effort levels `agy` takes (`thought_level` in ACP,
// `--effort` in CLI).
var Efforts = []string{"low", "medium", "high", "xhigh"}

// Models is the known default model list for an Antigravity login, grouped by
// base model with per-model effort levels (proved against `agy 1.5.13` ACP
// `configOptions` + `availableModels`).
var Models = []cli.Model{
	{ID: "gemini-4-neon", Name: "Gemini 4 Neon", Efforts: []string{"low", "medium", "high", "xhigh"}},
	{ID: "gemini-4-argon", Name: "Gemini 4 Argon", Efforts: []string{"low", "medium", "high"}},
	{ID: "gemini-4-hydrogen", Name: "Gemini 4 Hydrogen", Efforts: []string{"low", "medium", "high", "xhigh"}},
	{ID: "gemini-3.8-flash", Name: "Gemini 3.8 Flash", Efforts: []string{"low", "medium", "high"}},
	{ID: "gemini-pro-orchestrator-neon", Name: "Orchestrator (Neon)"},
	{ID: "gemini-pro-orchestrator-g1-pro", Name: "Orchestrator (Argon)"},
	{ID: "claude-opus-5-5", Name: "Claude Opus 5.5", Efforts: []string{"low", "medium", "high"}},
	{ID: "claude-sonnet-5-5", Name: "Claude Sonnet 5.5", Efforts: []string{"low", "medium", "high"}},
	{ID: "gpt-oss-120b-medium", Name: "GPT-OSS 120B (Medium)"},
}

// CredentialVars are the variables that name who `agy` talks to and as whom
// (cli.Caps.CredentialVars, V0.review-hangs (1)): Env sets the key and the
// endpoint of an api-key provider; a cli-login or no-auth provider must not
// inherit fyrd's.
var CredentialVars = []string{"GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GEMINI_BASE_URL", "AGY_ADC_AUTH"}

// Adapter runs `agy` via ACP.
type Adapter struct{}

func (Adapter) ID() string               { return ID }
func (Adapter) DefaultBinary() string    { return "agy" }
func (Adapter) DefaultConfigDir() string { return "~/.gemini" }

// FastModel picks a provider's fast model for system jobs (titles, setup,
// approval judge): Gemini 4 Hydrogen first, then Flash.
func (Adapter) FastModel(models []cli.Model, _ map[string]string, isEndpoint bool) string {
	pick := func(ms []cli.Model) string {
		for _, kw := range []string{"hydrogen", "flash"} {
			for _, m := range ms {
				if strings.Contains(strings.ToLower(m.ID), kw) {
					return m.ID
				}
			}
		}
		return ""
	}
	if id := pick(models); id != "" {
		return id
	}
	if !isEndpoint {
		return pick(Models)
	}
	return ""
}

func (Adapter) LoginSpec(base []string, configDir string, _ bool) cli.LoginSpec {
	args, term := LoginArgs(configDir)
	return cli.LoginSpec{
		Binary:   "agy",
		Args:     args,
		Terminal: term,
		Env:      LoginEnv(base),
	}
}

func (Adapter) ParseLogin(buf []byte, _ bool) cli.LoginOut {
	o := ParseLogin(buf)
	return cli.LoginOut{URL: o.URL, Prompt: o.Prompt, Line: o.Line}
}

func (Adapter) RedactLogin(s string) string { return Redact(s) }

func (Adapter) LoginNeed(configDir string, _ bool) string {
	cmd := "agy acp --login"
	if configDir != "" {
		cmd = "agy acp --login --gemini-dir " + configDir
	}
	return "Press Log in to sign in to this account here. Or run `" + cmd + "` in a terminal on this host."
}

func (Adapter) Caps() cli.Caps {
	return cli.Caps{
		Auth:           []cli.AuthKind{cli.CLILogin, cli.APIKey, cli.NoAuth},
		Efforts:        Efforts,
		Models:         Models,
		CredentialVars: CredentialVars,
	}
}

// Env applies auth and endpoint for `agy`:
//   - api-key: sets GEMINI_API_KEY, and GOOGLE_GEMINI_BASE_URL when Endpoint.BaseURL is set;
//   - cli-login / none: passes through l.Env;
//   - any Endpoint.ModelEnv entries in sorted order.
//
// Note: for api-key auth, `agy` also requires `{"modelProvider":"gemini"}` in
// `<gemini-dir>/antigravity-cli/settings.json`; Start/Print/ListModels prepare
// an isolated gemini dir when launching an API-key provider.
func (Adapter) Env(l cli.Launch) ([]string, error) {
	env := append([]string{}, l.Env...)
	switch l.Auth.Kind {
	case cli.APIKey:
		if l.Key == "" {
			return nil, fmt.Errorf("no API key")
		}
		env = append(env, "GEMINI_API_KEY="+l.Key)
		if l.Endpoint.BaseURL != "" {
			env = append(env, "GOOGLE_GEMINI_BASE_URL="+l.Endpoint.BaseURL)
		}
	case cli.CLILogin, cli.NoAuth, "":
	default:
		return nil, fmt.Errorf("agy: unknown auth %q", l.Auth.Kind)
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

// Args are the `agy` flags for an ACP session launch.
func Args(l cli.Launch, geminiDir string) []string {
	args := []string{"acp", "--stdio"}
	if geminiDir != "" {
		args = append(args, "--gemini-dir", geminiDir)
	} else if d := l.Auth.ConfigDir; d != "" {
		args = append(args, "--gemini-dir", hostfs.ExpandHome(d, ""))
	}
	for _, d := range l.AddDirs {
		if d != "" {
			args = append(args, "--add-dir", d)
		}
	}
	if isBypassMode(l.PermissionMode) {
		args = append(args, "--dangerously-skip-permissions")
	}
	return args
}

func isBypassMode(mode string) bool {
	return mode == "bypassPermissions" || mode == "dontAsk"
}

// mapPermissionMode maps Fyr's permission mode words to ACP mode ids.
func mapPermissionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "", "auto":
		return "auto"
	case "acceptEdits", "accept-edits":
		return "accept-edits"
	case "plan":
		return "plan"
	case "default":
		return "default"
	case "bypassPermissions", "dontAsk":
		return "" // handled via --dangerously-skip-permissions
	default:
		return mode
	}
}

// prepareGeminiDir sets up `--gemini-dir` when needed. For API-key providers
// without an explicit config folder, it creates a private temp folder with
// `antigravity-cli/settings.json` set to `{"modelProvider":"gemini"}` so the
// user's `~/.gemini` is never touched.
func prepareGeminiDir(l cli.Launch) (dir string, cleanup func(), err error) {
	cleanup = func() {}
	if l.Auth.Kind != cli.APIKey {
		if l.Auth.ConfigDir != "" {
			return hostfs.ExpandHome(l.Auth.ConfigDir, ""), cleanup, nil
		}
		return "", cleanup, nil
	}
	if l.Auth.ConfigDir != "" {
		dir = hostfs.ExpandHome(l.Auth.ConfigDir, "")
	} else {
		tmp, err := os.MkdirTemp("", "fyr-agy-key-")
		if err != nil {
			return "", cleanup, fmt.Errorf("agy config dir: %w", err)
		}
		dir = tmp
		cleanup = func() { os.RemoveAll(tmp) }
	}
	appDir := filepath.Join(dir, "antigravity-cli")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("agy config dir: %w", err)
	}
	settingsPath := filepath.Join(appDir, "settings.json")
	cfg := map[string]any{}
	b, rerr := os.ReadFile(settingsPath)
	switch {
	case rerr == nil:
		if err := json.Unmarshal(b, &cfg); err != nil {
			// The user's own file, which we could not read: leave it as it is.
			cleanup()
			return "", func() {}, fmt.Errorf("agy settings %s: %w", settingsPath, err)
		}
	case !errors.Is(rerr, fs.ErrNotExist):
		cleanup()
		return "", func() {}, fmt.Errorf("agy settings: %w", rerr)
	}
	if cfg["modelProvider"] != "gemini" {
		cfg["modelProvider"] = "gemini"
		b, _ := json.MarshalIndent(cfg, "", "  ")
		if err := fsutil.WriteAtomic(settingsPath, b, 0o600); err != nil {
			cleanup()
			return "", func() {}, fmt.Errorf("agy settings: %w", err)
		}
	}
	return dir, cleanup, nil
}

// Start launches an `agy acp --stdio` session process.
func (a Adapter) Start(l cli.Launch, h cli.Handlers) (cli.Session, error) {
	env, err := a.Env(l)
	if err != nil {
		return nil, err
	}
	geminiDir, cleanup, err := prepareGeminiDir(l)
	if err != nil {
		return nil, err
	}
	mcpServers, err := readMCPConfig(l.MCPConfigFile)
	if err != nil {
		cleanup()
		return nil, err
	}
	bin := l.Binary
	if bin == "" {
		bin = "agy"
	}
	s, err := startACP(acpSpec{
		Binary:          bin,
		Args:            Args(l, geminiDir),
		Dir:             l.Dir,
		Env:             env,
		Log:             l.Log,
		Cgroup:          l.Cgroup,
		Wrap:            l.Wrap,
		Runner:          l.Runner,
		ID:              l.ProcID,
		Key:             l.RunnerKey,
		Meta:            l.Meta,
		SessionID:       l.Session,
		Resume:          l.Resume,
		Model:           l.Model,
		Effort:          l.Effort,
		PermissionMode:  l.PermissionMode,
		SystemPrompt:    l.SystemPrompt,
		AddDirs:         l.AddDirs,
		MCPServers:      mcpServers,
		AllowedTools:    l.AllowedTools,
		DisallowedTools: l.DisallowedTools,
		Cleanup:         cleanup,
	}, h)
	if err != nil {
		cleanup()
		return nil, err
	}
	return s, nil
}

// Reattach speaks ACP over a runner pipe kept across a restart of fyrd (V0.90).
func (Adapter) Reattach(p runner.Pipe, log io.Writer, h cli.Handlers) (cli.Session, error) {
	return openACP(p, acpSpec{Log: log}, h, true), nil
}

// ReattachSession speaks ACP over a runner pipe with the thread's known
// session state restored across a restart of fyrd (V0.90, V0.agy-reattach-queue).
func (Adapter) ReattachSession(p runner.Pipe, state cli.ReattachState, log io.Writer, h cli.Handlers) (cli.Session, error) {
	return openACP(p, acpSpec{
		Log:             log,
		SessionID:       state.SessionID,
		Model:           state.Model,
		ReattachRunning: state.Running,
		ReattachQueued:  state.Queued,
		AllowedTools:    state.AllowedTools,
		DisallowedTools: state.DisallowedTools,
		MCPServers:      state.MCPServers,
	}, h, true), nil
}

// Print runs one prompt via an ephemeral ACP session with no tools allowed
// (every permission request is denied), no MCP servers, and system appended as
// the session system prompt. Prompts travel on stdin so arbitrarily large
// conversations never hit command-line limits or leak into process listings.
func (a Adapter) Print(ctx context.Context, l cli.Launch, system, prompt string) (string, error) {
	l.MCPConfigFile = ""
	l.AllowedTools = nil
	l.Resume = false
	l.Session = ""
	l.PermissionMode = "default"
	l.SystemPrompt = system
	l.Runner = nil
	if l.Dir == "" {
		l.Dir = os.TempDir()
	}
	denyAll := cli.Handlers{
		CanUseTool: func(cli.Msg) cli.Msg {
			return cli.Msg{"behavior": "deny", "message": "Tools are disabled for this call."}
		},
	}
	sess, err := a.Start(l, denyAll)
	if err != nil {
		return "", err
	}
	defer func() {
		sess.Kill()
		<-sess.Done()
	}()
	if _, err := sess.Initialize(ctx, nil); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("no answer in time")
		}
		return "", fmt.Errorf("agy: %v", err)
	}
	if err := sess.SendUser([]cli.Msg{{"type": "text", "text": prompt}}, "print"); err != nil {
		return "", fmt.Errorf("agy: %v", err)
	}
	var lastText string
	for {
		select {
		case msg, ok := <-sess.Out():
			if !ok {
				if ctx.Err() != nil {
					return "", fmt.Errorf("no answer in time")
				}
				if lastText != "" {
					return strings.TrimSpace(lastText), nil
				}
				if serr := sess.Err(); serr != nil {
					return "", fmt.Errorf("agy: %s", trimErr(serr.Error()))
				}
				return "", fmt.Errorf("agy: ended without a response")
			}
			switch msg["type"] {
			case "assistant":
				if inner, _ := msg["message"].(cli.Msg); inner != nil {
					if blocks, _ := inner["content"].([]any); len(blocks) > 0 {
						for _, x := range blocks {
							if b, _ := x.(cli.Msg); b["type"] == "text" {
								if t, _ := b["text"].(string); t != "" {
									lastText += t
								}
							}
						}
					}
				}
			case "result":
				isErr, _ := msg["is_error"].(bool)
				res, _ := msg["result"].(string)
				if isErr {
					return "", fmt.Errorf("agy: %s", trimErr(res))
				}
				if res != "" {
					return strings.TrimSpace(res), nil
				}
				return strings.TrimSpace(lastText), nil
			}
		case <-ctx.Done():
			return "", fmt.Errorf("no answer in time")
		}
	}
}

func trimErr(msg string) string {
	msg = strings.TrimSpace(msg)
	if r := []rune(msg); len(r) > 300 {
		msg = "…" + string(r[len(r)-300:])
	}
	if msg == "" {
		return "unknown error"
	}
	return msg
}

func (Adapter) SessionsDir(a cli.Auth) string { return SessionsDir(a) }

func (Adapter) SessionFile(dir, cwd, sid string) string { return SessionFile(dir, cwd, sid) }

func (Adapter) CopySession(src, fromDir, toDir, sid string) error {
	return CopySession(src, fromDir, toDir, sid)
}

func (a Adapter) LoggedIn(au cli.Auth) bool { return LoggedIn(au) }
