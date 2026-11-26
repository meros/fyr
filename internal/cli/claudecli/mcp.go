package claudecli

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/launchenv"
)

// V0.provider-mcp: the MCP servers of a provider, through `claude mcp`
// (2.1.327, checked 26 Nov 2026). The CLI has no JSON output for them:
// `claude mcp list` prints one line per server after its health check,
//
//	<name>: <url or command line>[ (HTTP)|(SSE)] - ✔ Connected
//	<name>: … - ! Needs authentication
//	<name>: … - ✘ Failed to connect — <reason>
//	<name>: … - ⏸ Pending approval
//
// and the scope is in no line: a claude.ai connector is named "claude.ai …",
// a plugin's server "plugin:…", and a server of the user config is in that
// file's mcpServers. `add-json -` reads the server's JSON on stdin, so a
// header or env value is never in the process's arguments; `login
// --no-browser` draws on a terminal only, prints the sign-in link and
// reads the address the browser was sent back to.

// sessionVars mark a process started inside a Claude Code session; the CLI
// then refuses `add-json -` ("its permission prompt would not show the
// server"). fyrd started from an agent's shell carries them; the owner's
// press in Settings is not a session's tool call.
var sessionVars = []string{"CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ID",
	"CLAUDE_CODE_SESSION_ATTENDED", "CLAUDE_PID", "CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_MESSAGING_SOCKET",
	"CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_CODE_CHILD_NOTES_DIR", "CLAUDE_CODE_FORK_SUBAGENT"}

// MCPNameRE is a name the owner may give a new server.
var MCPNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// MCPEnv is the environment of every `claude mcp` command of a provider:
// base without credentials, session markers or a display, then what its
// sessions get (Env: its config folder, token, key and endpoint), with no
// way to open a browser on the host.
func (a Adapter) MCPEnv(base []string, l cli.Launch) ([]string, error) {
	env, err := a.Env(l)
	if err != nil {
		return nil, err
	}
	return cli.LoginEnv(launchenv.Without(base, sessionVars), claude.CredentialVars, env...), nil
}

// MCPCommand is how to run one `claude mcp` command for the provider l.
func (a Adapter) MCPCommand(base []string, l cli.Launch, op cli.MCPOp) (cli.MCPRun, error) {
	env, err := a.MCPEnv(base, l)
	if err != nil {
		return cli.MCPRun{}, err
	}
	run := cli.MCPRun{Binary: l.Binary, Env: env}
	if run.Binary == "" {
		run.Binary = "claude"
	}
	if op.Kind != "list" && op.Kind != "add" {
		if err := checkName(op.Name); err != nil {
			return cli.MCPRun{}, err
		}
	}
	switch op.Kind {
	case "list":
		run.Args = []string{"mcp", "list"}
	case "add":
		if op.Add == nil {
			return cli.MCPRun{}, errors.New("no server to add")
		}
		js, err := MCPJSON(*op.Add)
		if err != nil {
			return cli.MCPRun{}, err
		}
		scope := op.Scope
		if scope == "" {
			scope = cli.MCPUser
		}
		if scope != cli.MCPUser {
			return cli.MCPRun{}, errors.New("Fyr adds a server to the user config only")
		}
		run.Args, run.Stdin = []string{"mcp", "add-json", "--scope", scope, op.Add.Name, "-"}, js
	case "remove":
		switch op.Scope {
		case cli.MCPUser, cli.MCPLocal, cli.MCPProject:
		case cli.MCPPlugin:
			return cli.MCPRun{}, errors.New("A plugin's server goes away with the plugin: remove or turn off the plugin instead.")
		case cli.MCPConnector:
			return cli.MCPRun{}, errors.New("A claude.ai connector is removed in claude.ai's connector settings, not here.")
		default:
			return cli.MCPRun{}, errors.New("this server is not in a config Fyr can change")
		}
		run.Args = []string{"mcp", "remove", "--scope", op.Scope, op.Name}
	case "login":
		run.Args, run.Terminal = []string{"mcp", "login", "--no-browser", op.Name}, true
	case "logout":
		run.Args = []string{"mcp", "logout", op.Name}
	default:
		return cli.MCPRun{}, fmt.Errorf("unknown MCP command %q", op.Kind)
	}
	return run, nil
}

