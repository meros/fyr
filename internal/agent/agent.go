// Package agent runs one `claude` process per thread (SPEC 0.5 V0.3,
// V0.7, V0.12). It turns the stream into observations on the host and
// turns permission requests into Decisions.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/runner"
)

// authOf is the profile's auth; a legacy profile is a cli-login in its
// configDir (the CLI's default when empty).
func authOf(p provider.Profile) cli.Auth {
	if p.Auth.Kind != "" {
		return p.Auth
	}
	return cli.Auth{Kind: cli.CLILogin, ConfigDir: p.ConfigDir}
}

// adapter is the CLI adapter a profile runs on.
func (m *Manager) adapter(p provider.Profile) (cli.Adapter, error) {
	id := p.CLI
	if id == "" {
		id = claudecli.ID
	}
	if a, ok := m.cfg.Adapters[id]; ok {
		return a, nil
	}
	if id == claudecli.ID {
		return claudecli.Adapter{}, nil
	}
	return nil, fmt.Errorf("no CLI adapter %q", id)
}

// observe reports what the manager saw of a thread's process as an Input
// from the process observer.
func (m *Manager) observe(threadID, kind string, args any) {
	m.host.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: kind, Thread: threadID, Args: args})
}

// Host is what the agent needs from the engine.
type Host interface {
	// Apply sends one Input down the engine's one path (decision 0020):
	// the agent manager observes the process and reports what it saw.
	Apply(in core.Input) (any, error)
	Thread(id string) (core.Thread, bool)
}

type Config struct {
	Profile  func(id string) provider.Profile
	IdleStop time.Duration // V0.7, default 10 min
	MCPURL   string        // Fyr's MCP endpoint; empty disables the binding
	LogDir   string        // raw protocol logs, optional
	Prompt   string        // appended system prompt
	// Attachments resolves stored files by id (SPEC 0.5 V0.18).
	Attachments func(id string) (attach.Meta, error)
	// OnRateLimit gets a session's rate_limit_info with its profile id (V0.44); optional.
	OnRateLimit func(profile string, info map[string]any)
	// OnCommands gets the commands of a session's initialize answer with
	// its thread, profile id and folder (V0.79); optional.
	OnCommands func(thread, profile, cwd string, cmds []core.Command)
	// HandoverDir holds the files a handover starts from (V0.45); empty
	// means a folder in the system temp dir.
	HandoverDir string
	// Adapters by id (V0.50); claude is built in.
	Adapters map[string]cli.Adapter
	// Cgroups gives each thread's process a cgroup (V0.80); optional.
	Cgroups Cgroups
	// Runner starts the processes in the launcher's runner, so they outlive
	// fyrd (V0.90); nil starts them as fyrd's children.
	Runner *runner.Client
}

// Cgroups is what the agent needs from the cgroup tree (*cgroup.Tree):
// Thread returns the directory a thread's process starts in ("" for the
// daemon's), and each Thread is matched by one Release.
type Cgroups interface {
	Thread(id string) string
	Release(id string)
}

type Manager struct {
	host Host
	cfg  Config

	mu       sync.Mutex
	sessions map[string]*session
	starting map[string]*starting // thread id → its start in flight (#40)
	tokens   map[string]string    // MCP token → thread id
}

