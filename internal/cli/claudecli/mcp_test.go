package claudecli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/cli"
)

// V0.provider-mcp: `claude mcp list` as 2.1.327 prints it, with made-up
// servers: a claude.ai connector, a plugin's server, user servers over
// HTTP, SSE and stdio, one failing with a long reason, one pending.
const listOut = "Checking MCP server health\u2026\n\n" +
	"claude.ai Acme Docs: https://mcp.example.com/docs - \u2714 Connected\n" +
	"claude.ai Acme Mail: https://mail.example.com/mcp - ! Needs authentication\n" +
	"plugin:acme:tools: https://tools.example.com/mcp?client=claude-code-plugin (HTTP) - ! Needs authentication\n" +
	"tracker: https://tracker.example.com/mcp?api_key=sk-very-secret-value (HTTP) - \u2714 Connected\n" +
	"events: https://events.example.com/sse (SSE) - \u2714 Connected\n" +
	"browser: /opt/acme/bin/browser-mcp --token abc123secret  - \u2714 Connected\n" +
	"render: uvx render-mcp - \u2718 Failed to connect \u2014 ENOENT: Executable not found in $PATH: \"uvx\"\n" +
	"shared: npx shared-mcp - \u23f8 Pending approval\n"

const userConfig = `{"numStartups": 3, "mcpServers": {
	"tracker": {"type": "http", "url": "https://tracker.example.com/mcp", "headers": {"X-Api-Key": "hdr-secret", "Accept": "x"}},
	"events": {"type": "sse", "url": "https://events.example.com/sse"},
	"browser": {"type": "stdio", "command": "/opt/acme/bin/browser-mcp", "args": ["--token", "abc123secret"], "env": {"ACME_TOKEN": "env-secret"}},
	"render": {"type": "stdio", "command": "uvx", "args": ["render-mcp"]}
}}`

func TestParseMCPList(t *testing.T) {
	got := Adapter{}.ParseMCPList([]byte(listOut), []byte(userConfig))
	type row struct {
		name, scope, transport, target string
		status                         cli.MCPStatus
		reason                         string
	}
	want := []row{
		{"claude.ai Acme Docs", cli.MCPConnector, "http", "https://mcp.example.com/docs", cli.MCPConnected, ""},
		{"claude.ai Acme Mail", cli.MCPConnector, "http", "https://mail.example.com/mcp", cli.MCPNeedsAuth, ""},
		{"plugin:acme:tools", cli.MCPPlugin, "http", "https://tools.example.com/mcp?client=claude-code-plugin", cli.MCPNeedsAuth, ""},
		{"tracker", cli.MCPUser, "http", "https://tracker.example.com/mcp?api_key=••••", cli.MCPConnected, ""},
		{"events", cli.MCPUser, "sse", "https://events.example.com/sse", cli.MCPConnected, ""},
		{"browser", cli.MCPUser, "stdio", "/opt/acme/bin/browser-mcp --token ••••", cli.MCPConnected, ""},
		{"render", cli.MCPUser, "stdio", "uvx render-mcp", cli.MCPFailed, `ENOENT: Executable not found in $PATH: "uvx"`},
		{"shared", cli.MCPOther, "stdio", "npx shared-mcp", cli.MCPPending, ""},
	}
	if len(got) != len(want) {
		t.Fatalf("%d servers: %+v", len(got), got)
	}
	for i, w := range want {
		g := got[i]
		if (row{g.Name, g.Scope, g.Transport, g.Target, g.Status, g.Reason}) != w {
			t.Errorf("server %d:\n got  %+v\n want %+v", i, g, w)
		}
	}
	// The names of headers and variables, never their values.
	if !slices.Equal(got[3].Headers, []string{"Accept", "X-Api-Key"}) || !slices.Equal(got[5].Env, []string{"ACME_TOKEN"}) {
		t.Fatalf("names: %+v %+v", got[3], got[5])
	}
	b, _ := json.Marshal(got)
	for _, s := range []string{"hdr-secret", "env-secret", "abc123secret", "sk-very-secret"} {
		if strings.Contains(string(b), s) {
			t.Fatalf("%q in the list: %s", s, b)
		}
	}
	// No servers, and a config the CLI could not read either.
	if got := (Adapter{}).ParseMCPList([]byte("No MCP servers configured. Use `claude mcp add` to add a server.\n"), []byte("{")); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
	// Without a user config every other server is "other".
	if got := (Adapter{}).ParseMCPList([]byte(listOut), nil); got[3].Scope != cli.MCPOther || got[3].Transport != "http" {
		t.Fatalf("%+v", got[3])
	}
}