// checkName: a server's name as one argument (it may hold spaces, as a
// connector's does); never a flag.
func checkName(n string) error {
	if n == "" || strings.HasPrefix(n, "-") || len(n) > 200 || strings.ContainsFunc(n, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return errors.New("no such server")
	}
	return nil
}

var headerNameRE = regexp.MustCompile(`^[A-Za-z0-9!#$%&'*+.^_|~-]{1,128}$`)

// MCPJSON is the CLI's JSON for a server the owner adds, checked.
func MCPJSON(s cli.MCPAdd) (string, error) {
	if !MCPNameRE.MatchString(s.Name) {
		return "", errors.New("A name is letters, digits, '-', '_' or '.', up to 64, starting with a letter or digit.")
	}
	m := map[string]any{"type": s.Transport}
	switch s.Transport {
	case "http", "sse":
		u, err := url.Parse(strings.TrimSpace(s.URL))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return "", errors.New("The URL must be an http(s) address.")
		}
		m["url"] = u.String()
		if len(s.Headers) > 0 {
			h := map[string]string{}
			for k, v := range s.Headers {
				if !headerNameRE.MatchString(k) || strings.ContainsAny(v, "\r\n") {
					return "", fmt.Errorf("bad header %q", k)
				}
				h[k] = v
			}
			m["headers"] = h
		}
		if len(s.Env) > 0 || s.Command != "" {
			return "", errors.New("an " + s.Transport + " server takes a URL and headers, not a command")
		}
	case "stdio":
		if strings.TrimSpace(s.Command) == "" {
			return "", errors.New("A stdio server needs a command.")
		}
		m["command"] = strings.TrimSpace(s.Command)
		m["args"] = append([]string{}, s.Args...)
		if len(s.Env) > 0 {
			e := map[string]string{}
			for k, v := range s.Env {
				if !envNameRE.MatchString(k) {
					return "", fmt.Errorf("bad variable name %q", k)
				}
				e[k] = v
			}
			m["env"] = e
		}
		if len(s.Headers) > 0 || s.URL != "" {
			return "", errors.New("a stdio server takes a command and env, not a URL")
		}
	default:
		return "", errors.New("The transport is stdio, http or sse.")
	}
	b, err := json.Marshal(m)
	return string(b), err
}

var envNameRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// MCPConfigFile is the user config the CLI reads for l: .claude.json in
// CLAUDE_CONFIG_DIR, else in the home folder (not ~/.claude: a provider
// whose folder is ~/.claude set by name has a config of its own).
func (Adapter) MCPConfigFile(l cli.Launch, home string) string {
	if d := l.Auth.ConfigDir; d != "" {
		return filepath.Join(hostfs.ExpandHome(d, home), ".claude.json")
	}
	return filepath.Join(home, ".claude.json")
}

// listRE is one server line of `claude mcp list`.
var listRE = regexp.MustCompile(`^(.+?): (.*) - ([✔✓!✘✗⏸])\s*(.*)$`)