type session struct {
	thread string
	// proc names the process (its id in the runner, V0.90); Decision ids
	// derive from it.
	proc   string
	cwd    string
	token  string
	c      cli.Session
	effort string // the --effort it started with (V0.50)
	// live is true from the start of a turn until its result. A turn starts
	// on a send or on its own (the CLI resumes after a background task), so
	// the stream decides, not a count of sends (SPEC 6.2).
	live      bool
	interrupt bool
	asks      map[string]chan core.Effect // decision id → answer
	// adopted: Decisions an adopted process had open (V0.90), whose
	// request the runner has not asked again yet.
	adopted map[string]bool
	reqs    map[string]string // control request id → decision id
	// out holds unread messages: id → "sent" or "queued" (V0.33);
	// cancelling marks the ones a cancel request is out for.
	out        map[string]string
	cancelling map[string]bool
	// lifecycle is true once the CLI reported command_lifecycle; without
	// it a message counts as read when its turn ends.
	lifecycle bool
	// viewTools: the process has the view tools (V0.37). Since V0.107
	// every process with Fyr's MCP tools has them, so a view the owner turns
	// on during a run gets the feed's upkeep in that run; the view's prompt
	// still comes only with a process started with the view on.
	viewTools bool
	// changeTools: this process has Fyr's MCP tools, the change tools
	// among them (V0.62), and the feed's hooks.
	changeTools bool
	// stopBlocked: the Stop hook blocked once this run with the feed's stop
	// notices; summary is the thread's summary before that forced turn, and
	// forcedWork is true when the forced turn called a tool other than the
	// upkeep tools. The hook runs on the client's reader, ahead of the
	// stream messages still queued for handle, so the summary comes from the
	// hook's input and the forced turn counts from the synthetic message
	// onward.
	stopBlocked bool
	summary     string
	inForced    bool
	forcedWork  bool
	// refresh: end the process after this run, so the next one starts
	// with the current flag.
	refresh bool
	// results wait for turn results (a handover waits on the source's
	// turn, V0.45).
	results []waiter
	digest  *digest
	idle    *time.Timer
	log     io.Closer
	// cgroup: the process took its thread's cgroup (V0.80); released when
	// it ends.
	cgroup bool
}

// releaseCgroup gives back the thread's cgroup once the process is gone.
func (m *Manager) releaseCgroup(s *session) {
	if s.cgroup {
		s.cgroup = false
		m.cfg.Cgroups.Release(s.thread)
	}
}

// waiter gets the result of the turn that read message msg (#40): a
// result that ends a turn while msg still waits in the CLI's queue is
// another turn's. An empty msg takes the next result.
type waiter struct {
	msg string
	ch  chan claude.Msg
}

func New(host Host, cfg Config) *Manager {
	if cfg.IdleStop == 0 {
		cfg.IdleStop = 10 * time.Minute
	}
	if cfg.Prompt == "" {
		cfg.Prompt = FyrPrompt
	}
	return &Manager{host: host, cfg: cfg, sessions: map[string]*session{}, starting: map[string]*starting{}, tokens: map[string]string{}}
}

// ThreadForToken maps an MCP bearer token to its thread (16.2 MCP binding).
func (m *Manager) ThreadForToken(tok string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.tokens[tok]
	return id, ok
}

