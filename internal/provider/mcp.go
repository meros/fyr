package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/hostfs"
	"github.com/meros/fyr/internal/proc"
)

// V0.provider-mcp: a provider's MCP servers in Settings. Every command runs
// the provider's own CLI (cli.MCPManager) with the environment its sessions
// get, so it reads and changes the config those sessions start with. The
// list health-checks every server (10-30 s), so it runs on its own
// goroutine on the owner's press and the page shows the last answer with
// its time; add, remove and sign-out are quick and the reply waits for
// them. A sign-in runs like the account's login (login.go): the page shows
// the link and takes the address the browser was sent back to.

const (
	// mcpListTimeout ends a list whose health check hangs.
	mcpListTimeout = 90 * time.Second
	// mcpCmdTimeout ends an add, remove or sign-out.
	mcpCmdTimeout = 30 * time.Second
	// loginMCP is the kind of an MCP server's sign-in.
	loginMCP = "mcp"
)

// MCPDTO is a provider's MCP section: the last list, when it ran, why it
// failed, whether one runs now, the config it reads, the other providers
// that read the same config, and a sign-in that runs or just ended.
type MCPDTO struct {
	Servers   []cli.MCPServer `json:"servers"`
	CheckedAt *time.Time      `json:"checkedAt,omitempty"`
	Error     string          `json:"error,omitempty"`
	Listing   bool            `json:"listing,omitempty"`
	// Config: the CLI's user config file these servers are kept in.
	Config string `json:"config"`
	// SharedWith: the names of the other providers on the same config,
	// which see the same servers and sign-ins (a claude.ai connector is the
	// account's, so it may differ).
	SharedWith []string  `json:"sharedWith,omitempty"`
	Login      *LoginDTO `json:"login,omitempty"`
}

type mcpState struct {
	servers []cli.MCPServer
	at      time.Time
	err     string
	listing bool
	// again: a change landed while a list ran, which may have missed it,
	// so another list follows.
	again bool
}

func mcpKey(id string) string { return "mcp:" + id } // a provider id has no ':'

func mcpManager(p Provider) (cli.MCPManager, bool) {
	m, ok := AdapterFor(p.CLI).(cli.MCPManager)
	return m, ok
}

// mcpLaunch is what the provider's sessions start with, for its MCP
// commands: its config folder, its token or key, its endpoint.
func (r *Registry) mcpLaunch(p Provider) cli.Launch {
	l := cli.Launch{Binary: r.LoginBinary, Auth: p.Auth, Endpoint: p.Endpoint}
	if d := p.Auth.ConfigDir; d != "" {
		l.Auth.ConfigDir = filepath.Clean(hostfs.ExpandHome(d, r.Home))
	}
	if key, err := r.key(p); err == nil {
		l.Key = key // as a session's launch takes it (agent.go)
	}
	return l
}

// mcpConfig is the user config file the provider's CLI reads.
func (r *Registry) mcpConfig(p Provider, m cli.MCPManager) string {
	return m.MCPConfigFile(r.mcpLaunch(p), r.Home)
}

// mcpCwd is where the commands run: a private folder of their own, so no
// project's local or .mcp.json servers mix in and the CLI's per-folder
// state stays in one entry.
func (r *Registry) mcpCwd() (string, error) {
	d := r.MCPDir
	if d == "" {
		c, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		d = filepath.Join(c, "fyr", "mcp")
	}
	return d, os.MkdirAll(d, 0o700)
}

func (r *Registry) mcpBase() []string {
	if r.LoginEnv != nil {
		return r.LoginEnv
	}
	return os.Environ()
}

// runMCP runs one command and returns its output; an error carries the
// CLI's own words, redacted.
func (r *Registry) runMCP(ctx context.Context, p Provider, m cli.MCPManager, op cli.MCPOp) ([]byte, error) {
	run, err := m.MCPCommand(r.mcpBase(), r.mcpLaunch(p), op)
	if err != nil {
		return nil, err
	}
	cwd, err := r.mcpCwd()
	if err != nil {
		return nil, fmt.Errorf("could not make a folder for the MCP commands: %w", err)
	}
	cmd := proc.Command(ctx, run.Binary, run.Args...)
	cmd.Env, cmd.Dir = run.Env, cwd
	cmd.Stdin = strings.NewReader(run.Stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	if err == nil {
		return out.Bytes(), nil
	}
	if ctx.Err() != nil {
		return out.Bytes(), fmt.Errorf("claude mcp %s did not answer in time", op.Kind)
	}
	if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "executable file not found") {
		return nil, errors.New(filepath.Base(run.Binary) + " is not installed on this host")
	}
	return out.Bytes(), errors.New(cli.Clip(m.RedactMCP(cli.ErrText(lastLines(cli.Plain(out.Bytes()), 3), err.Error())), 300))
}

