package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/agycli"
)

// V0.provider-mcp: the fake `claude mcp` (FYR_FAKE_LOGIN=mcp). It keeps
// user servers in the .claude.json the real CLI reads (CLAUDE_CONFIG_DIR's,
// else the home folder's), lists them with a connector of the account its
// token names and a plugin's server, and signs in like `mcp login
// --no-browser`. Each run appends its arguments and environment to
// FYR_FAKE_LOG.
const (
	mcpHeaderSecret = "hdr-secret-123"
	mcpGoodCode     = "good-mcp-code"
)

func fakeMCP() {
	args := os.Args[1:]
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	cfgFile := filepath.Join(os.Getenv("HOME"), ".claude.json")
	if dir != "" {
		cfgFile = filepath.Join(dir, ".claude.json")
	}
	tok := os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")
	if f := os.Getenv("FYR_FAKE_LOG"); f != "" {
		wd, _ := os.Getwd()
		lf, _ := os.OpenFile(f, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		fmt.Fprintf(lf, "args=%s config=%s oauth=%s browser=%s session=%s cwd=%s\n", strings.Join(args, "|"), dir, tok, os.Getenv("BROWSER"), os.Getenv("CLAUDECODE"), wd)
		lf.Close()
	}
	cfg := map[string]any{}
	if b, err := os.ReadFile(cfgFile); err == nil {
		json.Unmarshal(b, &cfg)
	}
	servers, _ := cfg["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	save := func() {
		cfg["mcpServers"] = servers
		b, _ := json.Marshal(cfg)
		os.WriteFile(cfgFile, b, 0o600)
	}
	signed := func(name string) string { return cfgFile + ".signed-" + name }
	if len(args) < 2 || args[0] != "mcp" {
		os.Exit(2)
	}
	switch args[1] {
	case "list":
		fmt.Print("Checking MCP server health…\n\n")
		if tok == "tok-acme" {
			fmt.Println("claude.ai Acme Mail: https://mail.example.com/mcp - ! Needs authentication")
		} else {
			fmt.Println("claude.ai Acme Docs: https://docs.example.com/mcp - ✔ Connected")
		}
		fmt.Println("plugin:acme:tools: https://tools.example.com/mcp (HTTP) - ! Needs authentication")
		names := make([]string, 0, len(servers))
		for n := range servers {
			names = append(names, n)
		}
		slices.Sort(names)
		for _, n := range names {
			s, _ := servers[n].(map[string]any)
			u, _ := s["url"].(string)
			st := "✔ Connected"
			if _, err := os.Stat(signed(n)); err != nil && strings.Contains(u, "/authed/") {
				st = "! Needs authentication"
			}
			fmt.Printf("%s: %s (HTTP) - %s\n", n, u, st)
		}
	case "add-json":
		if len(args) != 6 || args[2] != "--scope" || args[3] != "user" || args[5] != "-" {
			os.Exit(3)
		}
		var s map[string]any
		if err := json.NewDecoder(os.Stdin).Decode(&s); err != nil {
			fmt.Println("Invalid configuration: not a JSON object.")
			os.Exit(1)
		}
		if _, ok := servers[args[4]]; ok {
			fmt.Fprintf(os.Stderr, "MCP server %s already exists in user config\n", args[4])
			os.Exit(1)
		}
		servers[args[4]] = s
		save()
		fmt.Printf("Added %s MCP server %s to user config\n", s["type"], args[4])
	case "remove":
		name := args[len(args)-1]
		if _, ok := servers[name]; !ok {
			fmt.Fprintf(os.Stderr, "No MCP server named %q in user scope\n", name)
			os.Exit(1)
		}
		delete(servers, name)
		save()
	case "logout":
		os.Remove(signed(args[2]))
		fmt.Printf("Signed out of %q.\n", args[2])
	case "login":
		if args[2] != "--no-browser" {
			os.Exit(3)
		}
		name := args[3]
		link := "https://auth.example.com/authorize?response_type=code&client_id=acme&redirect_uri=http%3A%2F%2Flocalhost%3A41234%2Fcallback&state=s1"
		fmt.Print("Visit this URL to authorize:\r\n")
		fmt.Printf("\x1b]8;;%s\x07%s\x1b]8;;\x07\r\n", link, link[:40])
		fmt.Printf("\x1b]8;;%s\x07%s\x1b]8;;\x07\r\n\r\n", link, link[40:])
		fmt.Print("Waiting for authorization…\r\nOr paste the redirect URL here: ")
		in := bufio.NewScanner(os.Stdin)
		if !in.Scan() {
			os.Exit(1)
		}
		if !strings.Contains(in.Text(), "code="+mcpGoodCode) {
			fmt.Printf("\r\nError: OAuth state mismatch for %s\r\n", strings.TrimSpace(in.Text()))
			os.Exit(1)
		}
		os.WriteFile(signed(name), nil, 0o600)
		fmt.Printf("\r\nAuthentication successful. Connected to %s.\r\n", name)
	default:
		os.Exit(3)
	}
}

func (lf *loginFix) waitMCP(t *testing.T, id, what string, ok func(*MCPDTO) bool) *MCPDTO {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if d := lf.dto(id); d.MCP != nil && ok(d.MCP) {
			return d.MCP
		}
	}
	t.Fatalf("%s never got %s: %+v", id, what, lf.dto(id).MCP)
	return nil
}