// Live reports whether a thread has a process.
func (m *Manager) Live(thread string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[thread] != nil
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Do runs one effect of a command for a thread.
func (m *Manager) Do(threadID string, e core.Effect) error {
	switch e.Kind {
	case core.EffectSend:
		return m.send(threadID, e, nil)
	case core.EffectFeed:
		return m.feed(threadID)
	case core.EffectAnswer:
		return m.answer(threadID, e)
	case core.EffectInterrupt:
		return m.doInterrupt(threadID)
	case core.EffectStop:
		m.Stop(threadID)
		return nil
	case core.EffectRefresh:
		m.refreshView(threadID)
		return nil
	case core.EffectCancelMessage:
		return m.cancelMessage(threadID, e.MessageID)
	case core.EffectHandover:
		m.handover(threadID, e)
		return nil
	case core.EffectSetModel:
		m.mu.Lock()
		s := m.sessions[threadID]
		m.mu.Unlock()
		if s != nil {
			// V0.50: the CLI takes effort only at start; the process ends
			// when idle (or after its run) and the next message resumes
			// with the new effort.
			if e.Effort != nil {
				t, _ := m.host.Thread(threadID)
				p := m.cfg.Profile(t.Profile)
				if cli.EffortFor(p.ModelCaps, e.Model, *e.Effort, p.DefaultEffort) != s.effort {
					m.refreshView(threadID)
					m.mu.Lock()
					gone := m.sessions[threadID] != s
					m.mu.Unlock()
					if gone {
						return nil // the next start passes --model and --effort
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return s.c.SetModel(ctx, e.Model)
		}
		return nil // the next start passes --model and --effort
	}
	return fmt.Errorf("agent: unknown effect %q", e.Kind)
}

var mcpNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// ensure returns the live session of a thread, starting the process if needed.
func (m *Manager) ensure(threadID string) (*session, error) {
	return m.ensureAs(threadID, nil)
}

// as starts a thread's process on another profile than its own: a
// handover starts its target before the thread moves (V0.45).
type as struct {
	profile provider.Profile
	model   string
	effort  string
	session string
	resume  bool
	addDirs []string
}

// starting is a process start in flight: every other caller for the same
// thread waits for it instead of starting a second process on the same
// session (#40).
type starting struct {
	done chan struct{}
	s    *session
	err  error
}

// ensureAs returns the thread's live process, starting one if needed. With
// o (a handover) it always starts a new process on o and ends the one that
// runs. One start per thread runs at a time; the start takes up to 60 s, so
// it runs outside m.mu and the others wait on its done channel (#40).
func (m *Manager) ensureAs(threadID string, o *as) (*session, error) {
	m.mu.Lock()
	for {
		st := m.starting[threadID]
		if st == nil {
			break
		}
		m.mu.Unlock()
		<-st.done
		if o == nil {
			return st.s, st.err
		}
		m.mu.Lock()
	}
	old := m.sessions[threadID]
	if old != nil && o == nil {
		m.mu.Unlock()
		return old, nil
	}
	delete(m.sessions, threadID)
	st := &starting{done: make(chan struct{})}
	m.starting[threadID] = st
	m.mu.Unlock()
	if old != nil {
		m.end(threadID, old)
	}
	s, err := m.start(threadID, o)
	m.mu.Lock()
	delete(m.starting, threadID)
	if err == nil {
		m.sessions[threadID] = s
	}
	st.s, st.err = s, err
	m.mu.Unlock()
	if err == nil {
		go m.read(s)
	}
	close(st.done)
	return s, err
}

// start launches and initializes a process; ensureAs makes it the thread's.
func (m *Manager) start(threadID string, o *as) (*session, error) {
	t, ok := m.host.Thread(threadID)
	if !ok {
		return nil, fmt.Errorf("no thread %s", threadID)
	}
	handover := o != nil
	if o == nil {
		pr := m.cfg.Profile(t.Profile)
		o = &as{profile: pr, model: t.Model, session: t.SessionID, resume: t.SessionID != "",
			effort: cli.EffortFor(pr.ModelCaps, t.Model, t.Effort, pr.DefaultEffort)}
		if o.session == "" {
			o.session = newUUID()
		}
	}
	p := o.profile
	ad, err := m.adapter(p)
	if err != nil {
		return nil, err
	}
	l := cli.Launch{Binary: p.Binary, Dir: t.Cwd, Auth: authOf(p), Endpoint: p.Endpoint, Env: p.Env,
		Model: o.model, Effort: o.effort, Session: o.session, Resume: o.resume, AddDirs: o.addDirs, PermissionMode: p.PermissionMode}
	if p.Key != nil {
		k, kerr := p.Key()
		if kerr != nil || k == "" {
			if p.KeyRequired || l.Auth.Kind == cli.APIKey {
				what := "a key"
				if l.Auth.Kind == cli.CLILogin {
					what = "a login token"
				}
				return nil, fmt.Errorf("%s needs %s: add it in Settings → Providers", name(p), what)
			}
		}
		l.Key = k
	}
	s := &session{thread: threadID, proc: threadID + "-" + randHex(6), cwd: t.Cwd, asks: map[string]chan core.Effect{}, reqs: map[string]string{}, digest: newDigest(),
		out: map[string]string{}, cancelling: map[string]bool{}, effort: l.Effort}
	if m.cfg.MCPURL != "" {
		s.token = randHex(24)
		cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fyr": map[string]any{
			"type": "http", "url": m.cfg.MCPURL, "headers": map[string]string{"Authorization": "Bearer " + s.token}}}})
		l.MCPConfig, l.AllowedTools = string(cfg), []string{"mcp__fyr__*"}
	}
	prompt := m.cfg.Prompt
	if prompt == "-" {
		prompt = ""
	}
	s.changeTools = m.cfg.MCPURL != "" // V0.62
	s.viewTools = m.cfg.MCPURL != ""   // V0.107
	if t.ViewOn() && m.cfg.MCPURL != "" {
		prompt = strings.TrimSpace(prompt + "\n\n" + ViewPrompt)
	}
	l.SystemPrompt = prompt
	if m.cfg.LogDir != "" {
		os.MkdirAll(m.cfg.LogDir, 0o700)
		f, err := os.OpenFile(filepath.Join(m.cfg.LogDir, threadID+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			l.Log, s.log = f, f
		}
	}
	if _, err := os.Stat(t.Cwd); err != nil {
		return nil, fmt.Errorf("working directory: %w", err)
	}
	// The CLI connects its MCP servers while it starts, before initialize,
	// so the token must be valid before the process exists (SPEC 16.2).
	if s.token != "" {
		m.mu.Lock()
		m.tokens[s.token] = threadID
		m.mu.Unlock()
	}
	dropToken := func() {
		m.mu.Lock()
		delete(m.tokens, s.token)
		m.mu.Unlock()
	}
	// V0.80: the CLI and everything it starts run in the thread's cgroup.
	if m.cfg.Cgroups != nil {
		l.Cgroup = m.cfg.Cgroups.Thread(threadID)
		s.cgroup = l.Cgroup != ""
	}
	if m.cfg.Runner != nil {
		// V0.90: the runner holds the process; the note lets the next fyrd
		// adopt it.
		l.Runner, l.ProcID, l.RunnerKey = m.cfg.Runner, s.proc, threadID
		l.Meta, _ = json.Marshal(procMeta{Thread: threadID, CLI: ad.ID(), Token: s.token, Effort: l.Effort,
			ViewTools: s.viewTools, ChangeTools: s.changeTools, Cgroup: s.cgroup})
	}
	c, err := ad.Start(l, cli.Handlers{
		CanUseTool: func(req claude.Msg) claude.Msg { return m.canUseTool(s, req) },
		Cancel:     func(reqID string) { m.cancelRequest(s, reqID) },
		Hook:       func(id string, in claude.Msg) claude.Msg { return m.hook(s, id, in) },
	})
	if err != nil {
		dropToken()
		m.releaseCgroup(s)
		if s.log != nil {
			s.log.Close()
		}
		// V0.90 (1): the runner holds a live process for this thread; link
		// to it instead of a second one. A handover needs its own process.
		var ex *runner.ExistsError
		if errors.As(err, &ex) && !handover {
			return m.adoptExisting(threadID, ex.ID)
		}
		return nil, err
	}
	s.c = c
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	answer, err := c.Initialize(ctx, initHooks(s.viewTools || s.changeTools))
	if err != nil {
		c.Kill()
		<-c.Done()
		dropToken()
		m.releaseCgroup(s)
		if perr := c.Err(); perr != nil {
			// The CLI's own words, e.g. "No conversation found" (V0.45).
			return nil, fmt.Errorf("initialize: %w (%s)", err, strings.TrimSpace(perr.Error()))
		}
		return nil, fmt.Errorf("initialize: %w", err)
	}
	// V0.79: the commands and skills the composer offers.
	if cmds := commandsOf(answer); cmds != nil && m.cfg.OnCommands != nil {
		m.cfg.OnCommands(threadID, p.ID, t.Cwd, cmds)
	}
	return s, nil
}

// Stop ends a thread's process; open questions die with it.
func (m *Manager) Stop(threadID string) {
	m.mu.Lock()
	s := m.sessions[threadID]
	m.mu.Unlock()
	if s != nil {
		s.c.Close()
		select {
		case <-s.c.Done():
		case <-time.After(5 * time.Second):
			s.c.Kill()
		}
	}
}

// StopAll ends every process, for shutdown. With a runner it ends none
// (V0.90): it lets go of each, and the next fyrd adopts them.
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	var held []*runner.Conn
	for id, s := range m.sessions {
		if p := pipeOf(s); p != nil {
			held = append(held, p)
			continue
		}
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, p := range held {
		p.Detach()
	}
	for _, id := range ids {
		m.Stop(id)
	}
}