// lastLines are the last n lines of text that hold anything.
func lastLines(text string, n int) string {
	var keep []string
	lines := strings.Split(text, "\n")
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
		if l := strings.Join(strings.Fields(lines[i]), " "); l != "" {
			keep = append([]string{l}, keep...)
		}
	}
	return strings.Join(keep, " ")
}

func (r *Registry) mcpState(id string) *mcpState {
	r.mmu.Lock()
	defer r.mmu.Unlock()
	if r.mcp == nil {
		r.mcp = map[string]*mcpState{}
	}
	s := r.mcp[id]
	if s == nil {
		s = &mcpState{}
		r.mcp[id] = s
	}
	return s
}

// mcpFor is the provider and its MCP manager, or the reply that says why not.
func (r *Registry) mcpFor(id string) (Provider, cli.MCPManager, *api.Reply) {
	p, ok := r.get(id)
	if !ok {
		return p, nil, &api.Reply{Error: "no provider " + id, Status: 404}
	}
	m, ok := mcpManager(p)
	if !ok {
		return p, nil, &api.Reply{Error: p.Name + " has no MCP servers Fyr can manage", Status: 409}
	}
	return p, m, nil
}

// mcpList runs provider.mcpList: it starts a list and answers at once.
func (r *Registry) mcpList(id string) api.Reply {
	p, m, rep := r.mcpFor(id)
	if rep != nil {
		return *rep
	}
	r.startMCPList(p, m, false)
	return api.Reply{}
}

// startMCPList lists the provider's servers on its own goroutine. A list
// that runs already is the answer to a press; after a change (again) one
// more list follows it, since the running one may have read the config
// before the change.
func (r *Registry) startMCPList(p Provider, m cli.MCPManager, afterChange bool) {
	st := r.mcpState(p.ID)
	r.mmu.Lock()
	if st.listing {
		st.again = st.again || afterChange
		r.mmu.Unlock()
		return
	}
	st.listing = true
	r.mmu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), r.mcpListTimeout())
		defer cancel()
		out, err := r.runMCP(ctx, p, m, cli.MCPOp{Kind: "list"})
		cfg, _ := os.ReadFile(r.mcpConfig(p, m))
		servers := m.ParseMCPList(out, cfg)
		clear(cfg) // it may hold header and env values
		r.mmu.Lock()
		st.listing, st.at = false, time.Now().UTC()
		if err != nil && len(servers) == 0 {
			st.err = err.Error()
		} else {
			st.servers, st.err = servers, ""
		}
		again := st.again
		st.again = false
		r.mmu.Unlock()
		if err != nil {
			r.logf("providers: mcp list for %s failed: %s", p.ID, err)
		} else {
			r.logf("providers: mcp list for %s: %d servers", p.ID, len(servers))
		}
		if again {
			r.startMCPList(p, m, false)
		}
		r.changed()
	}()
}

func (r *Registry) mcpListTimeout() time.Duration {
	if r.MCPListTimeout > 0 {
		return r.MCPListTimeout
	}
	return mcpListTimeout
}

// mcpChange runs a quick command (add, remove, sign-out), then lists again.
func (r *Registry) mcpChange(p Provider, m cli.MCPManager, op cli.MCPOp) api.Reply {
	ctx, cancel := context.WithTimeout(context.Background(), mcpCmdTimeout)
	defer cancel()
	_, err := r.runMCP(ctx, p, m, op)
	if err != nil {
		r.logf("providers: mcp %s %s for %s failed: %s", op.Kind, op.Name, p.ID, err)
		return fail(err)
	}
	r.logf("providers: mcp %s %s for %s", op.Kind, op.Name, p.ID)
	r.startMCPList(p, m, true)
	return api.Reply{}
}

// mcpAdd runs provider.mcpAdd: a server in the user config.
func (r *Registry) mcpAdd(id string, a core.Args) api.Reply {
	p, m, rep := r.mcpFor(id)
	if rep != nil {
		return *rep
	}
	add := cli.MCPAdd{Name: strings.TrimSpace(a.Str("name")), Transport: strings.TrimSpace(a.Str("transport")),
		URL: strings.TrimSpace(a.Str("url")), Command: strings.TrimSpace(a.Str("command")),
		Headers: strMap(a["headers"]), Env: strMap(a["env"])}
	if raw, ok := a["args"].([]any); ok {
		for _, x := range raw {
			if s, ok := x.(string); ok {
				add.Args = append(add.Args, s)
			}
		}
	}
	if add.Transport == "" {
		add.Transport = "stdio"
		if add.URL != "" {
			add.Transport = "http"
		}
	}
	return r.mcpChange(p, m, cli.MCPOp{Kind: "add", Name: add.Name, Scope: cli.MCPUser, Add: &add})
}