func server(m *MCPDTO, name string) (cli.MCPServer, bool) {
	for _, s := range m.Servers {
		if s.Name == name {
			return s, true
		}
	}
	return cli.MCPServer{}, false
}

func listed(m *MCPDTO, name string, status cli.MCPStatus) bool {
	s, ok := server(m, name)
	return !m.Listing && ok && s.Status == status
}

// V0.provider-mcp: a Claude provider's MCP section. Each command runs the
// provider's CLI with its sessions' environment; providers on one config
// share its servers (and say so) but list their own account's connectors;
// a server is added with its header on stdin only, signed in to with the
// pasted redirect address, signed out of and removed; a plugin's server is
// not removed; an Antigravity provider has no section.
func TestProviderMCP(t *testing.T) {
	lf := newLoginFix(t, "mcp")
	r := lf.r
	r.MCPDir = t.TempDir()
	r.LoginEnv = append(r.LoginEnv, "HOME="+lf.f.home, "CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1")
	for _, p := range []Provider{
		{ID: "main", Name: "Claude", CLI: "claude", Auth: cli.Auth{Kind: cli.CLILogin}},
		{ID: "tok", Name: "Claude (acme)", CLI: "claude", TokenLogin: true, Auth: cli.Auth{Kind: cli.CLILogin, Secret: SecretName("tok")}},
		{ID: "work", Name: "Claude (work)", CLI: "claude", Auth: cli.Auth{Kind: cli.CLILogin, ConfigDir: "~/.claude-work"}},
		{ID: "ag", Name: "Antigravity", CLI: agycli.ID, Auth: cli.Auth{Kind: cli.CLILogin}},
	} {
		p.Models = []cli.Model{}
		if err := r.add(p); err != nil {
			t.Fatal(err)
		}
	}
	r.Secrets.Put(SecretName("tok"), "tok-acme")

	// Before any list: the section, its config and who shares it.
	m := lf.dto("main").MCP
	if m == nil || len(m.Servers) != 0 || m.CheckedAt != nil || m.Config != "~/.claude.json" || !slices.Equal(m.SharedWith, []string{"Claude (acme)"}) {
		t.Fatalf("main before: %+v", m)
	}
	if w := lf.dto("work").MCP; w == nil || w.Config != "~/.claude-work/.claude.json" || len(w.SharedWith) != 0 {
		t.Fatalf("work: %+v", w)
	}
	if d := lf.dto("ag"); d.MCP != nil {
		t.Fatalf("an agy provider has an MCP section: %+v", d.MCP)
	}
	if rep := lf.cmd("provider.mcpList", map[string]any{"id": "ag"}); rep.OK || rep.Status != 409 {
		t.Fatalf("agy list: %+v", rep)
	}

	// Add: the reply waits for the CLI; the list runs again by itself.
	if rep := lf.cmd("provider.mcpAdd", map[string]any{"id": "main", "name": "tracker", "transport": "http", "url": "https://tracker.example.com/authed/mcp",
		"headers": map[string]any{"Authorization": "Bearer " + mcpHeaderSecret}}); !rep.OK {
		t.Fatalf("add: %+v", rep)
	}
	m = lf.waitMCP(t, "main", "tracker", func(m *MCPDTO) bool { return listed(m, "tracker", cli.MCPNeedsAuth) })
	tr, _ := server(m, "tracker")
	if tr.Scope != cli.MCPUser || tr.Transport != "http" || !slices.Equal(tr.Headers, []string{"Authorization"}) || m.CheckedAt == nil || m.Error != "" {
		t.Fatalf("tracker: %+v in %+v", tr, m)
	}
	if s, ok := server(m, "claude.ai Acme Docs"); !ok || s.Scope != cli.MCPConnector || s.Status != cli.MCPConnected {
		t.Fatalf("connector: %+v", m.Servers)
	}
	if b, _ := os.ReadFile(filepath.Join(lf.f.home, ".claude.json")); !strings.Contains(string(b), mcpHeaderSecret) {
		t.Fatalf("the CLI did not get the header: %s", b)
	}
	if rep := lf.cmd("provider.mcpAdd", map[string]any{"id": "main", "name": "tracker", "url": "https://tracker.example.com/authed/mcp"}); rep.OK || !strings.Contains(rep.Error, "already exists") {
		t.Fatalf("a second add: %+v", rep)
	}
	if rep := lf.cmd("provider.mcpAdd", map[string]any{"id": "main", "name": "-x", "command": "x"}); rep.OK {
		t.Fatalf("a bad name: %+v", rep)
	}

	// The token provider shares the folder's servers, with its own account's connector.
	if rep := lf.cmd("provider.mcpList", map[string]any{"id": "tok"}); !rep.OK {
		t.Fatalf("list: %+v", rep)
	}
	tm := lf.waitMCP(t, "tok", "its list", func(m *MCPDTO) bool { return listed(m, "tracker", cli.MCPNeedsAuth) })
	if _, ok := server(tm, "claude.ai Acme Mail"); !ok {
		t.Fatalf("tok's connectors: %+v", tm.Servers)
	}
	if _, ok := server(tm, "claude.ai Acme Docs"); ok {
		t.Fatalf("tok listed the folder's account's connector: %+v", tm.Servers)
	}
	lf.cmd("provider.mcpList", map[string]any{"id": "work"})
	wm := lf.waitMCP(t, "work", "its list", func(m *MCPDTO) bool { return m.CheckedAt != nil && !m.Listing })
	if _, ok := server(wm, "tracker"); ok {
		t.Fatalf("work sees main's servers: %+v", wm.Servers)
	}

	// A plugin's server and one not listed are not removed.
	if rep := lf.cmd("provider.mcpRemove", map[string]any{"id": "main", "name": "plugin:acme:tools"}); rep.OK || !strings.Contains(rep.Error, "plugin") {
		t.Fatalf("plugin remove: %+v", rep)
	}
	if rep := lf.cmd("provider.mcpRemove", map[string]any{"id": "main", "name": "nope"}); rep.OK || rep.Status != 409 {
		t.Fatalf("unknown remove: %+v", rep)
	}

	// Sign in: the link, a wrong redirect fails with the CLI's line (redacted), a second start waits, the right one signs in.
	if rep := lf.cmd("provider.mcpLogin", map[string]any{"id": "main", "name": "tracker"}); !rep.OK {
		t.Fatalf("login: %+v", rep)
	}
	if rep := lf.cmd("provider.mcpLogin", map[string]any{"id": "main", "name": "tracker"}); rep.OK || rep.Status != 409 {
		t.Fatalf("a second sign-in: %+v", rep)
	}
	m = lf.waitMCP(t, "main", "the link", func(m *MCPDTO) bool { return m.Login != nil && m.Login.State == LoginWaiting })
	if m.Login.Kind != loginMCP || m.Login.Server != "tracker" || !strings.HasPrefix(m.Login.URL, "https://auth.example.com/authorize?") || !strings.HasSuffix(m.Login.URL, "state=s1") {
		t.Fatalf("login: %+v", m.Login)
	}
	if d := lf.dto("main"); d.Login != nil {
		t.Fatalf("the MCP sign-in shows as the account's login: %+v", d.Login)
	}
	if rep := lf.cmd("provider.mcpLoginCode", map[string]any{"id": "main", "code": "http://localhost:41234/callback?code=wrong-one&state=s9"}); !rep.OK {
		t.Fatalf("code: %+v", rep)
	}
	m = lf.waitMCP(t, "main", "failed", func(m *MCPDTO) bool { return m.Login != nil && m.Login.State == LoginFailed })
	if !strings.Contains(m.Login.Message, "state mismatch") || strings.Contains(m.Login.Message, "wrong-one") {
		t.Fatalf("failed: %q", m.Login.Message)
	}
	lf.cmd("provider.mcpLogin", map[string]any{"id": "main", "name": "tracker"})
	lf.waitMCP(t, "main", "the link again", func(m *MCPDTO) bool { return m.Login != nil && m.Login.State == LoginWaiting })
	if rep := lf.cmd("provider.mcpLoginCode", map[string]any{"id": "main", "code": " http://localhost:41234/callback?code=" + mcpGoodCode + "&state=s1 "}); !rep.OK {
		t.Fatalf("code: %+v", rep)
	}
	lf.waitMCP(t, "main", "signed in", func(m *MCPDTO) bool {
		return m.Login != nil && m.Login.State == LoginDone && listed(m, "tracker", cli.MCPConnected)
	})

	// Cancel ends a sign-in that waits.
	lf.cmd("provider.mcpLogin", map[string]any{"id": "main", "name": "claude.ai Acme Docs"})
	lf.waitMCP(t, "main", "the link", func(m *MCPDTO) bool { return m.Login != nil && m.Login.State == LoginWaiting })
	lf.cmd("provider.mcpLoginCancel", map[string]any{"id": "main"})
	lf.waitMCP(t, "main", "cancelled", func(m *MCPDTO) bool { return m.Login != nil && m.Login.State == LoginCancelled })

	// Sign out, then remove.
	if rep := lf.cmd("provider.mcpLogout", map[string]any{"id": "main", "name": "tracker"}); !rep.OK {
		t.Fatalf("logout: %+v", rep)
	}
	lf.waitMCP(t, "main", "signed out", func(m *MCPDTO) bool { return listed(m, "tracker", cli.MCPNeedsAuth) })
	if rep := lf.cmd("provider.mcpRemove", map[string]any{"id": "main", "name": "tracker"}); !rep.OK {
		t.Fatalf("remove: %+v", rep)
	}
	lf.waitMCP(t, "main", "removed", func(m *MCPDTO) bool { _, ok := server(m, "tracker"); return !m.Listing && !ok })

	// Every run had its provider's environment and none of fyrd's session or account.
	b, err := os.ReadFile(lf.fakeLog)
	if err != nil {
		t.Fatal(err)
	}
	runs := strings.Split(strings.TrimSpace(string(b)), "\n")
	saw := map[string]bool{}
	for _, l := range runs {
		if strings.Contains(l, "session=1") || strings.Contains(l, "daemon-token") || !strings.Contains(l, "browser=true") || !strings.Contains(l, "cwd="+r.MCPDir) {
			t.Fatalf("a run's environment: %s", l)
		}
		switch {
		case strings.Contains(l, "oauth=tok-acme"):
			saw["tok"] = strings.Contains(l, "config= ")
		case strings.Contains(l, "config="+filepath.Join(lf.f.home, ".claude-work")+" "):
			saw["work"] = true
		case strings.Contains(l, "config= oauth= "):
			saw["main"] = true
		}
	}
	if !saw["tok"] || !saw["work"] || !saw["main"] {
		t.Fatalf("the runs: %v\n%s", saw, b)
	}
	// No header value or pasted code in the arguments, a log line, a reply or a DTO.
	all := string(b) + lf.logs.String() + strings.Join(lf.replies, "\n")
	for _, s := range []string{mcpHeaderSecret, mcpGoodCode, "wrong-one"} {
		if strings.Contains(all, s) {
			t.Fatalf("%q leaked", s)
		}
	}

	// Removing the provider ends its section.
	if rep := lf.cmd("provider.delete", map[string]any{"id": "work"}); !rep.OK {
		t.Fatalf("delete: %+v", rep)
	}
	r.mmu.Lock()
	_, kept := r.mcp["work"]
	r.mmu.Unlock()
	if kept {
		t.Fatal("the MCP state outlived its provider")
	}
}

// A list that hangs ends at its timeout with the reason, and the page can
// ask again.
func TestProviderMCPListTimeout(t *testing.T) {
	lf := newLoginFix(t, "mcp")
	r := lf.r
	r.MCPDir = t.TempDir()
	r.MCPListTimeout = 300 * time.Millisecond
	bin := filepath.Join(t.TempDir(), "claude")
	os.WriteFile(bin, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755)
	r.LoginBinary = bin
	r.add(Provider{ID: "main", Name: "Claude", CLI: "claude", Auth: cli.Auth{Kind: cli.CLILogin}, Models: []cli.Model{}})
	start := time.Now()
	if rep := lf.cmd("provider.mcpList", map[string]any{"id": "main"}); !rep.OK || time.Since(start) > 2*time.Second {
		t.Fatalf("the list blocked the command: %+v %s", rep, time.Since(start))
	}
	if m := lf.dto("main").MCP; !m.Listing {
		t.Fatalf("not listing: %+v", m)
	}
	m := lf.waitMCP(t, "main", "the timeout", func(m *MCPDTO) bool { return !m.Listing && m.Error != "" })
	if !strings.Contains(m.Error, "did not answer in time") || m.CheckedAt == nil {
		t.Fatalf("%+v", m)
	}
}