// Every command runs with what the provider's sessions get, minus what
// fyrd carries: the folder, the token, the key; no session markers, no
// display, no other account's variables.
func TestMCPCommandEnv(t *testing.T) {
	base := []string{"PATH=/bin", "HOME=/home/u", "CLAUDE_CONFIG_DIR=/daemon", "CLAUDE_CODE_OAUTH_TOKEN=daemon-tok", "ANTHROPIC_API_KEY=daemon-key",
		"CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDE_CODE_ENTRYPOINT=cli", "DISPLAY=:0"}
	env := func(l cli.Launch, op cli.MCPOp) (cli.MCPRun, map[string]string) {
		t.Helper()
		run, err := Adapter{}.MCPCommand(base, l, op)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		for _, kv := range run.Env {
			k, v, _ := strings.Cut(kv, "=")
			if _, dup := m[k]; dup {
				t.Fatalf("%s twice: %v", k, run.Env)
			}
			m[k] = v
		}
		for _, k := range []string{"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT", "DISPLAY"} {
			if _, ok := m[k]; ok {
				t.Fatalf("%s reached the command: %v", k, run.Env)
			}
		}
		if m["BROWSER"] != "true" || m["PATH"] != "/bin" {
			t.Fatalf("env %v", run.Env)
		}
		return run, m
	}
	// A folder login: its folder, no token.
	run, m := env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: "/home/u/.claude-work"}}, cli.MCPOp{Kind: "list"})
	if m["CLAUDE_CONFIG_DIR"] != "/home/u/.claude-work" || m["CLAUDE_CODE_OAUTH_TOKEN"] != "" || m["ANTHROPIC_API_KEY"] != "" || strings.Join(run.Args, " ") != "mcp list" || run.Binary != "claude" {
		t.Fatalf("folder: %+v %v", run, m)
	}
	// The default folder: no CLAUDE_CONFIG_DIR at all (the CLI then reads ~/.claude.json, not ~/.claude/.claude.json).
	_, m = env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin}}, cli.MCPOp{Kind: "list"})
	if _, ok := m["CLAUDE_CONFIG_DIR"]; ok {
		t.Fatalf("default folder: %v", m)
	}
	// A token login: its token (its claude.ai connectors are its account's).
	_, m = env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin}, Key: "tok-acme"}, cli.MCPOp{Kind: "logout", Name: "claude.ai Acme Mail"})
	if m["CLAUDE_CODE_OAUTH_TOKEN"] != "tok-acme" {
		t.Fatalf("token: %v", m)
	}
	// An endpoint: its URL and key.
	_, m = env(cli.Launch{Auth: cli.Auth{Kind: cli.APIKey}, Key: "k-1", Endpoint: cli.Endpoint{BaseURL: "https://api.example.com"}}, cli.MCPOp{Kind: "list"})
	if m["ANTHROPIC_BASE_URL"] != "https://api.example.com" || m["ANTHROPIC_AUTH_TOKEN"] != "k-1" {
		t.Fatalf("endpoint: %v", m)
	}
	if _, err := (Adapter{}).MCPCommand(base, cli.Launch{Auth: cli.Auth{Kind: cli.APIKey}}, cli.MCPOp{Kind: "list"}); err == nil {
		t.Fatal("an endpoint without its key ran")
	}
	// add: the JSON on stdin, never in the arguments.
	add := &cli.MCPAdd{Name: "tracker", Transport: "http", URL: "https://tracker.example.com/mcp", Headers: map[string]string{"Authorization": "Bearer hdr-secret"}}
	run, _ = env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin}, Binary: "/opt/claude"}, cli.MCPOp{Kind: "add", Add: add})
	if strings.Join(run.Args, " ") != "mcp add-json --scope user tracker -" || run.Binary != "/opt/claude" ||
		run.Stdin != `{"headers":{"Authorization":"Bearer hdr-secret"},"type":"http","url":"https://tracker.example.com/mcp"}` {
		t.Fatalf("add: %+v", run)
	}
	run, _ = env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin}}, cli.MCPOp{Kind: "login", Name: "claude.ai Acme Mail"})
	if strings.Join(run.Args, "|") != "mcp|login|--no-browser|claude.ai Acme Mail" || !run.Terminal {
		t.Fatalf("login: %+v", run)
	}
	run, _ = env(cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin}}, cli.MCPOp{Kind: "remove", Name: "tracker", Scope: cli.MCPUser})
	if strings.Join(run.Args, " ") != "mcp remove --scope user tracker" {
		t.Fatalf("remove: %+v", run)
	}
	for _, op := range []cli.MCPOp{
		{Kind: "remove", Name: "plugin:acme:tools", Scope: cli.MCPPlugin},
		{Kind: "remove", Name: "claude.ai Acme Mail", Scope: cli.MCPConnector},
		{Kind: "remove", Name: "x", Scope: cli.MCPOther},
		{Kind: "logout", Name: "--help"},
		{Kind: "add", Scope: cli.MCPLocal, Add: add},
		{Kind: "serve"},
	} {
		if _, err := (Adapter{}).MCPCommand(base, cli.Launch{Auth: cli.Auth{Kind: cli.CLILogin}}, op); err == nil {
			t.Errorf("%+v ran", op)
		}
	}
}