// ParseMCPList reads `claude mcp list`; userConfig is the user config
// file (.claude.json), nil when there is none.
func (a Adapter) ParseMCPList(out []byte, userConfig []byte) []cli.MCPServer {
	var cfg struct {
		MCPServers map[string]struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Env     map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	if len(userConfig) > 0 {
		json.Unmarshal(userConfig, &cfg) // a config the CLI cannot read either lists no user scope
	}
	servers := []cli.MCPServer{}
	for _, line := range strings.Split(cli.Plain(out), "\n") {
		m := listRE.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		s := cli.MCPServer{Name: m[1], Target: strings.TrimSpace(m[2])}
		switch {
		case strings.HasSuffix(s.Target, "(HTTP)"):
			s.Transport, s.Target = "http", strings.TrimSpace(strings.TrimSuffix(s.Target, "(HTTP)"))
		case strings.HasSuffix(s.Target, "(SSE)"):
			s.Transport, s.Target = "sse", strings.TrimSpace(strings.TrimSuffix(s.Target, "(SSE)"))
		case strings.HasPrefix(s.Target, "https://") || strings.HasPrefix(s.Target, "http://"):
			s.Transport = "http"
		default:
			s.Transport = "stdio"
		}
		rest := strings.TrimSpace(m[4])
		switch m[3] {
		case "✔", "✓":
			s.Status = cli.MCPConnected
		case "!":
			s.Status = cli.MCPNeedsAuth
		case "⏸":
			s.Status = cli.MCPPending
		case "✘", "✗":
			s.Status = cli.MCPFailed
			if _, why, ok := strings.Cut(rest, "—"); ok {
				s.Reason = cli.Clip(a.RedactMCP(strings.TrimSpace(why)), 240)
			}
		}
		if s.Status == cli.MCPNeedsAuth && !strings.Contains(strings.ToLower(rest), "auth") {
			s.Status = cli.MCPUnknown
		}
		switch {
		case strings.HasPrefix(s.Name, "claude.ai "):
			s.Scope = cli.MCPConnector
		case strings.HasPrefix(s.Name, "plugin:"):
			s.Scope = cli.MCPPlugin
		default:
			s.Scope = cli.MCPOther
			if c, ok := cfg.MCPServers[s.Name]; ok {
				s.Scope = cli.MCPUser
				if c.Type == "http" || c.Type == "sse" || c.Type == "stdio" {
					s.Transport = c.Type
				}
				s.Headers = slices.Sorted(maps.Keys(c.Headers))
				s.Env = slices.Sorted(maps.Keys(c.Env))
			}
		}
		s.Target = cli.Clip(a.RedactMCP(s.Target), 300)
		servers = append(servers, s)
	}
	return servers
}

var (
	// secretParamRE: a query parameter or a flag whose name says it holds a
	// secret, with its value (the code and state of a sign-in's redirect).
	secretParamRE = regexp.MustCompile(`(?i)([?&](?:[a-z_]*(?:key|token|secret|password|auth|sig|signature|code|state)[a-z_]*)=)[^&\s#]+`)
	secretFlagRE  = regexp.MustCompile(`(?i)(--?[a-z-]*(?:key|token|secret|password|auth)[a-z-]*(?:=|\s+))\S+`)
	secretVarRE   = regexp.MustCompile(`\b([A-Z_]*(?:KEY|TOKEN|SECRET|PASSWORD|PASS|AUTH)[A-Z_]*=)\S+`)
	bearerRE      = regexp.MustCompile(`(?i)(bearer\s+)\S+`)
	// longWordRE: a long run of letters and digits (a key) without
	// punctuation, so a store path's 32-character hash stays readable.
	longWordRE = regexp.MustCompile(`\b[A-Za-z0-9_]{40,}\b`)
)

// RedactMCP removes what may be a secret from an MCP line: tokens, the
// values of secret-looking parameters, flags and variables.
func (Adapter) RedactMCP(s string) string {
	s = tokenRE.ReplaceAllString(s, "••••")
	s = secretParamRE.ReplaceAllString(s, "${1}••••")
	s = secretFlagRE.ReplaceAllString(s, "${1}••••")
	s = secretVarRE.ReplaceAllString(s, "${1}••••")
	s = bearerRE.ReplaceAllString(s, "${1}••••")
	return longWordRE.ReplaceAllString(s, "••••")
}

var (
	anyURLRE      = regexp.MustCompile(`https?://\S+`)
	mcpLoginWords = []string{"error", "fail", "invalid", "denied", "not found", "no mcp server", "does not", "doesn't", "cannot", "can't", "unable", "timed out", "expired", "mismatch"}
)

// ParseMCPLogin reads `claude mcp login --no-browser` so far: the link
// after "Visit this URL to authorize", whether it asks for the redirect
// address, and the last error line.
func (a Adapter) ParseMCPLogin(b []byte, done bool) cli.LoginOut {
	var o cli.LoginOut
	text := cli.Plain(b)
	low := strings.ToLower(text)
	if i := strings.LastIndex(low, "this url to authorize"); i >= 0 {
		if u := anyURLRE.FindString(text[i:]); u != "" {
			o.URL = strings.TrimRight(u, ".,)")
		}
	}
	o.Prompt = strings.Contains(low, "redirect url")
	o.Line = cli.LastErrorLine(text, mcpLoginWords, a.RedactMCP)
	return o
}

var _ cli.MCPManager = Adapter{}