func strMap(v any) map[string]string {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, x := range m {
		if s, ok := x.(string); ok && strings.TrimSpace(k) != "" {
			out[strings.TrimSpace(k)] = s
		}
	}
	return out
}

// listed is a server of the provider's last list.
func (r *Registry) listed(id, name string) (cli.MCPServer, bool) {
	st := r.mcpState(id)
	r.mmu.Lock()
	defer r.mmu.Unlock()
	for _, s := range st.servers {
		if s.Name == name {
			return s, true
		}
	}
	return cli.MCPServer{}, false
}

// mcpRemove runs provider.mcpRemove: the scope is the one the last list
// found, never the client's word.
func (r *Registry) mcpRemove(id, name string) api.Reply {
	p, m, rep := r.mcpFor(id)
	if rep != nil {
		return *rep
	}
	s, ok := r.listed(id, name)
	if !ok {
		return api.Reply{Error: "No server " + name + " in the last list. Check again first.", Status: 409}
	}
	return r.mcpChange(p, m, cli.MCPOp{Kind: "remove", Name: name, Scope: s.Scope})
}

// mcpLogout runs provider.mcpLogout.
func (r *Registry) mcpLogout(id, name string) api.Reply {
	p, m, rep := r.mcpFor(id)
	if rep != nil {
		return *rep
	}
	if _, ok := r.listed(id, name); !ok {
		return api.Reply{Error: "No server " + name + " in the last list. Check again first.", Status: 409}
	}
	return r.mcpChange(p, m, cli.MCPOp{Kind: "logout", Name: name})
}

// mcpLogin runs provider.mcpLogin: the server's sign-in on a terminal,
// one per provider at a time.
func (r *Registry) mcpLogin(id, name string) api.Reply {
	p, m, rep := r.mcpFor(id)
	if rep != nil {
		return *rep
	}
	if _, ok := r.listed(id, name); !ok {
		return api.Reply{Error: "No server " + name + " in the last list. Check again first.", Status: 409}
	}
	run, err := m.MCPCommand(r.mcpBase(), r.mcpLaunch(p), cli.MCPOp{Kind: "login", Name: name})
	if err != nil {
		return fail(err)
	}
	cwd, err := r.mcpCwd()
	if err != nil {
		return fail(err)
	}
	key := mcpKey(id)
	r.lmu.Lock()
	if r.logins == nil {
		r.logins = map[string]*loginRun{}
	}
	if l := r.logins[key]; l != nil && l.active() {
		r.lmu.Unlock()
		return api.Reply{Error: "A sign-in to " + l.server + " is already running; finish or cancel it first", Status: 409}
	}
	l := &loginRun{kind: loginMCP, server: name, parse: m.ParseMCPLogin, redact: m.RedactMCP, state: LoginStarting,
		expires: time.Now().Add(r.loginTimeout())}
	l.onEnd = func(state string) {
		if state == LoginDone {
			r.startMCPList(p, m, true)
		}
	}
	r.logins[key] = l
	r.lmu.Unlock()
	if err := r.startLoginProc(key, l, run.Binary, run.Args, run.Env, run.Terminal, cwd, ""); err != nil {
		l.mu.Lock()
		l.state, l.msg, l.ended = LoginFailed, err.Error(), time.Now()
		l.mu.Unlock()
		return fail(err)
	}
	r.logf("providers: mcp sign-in to %s for %s started", name, id)
	return api.Reply{Result: l.dto()}
}

// mcpDTO is the provider's MCP section, nil when its CLI has none.
func (r *Registry) mcpDTO(p Provider, all []Provider) *MCPDTO {
	m, ok := mcpManager(p)
	if !ok {
		return nil
	}
	cfg := r.mcpConfig(p, m)
	d := &MCPDTO{Servers: []cli.MCPServer{}, Config: tildeHomeFor(cfg, r.Home, "")}
	for _, x := range all {
		if x.ID == p.ID {
			continue
		}
		if xm, ok := mcpManager(x); ok && r.mcpConfig(x, xm) == cfg {
			d.SharedWith = append(d.SharedWith, x.Name)
		}
	}
	st := r.mcpState(p.ID)
	r.mmu.Lock()
	if !st.at.IsZero() {
		t := st.at
		d.CheckedAt = &t
	}
	d.Servers = append(d.Servers, st.servers...)
	d.Error, d.Listing = st.err, st.listing
	r.mmu.Unlock()
	d.Login = r.loginFor(mcpKey(p.ID))
	return d
}

// dropMCP forgets a removed provider's MCP state and ends its sign-in.
func (r *Registry) dropMCP(id string) {
	r.cancelLogin(mcpKey(id))
	r.mmu.Lock()
	delete(r.mcp, id)
	r.mmu.Unlock()
}