func TestMCPJSON(t *testing.T) {
	js, err := MCPJSON(cli.MCPAdd{Name: "render", Transport: "stdio", Command: "uvx", Args: []string{"render-mcp", "--port", "0"}, Env: map[string]string{"ACME_KEY": "v"}})
	if err != nil || js != `{"args":["render-mcp","--port","0"],"command":"uvx","env":{"ACME_KEY":"v"},"type":"stdio"}` {
		t.Fatalf("%s %v", js, err)
	}
	if js, _ := MCPJSON(cli.MCPAdd{Name: "a", Transport: "stdio", Command: "x"}); js != `{"args":[],"command":"x","type":"stdio"}` {
		t.Fatal(js)
	}
	for _, bad := range []cli.MCPAdd{
		{Name: "-x", Transport: "stdio", Command: "x"},
		{Name: "has space", Transport: "stdio", Command: "x"},
		{Name: "a", Transport: "stdio"},
		{Name: "a", Transport: "http", URL: "ftp://example.com"},
		{Name: "a", Transport: "http", URL: "https://example.com", Headers: map[string]string{"Bad Name": "v"}},
		{Name: "a", Transport: "http", URL: "https://example.com", Headers: map[string]string{"X": "a\r\nInjected: 1"}},
		{Name: "a", Transport: "stdio", Command: "x", Env: map[string]string{"1BAD": "v"}},
		{Name: "a", Transport: "ws", URL: "https://example.com"},
	} {
		if _, err := MCPJSON(bad); err == nil {
			t.Errorf("%+v was taken", bad)
		}
	}
}

func TestRedactMCP(t *testing.T) {
	a := Adapter{}
	for in, want := range map[string]string{
		"http://localhost:41234/callback?code=abc-123&state=xyz":            "http://localhost:41234/callback?code=••••&state=••••",
		"Authorization: Bearer abcdef":                                      "Authorization: Bearer ••••",
		"server --api-key=k1 --name x":                                      "server --api-key=•••• --name x",
		"env ACME_TOKEN=t1 PORT=8":                                          "env ACME_TOKEN=•••• PORT=8",
		"/nix/store/0123456789abcdfghijklmnpqrsvwxyz-acme-1.0/bin/acme mcp": "/nix/store/0123456789abcdfghijklmnpqrsvwxyz-acme-1.0/bin/acme mcp",
		"key " + strings.Repeat("A1", 25):                                   "key ••••",
	} {
		if got := a.RedactMCP(in); got != want {
			t.Errorf("RedactMCP(%q) = %q, want %q", in, got, want)
		}
	}
}

// `claude mcp login --no-browser` on a terminal: the link in an OSC 8
// hyperlink whose visible text wraps, then the prompt for the redirect.
func TestParseMCPLogin(t *testing.T) {
	link := "https://auth.example.com/authorize?response_type=code&client_id=acme&redirect_uri=http%3A%2F%2Flocalhost%3A41234%2Fcallback&state=s1"
	out := "Visit\x1b[1Cthis\x1b[1CURL\x1b[1Cto\x1b[1Cauthorize:\r\n" +
		"\x1b]8;;" + link + "\x07" + link[:50] + "\x1b]8;;\x07\r\n" +
		"\x1b]8;;" + link + "\x07" + link[50:] + "\x1b]8;;\x07\r\n\r\nWaiting for authorization\u2026\r\nOr paste the redirect URL here: "
	o := Adapter{}.ParseMCPLogin([]byte(out), false)
	if o.URL != link || !o.Prompt || o.Line != "" || o.Token != "" {
		t.Fatalf("%+v", o)
	}
	o = Adapter{}.ParseMCPLogin([]byte(out+"http://localhost:41234/callback?code=c0de&state=bad\r\nError: OAuth state mismatch for http://localhost:41234/callback?code=c0de&state=bad\r\n"), true)
	if !strings.Contains(o.Line, "state mismatch") || strings.Contains(o.Line, "c0de") {
		t.Fatalf("line %q", o.Line)
	}
	if o := (Adapter{}).ParseMCPLogin([]byte("Checking https://example.com\n"), false); o.URL != "" {
		t.Fatalf("a link before the prompt was taken: %+v", o)
	}
}

func TestMCPConfigFile(t *testing.T) {
	a := Adapter{}
	if f := a.MCPConfigFile(cli.Launch{}, "/home/u"); f != "/home/u/.claude.json" {
		t.Fatal(f)
	}
	if f := a.MCPConfigFile(cli.Launch{Auth: cli.Auth{ConfigDir: "~/.claude-work"}}, "/home/u"); f != "/home/u/.claude-work/.claude.json" {
		t.Fatal(f)
	}
}
