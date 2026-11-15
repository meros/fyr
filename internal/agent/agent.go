// Package agent runs one `claude` process per thread (SPEC 0.5 V0.3,
// V0.7, V0.12). It turns the stream into observations on the host and
// turns permission requests into Decisions.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/quota"
)

// Profile is a claude-compatible binary with its settings (V0.11).
type Profile struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Binary         string   `json:"binary"`
	Color          string   `json:"color"`
	Models         []string `json:"models"`
	DefaultModel   string   `json:"defaultModel"`
	PermissionMode string   `json:"permissionMode,omitempty"`
	Env            []string `json:"env,omitempty"`
	// Quota: where the provider allowance is read (V0.44); clients see the kind only.
	Quota *quota.Spec `json:"quota,omitempty"`
	// ConfigDir is the CLI's config dir (CLAUDE_CONFIG_DIR) when the
	// profile has its own; Fyr then sets it. See ConfigDirPath (V0.45).
	ConfigDir string `json:"configDir,omitempty"`

	// V0.50 (decision 0015): a profile made from a provider. CLI is the
	// adapter id ("" is claude); Auth and Endpoint are applied by the
	// adapter; Key reads the provider's secret at launch and never leaves
	// the host.
	CLI      string                 `json:"cli,omitempty"`
	Auth     cli.Auth               `json:"-"`
	Endpoint cli.Endpoint           `json:"-"`
	Key      func() (string, error) `json:"-"`
	// KeyRequired: the profile does not start without its key (an endpoint,
	// or an account that logs in with a token).
	KeyRequired bool `json:"-"`
	// Unavailable says why the provider cannot run now (V0.50: no token or
	// key yet); clients do not offer it for new threads or Hand over.
	Unavailable string `json:"unavailable,omitempty"`
	// ModelCaps: the offered models with their effort levels; DefaultEffort
	// is used when the thread names none and the model takes it.
	ModelCaps     []cli.Model `json:"modelCaps,omitempty"`
	DefaultEffort string      `json:"defaultEffort,omitempty"`
}

// auth is the profile's auth; a legacy profile is a cli-login in its
// configDir (the CLI's default when empty).
func (p Profile) auth() cli.Auth {
	if p.Auth.Kind != "" {
		return p.Auth
	}
	return cli.Auth{Kind: cli.CLILogin, ConfigDir: p.ConfigDir}
}

// adapter is the CLI adapter a profile runs on.
func (m *Manager) adapter(p Profile) (cli.Adapter, error) {
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

// Ev is a timeline event the agent adds to a thread.
type Ev struct {
	Type string
	Data any
}

// Host is what the agent needs from the engine.
type Host interface {
	// Observe changes one thread and appends events in one transaction.
	Observe(threadID string, fn func(t *core.Thread, now time.Time), evs ...Ev)
	Thread(id string) (core.Thread, bool)
}

type Config struct {
	Profile  func(id string) Profile
	IdleStop time.Duration // V0.7, default 10 min
	MCPURL   string        // Fyr's MCP endpoint; empty disables the binding
	LogDir   string        // raw protocol logs, optional
	Prompt   string        // appended system prompt
	// Attachments resolves stored files by id (SPEC 0.5 V0.18).
	Attachments func(id string) (attach.Meta, error)
	// OnRateLimit gets a session's rate_limit_info with its profile id (V0.44); optional.
	OnRateLimit func(profile string, info map[string]any)
	// HandoverDir holds the files a handover starts from (V0.45); empty
	// means a folder in the system temp dir.
	HandoverDir string
	// Adapters by id (V0.50); claude is built in.
	Adapters map[string]cli.Adapter
}

// FyrPrompt is the short block that steers agents to Fyr tools (V0.12).
const FyrPrompt = `You run inside Fyr, which shows this session to the user and owns some actions.
- Every change you make or prepare is a change record in Fyr (decision 0018). Fyr records this folder's git branch and its PRs itself. For anything else (a server or cluster change, a migration, a NixOS rebuild, another repo, a design, a document) call mcp__fyr__change_open before the first edit, then mcp__fyr__change_status as it moves (working, review, landed, live; a failed condition when something breaks), mcp__fyr__change_revise for a new version and mcp__fyr__change_attach for proof. Read Fyr's facts with mcp__fyr__change_list instead of polling gh.
- Ask the user about a change with mcp__fyr__change_request (review, approve, merge, apply, deploy, revert). To merge a pull request use ask "merge"; never run gh pr merge. You cannot mark your own change approved.
- Give the user a button for the next step with mcp__fyr__offer_action, on a change, the thread or your live view: run "script" for an exact command Fyr runs on their press, run "agent" for a step that needs judgement. Fyr never runs it without the press.
- When you open a PR some other way than gh pr create, call mcp__fyr__pr_link with its URL (or change_open with pr).
- When the user says the thread is finished once something happens, call mcp__fyr__settle_when.
- To see or coordinate the user's other sessions, use mcp__fyr__threads, mcp__fyr__thread_read, mcp__fyr__thread_send and mcp__fyr__thread_new (start a separate session for work that should run on its own).
- To show the user something (a screenshot after UI work, a chart, a mockup, a page, a 3D model or scene), call mcp__fyr__show with a path, html or url; it appears in the thread on desktop and phone. Pages run real HTML in a sandbox: WebGL, WASM, ES modules, three.js or model-viewer from cdn.jsdelivr.net, unpkg.com, esm.sh, cdnjs.cloudflare.com or ajax.googleapis.com, and their own files: give path a folder (index.html plus models, textures, scripts) or an HTML file next to its files.`

// ViewPrompt is appended to the system prompt of a thread with a live view.
const ViewPrompt = `This thread has a live view: a small page pinned above the conversation that shows the state of your work, so the user does not have to read the thread to know where things are.
- The view is a page (HTML, rarely rebuilt) plus a small JSON state (at most 16 kB) that you update often. Publish the page once with mcp__fyr__view_publish {html or path, state}; after that change only the state with mcp__fyr__view_state {patch}, a JSON Merge Patch (RFC 7396: keys merge, null deletes a key, arrays are replaced whole).
- State shape: "headline" (one line, the current status; Fyr also shows it in the inbox) plus any fields your page needs, for example "steps", "blockers", "next", "pr".
- In the page, window.fyr.state is the current state; fyr.onState(fn) runs fn now and on every change; an element with data-text="field.path" shows that field. Fragments get the Fyr theme (CSS variables --bg --fg --muted --line --accent, light and dark). The page cannot call Fyr or other hosts; it may load libraries from cdn.jsdelivr.net, unpkg.com, esm.sh, cdnjs.cloudflare.com and ajax.googleapis.com.
- Buttons send messages: fyr.send(text), or an element with data-send="text", sends that text to you as a message from the view, as if the user typed it. Use buttons for choices and next steps; they never run anything by themselves.
- A button can also run an action you declared on the view with mcp__fyr__offer_action {on: "view", id, …}: an element with data-action="<id>", or fyr.run("<id>"). Fyr runs it on the user's press: a script exactly as declared, or a prompt to you or a new thread. Use it for an exact step (run the e2e suite, apply the plan); use data-send for a choice.
- A message marked [Typed on the live view tab] was written while the user looks at the view, not the conversation. Answer it first in one or two plain, spoken sentences (the answer, or what you will do now); that reply is shown next to their message on the View tab. Then do the work and update the view.
- Update the view as the work moves, not only at the end: when you start or finish an item, change the headline and the items that moved. Fyr reminds you after a few minutes without an update.
- Before you end a turn, update the view if anything it shows changed, or call mcp__fyr__view_unchanged {reason}. Keep it short and current; it is the first thing the user reads.`

// FromView marks a message sent by a press in the live view (V0.37).
const FromView = "view"

// FromViewTyped marks a message the user typed on the View tab (V0.49).
const FromViewTyped = "view-typed"

// FromHandover marks the messages Fyr sends during a handover (V0.45).
const FromHandover = "handover"

type Manager struct {
	host Host
	cfg  Config

	mu       sync.Mutex
	sessions map[string]*session
	tokens   map[string]string // MCP token → thread id
}

type session struct {
	thread string
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
	reqs      map[string]string           // control request id → decision id
	// out holds unread messages: id → "sent" or "queued" (V0.33);
	// cancelling marks the ones a cancel request is out for.
	out        map[string]string
	cancelling map[string]bool
	// lifecycle is true once the CLI reported command_lifecycle; without
	// it a message counts as read when its turn ends.
	lifecycle bool
	// viewTools: the process started with the live view's prompt and
	// tools (V0.37). The flag can change while it runs; the Stop hook only
	// asks for a view the agent knows about.
	viewTools bool
	// viewBlocked: the Stop hook blocked once this run; summary is the
	// thread's summary before that forced turn, and forcedWork is true when
	// the forced turn called a tool other than the view tools.
	// The hook runs on the client's reader, ahead of the stream messages
	// still queued for handle, so the summary comes from the hook's input
	// and the forced turn counts from the synthetic message onward.
	viewBlocked bool
	// viewNudged is the last mid-turn reminder (V0.52).
	viewNudged time.Time
	summary    string
	inForced   bool
	forcedWork bool
	// refresh: end the process after this run, so the next one starts
	// with the current flag.
	refresh bool
	// results get the result message of each turn (a handover waits on
	// the source's turn, V0.45).
	results []chan claude.Msg
	digest  *digest
	idle    *time.Timer
	log     io.Closer
}

func New(host Host, cfg Config) *Manager {
	if cfg.IdleStop == 0 {
		cfg.IdleStop = 10 * time.Minute
	}
	if cfg.Prompt == "" {
		cfg.Prompt = FyrPrompt
	}
	return &Manager{host: host, cfg: cfg, sessions: map[string]*session{}, tokens: map[string]string{}}
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
	case "send":
		return m.send(threadID, e)
	case "answer":
		return m.answer(threadID, e)
	case "interrupt":
		return m.doInterrupt(threadID)
	case "stop":
		m.Stop(threadID)
		return nil
	case "refresh":
		m.refreshView(threadID)
		return nil
	case "cancelMessage":
		return m.cancelMessage(threadID, e.MessageID)
	case "handover":
		m.handover(threadID, e)
		return nil
	case "setModel":
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

// inlineImage is the set of image types Claude takes as image blocks.
var inlineImage = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// withAttachments adds image blocks for small images and lists every
// attachment with its path in the text (SPEC 0.5 V0.18).
func (m *Manager) withAttachments(e core.Effect) (core.Effect, []map[string]any) {
	var metas []map[string]any
	if len(e.Attachments) == 0 || m.cfg.Attachments == nil {
		return e, nil
	}
	var lines []string
	for _, id := range e.Attachments {
		a, err := m.cfg.Attachments(id)
		if err != nil {
			continue
		}
		metas = append(metas, map[string]any{"id": a.ID, "name": a.Name, "mediaType": a.MediaType, "size": a.Size})
		if inlineImage[a.MediaType] && a.Size <= 5<<20 {
			if b, err := os.ReadFile(a.Path); err == nil {
				e.Images = append(e.Images, core.Image{MediaType: a.MediaType, Data: base64.StdEncoding.EncodeToString(b)})
			}
		}
		lines = append(lines, fmt.Sprintf("Attached: %s (%s, %s) at %s", a.Name, a.MediaType, humanSize(a.Size), a.Path))
	}
	if len(lines) > 0 {
		if e.Text != "" {
			e.Text += "\n\n"
		}
		e.Text += strings.Join(lines, "\n")
	}
	return e, metas
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d kB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

func content(e core.Effect) []claude.Msg {
	var out []claude.Msg
	for _, im := range e.Images {
		out = append(out, claude.Msg{"type": "image", "source": claude.Msg{"type": "base64", "media_type": im.MediaType, "data": im.Data}})
	}
	if e.Text != "" {
		out = append(out, claude.Msg{"type": "text", "text": e.Text})
	}
	return out
}

func (m *Manager) send(threadID string, e core.Effect) error {
	if e.MessageID == "" {
		e.MessageID = newUUID()
	}
	s, err := m.ensure(threadID)
	if err != nil {
		// V0.33: the message stays in the log, marked lost.
		ud := map[string]any{"id": e.MessageID, "text": e.Text, "images": len(e.Images)}
		if e.From != "" {
			ud["from"] = e.From
		}
		m.host.Observe(threadID, func(t *core.Thread, now time.Time) {
			core.RunEnded(t, &core.Failure{Class: "process", Message: "Could not start the agent: " + err.Error(), At: now}, now)
		}, Ev{"user", ud}, Ev{"message.state", map[string]any{"id": e.MessageID, "state": "lost"}},
			Ev{"run.ended", map[string]any{"failure": map[string]any{"class": "process", "message": err.Error()}}})
		return err
	}
	m.mu.Lock()
	first := !s.live
	s.live = true
	if s.idle != nil {
		s.idle.Stop()
	}
	s.out[e.MessageID] = "sent"
	m.mu.Unlock()
	userText := e.Text
	if e.From == FromHandover {
		// V0.45: Fyr's own words, no prefix.
	} else if e.From == FromView {
		// V0.37: a press in the live view; the user pressed it.
		e.Text = "[From the live view] " + e.Text
	} else if e.From == FromViewTyped {
		// V0.49: typed while the user looks at the view, not the conversation.
		e.Text = "[Typed on the live view tab] " + e.Text
	} else if e.From != "" {
		// V0.22: tell the receiving agent which thread wrote this.
		label := e.From
		if ft, ok := m.host.Thread(e.From); ok && ft.Title != "" {
			label = fmt.Sprintf("%q (%s)", ft.Title, e.From)
		}
		e.Text = "[From thread " + label + "] " + e.Text
	}
	e, metas := m.withAttachments(e)
	ud := map[string]any{"id": e.MessageID, "text": userText, "images": len(e.Images)}
	if metas != nil {
		ud["attachments"] = metas
	}
	if e.From != "" {
		ud["from"] = e.From
	}
	evs := []Ev{{"user", ud}}
	if first {
		evs = append(evs, Ev{"run.started", map[string]any{}})
	}
	id := e.MessageID
	m.host.Observe(threadID, func(t *core.Thread, now time.Time) {
		if first {
			core.RunStarted(t, now)
		}
		core.SetOutbox(t, id, "sent")
	}, evs...)
	return s.c.SendUser(content(e), id)
}

// delivered moves a message to a new state (V0.33) and records it once.
// It ignores a message that is no longer unread, and a "cancelled" report
// for a message whose cancel request is still out (cancelMessage records it).
func (m *Manager) delivered(s *session, id, state string) {
	m.mu.Lock()
	cur := s.out[id]
	if cur == "" || cur == state || (state == "cancelled" && s.cancelling[id]) {
		m.mu.Unlock()
		return
	}
	if state == "queued" {
		s.out[id] = state
	} else {
		delete(s.out, id)
	}
	m.mu.Unlock()
	m.host.Observe(s.thread, func(t *core.Thread, _ time.Time) { core.SetOutbox(t, id, state) },
		Ev{"message.state", map[string]any{"id": id, "state": state}})
}

// unread lists the messages the agent has not read.
func (m *Manager) unread(s *session) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(s.out))
	for id := range s.out {
		ids = append(ids, id)
	}
	return ids
}

// cancelMessage withdraws a message from the CLI's queue (V0.33).
func (m *Manager) cancelMessage(threadID, id string) error {
	m.mu.Lock()
	s := m.sessions[threadID]
	waiting := s != nil && s.out[id] != ""
	if waiting {
		s.cancelling[id] = true
	}
	m.mu.Unlock()
	if !waiting {
		return fmt.Errorf("The agent has already read this message")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ok, err := s.c.CancelMessage(ctx, id)
	m.mu.Lock()
	delete(s.cancelling, id)
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("The agent cannot withdraw this message: %v", err)
	}
	if !ok {
		return fmt.Errorf("The agent has already read this message")
	}
	m.delivered(s, id, "cancelled")
	return nil
}

var mcpNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// ensure returns the live session of a thread, starting the process if needed.
func (m *Manager) ensure(threadID string) (*session, error) {
	return m.ensureAs(threadID, nil)
}

// as starts a thread's process on another profile than its own: a
// handover starts its target before the thread moves (V0.45).
type as struct {
	profile Profile
	model   string
	effort  string
	session string
	resume  bool
	addDirs []string
}

func (m *Manager) ensureAs(threadID string, o *as) (*session, error) {
	m.mu.Lock()
	if s := m.sessions[threadID]; s != nil {
		m.mu.Unlock()
		return s, nil
	}
	m.mu.Unlock()
	t, ok := m.host.Thread(threadID)
	if !ok {
		return nil, fmt.Errorf("no thread %s", threadID)
	}
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
	l := cli.Launch{Binary: p.Binary, Dir: t.Cwd, Auth: p.auth(), Endpoint: p.Endpoint, Env: p.Env,
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
	s := &session{thread: threadID, cwd: t.Cwd, asks: map[string]chan core.Effect{}, reqs: map[string]string{}, digest: newDigest(),
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
	if t.LiveView && m.cfg.MCPURL != "" {
		s.viewTools = true
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
	c, err := ad.Start(l, cli.Handlers{
		CanUseTool: func(req claude.Msg) claude.Msg { return m.canUseTool(s, req) },
		Cancel:     func(reqID string) { m.cancelRequest(s, reqID) },
		Hook: func(id string, in claude.Msg) claude.Msg {
			if id == "view" {
				return m.viewStop(s, in)
			}
			if id == "viewnudge" {
				return m.viewNudge(s)
			}
			name, _ := in["tool_name"].(string)
			ti, _ := in["tool_input"].(map[string]any)
			if r := core.Gate(name, ti); r != "" {
				return claude.Msg{"hookSpecificOutput": claude.Msg{"hookEventName": "PreToolUse",
					"permissionDecision": "deny", "permissionDecisionReason": r}}
			}
			return nil
		},
	})
	if err != nil {
		dropToken()
		return nil, err
	}
	s.c = c
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx, initHooks(s.viewTools)); err != nil {
		c.Kill()
		<-c.Done()
		dropToken()
		if perr := c.Err(); perr != nil {
			// The CLI's own words, e.g. "No conversation found" (V0.45).
			return nil, fmt.Errorf("initialize: %w (%s)", err, strings.TrimSpace(perr.Error()))
		}
		return nil, fmt.Errorf("initialize: %w", err)
	}
	m.mu.Lock()
	m.sessions[threadID] = s
	m.mu.Unlock()
	go m.read(s)
	return s, nil
}

func (m *Manager) read(s *session) {
	for msg := range s.c.Out() {
		m.handle(s, msg)
	}
	<-s.c.Done()
	m.mu.Lock()
	if m.sessions[s.thread] == s {
		delete(m.sessions, s.thread)
	}
	delete(m.tokens, s.token)
	live := s.live || len(s.asks) > 0
	for _, ch := range s.results {
		close(ch)
	}
	s.results = nil
	for id, ch := range s.asks {
		close(ch)
		delete(s.asks, id)
	}
	if s.idle != nil {
		s.idle.Stop()
	}
	m.mu.Unlock()
	if s.log != nil {
		s.log.Close()
	}
	// V0.33: the CLI's queue died with the process.
	for _, id := range m.unread(s) {
		m.delivered(s, id, "lost")
	}
	if live {
		msg := "The agent process exited during the run"
		if err := s.c.Err(); err != nil {
			msg += ": " + clip(strings.TrimSpace(err.Error()), 600)
		}
		m.host.Observe(s.thread, func(t *core.Thread, now time.Time) {
			core.ProcessLost(t, "process", msg, now)
		}, Ev{"run.ended", map[string]any{"failure": map[string]any{"class": "process", "message": msg}}})
	}
}

func (m *Manager) handle(s *session, msg claude.Msg) {
	typ, _ := msg["type"].(string)
	if typ == "assistant" || (typ == "system" && msg["subtype"] == "init") {
		m.turnStarted(s)
	}
	switch typ {
	case "rate_limit_event":
		// V0.44: a free quota update between the host's reads.
		if info, ok := msg["rate_limit_info"].(map[string]any); ok && m.cfg.OnRateLimit != nil {
			if t, ok := m.host.Thread(s.thread); ok {
				m.cfg.OnRateLimit(m.cfg.Profile(t.Profile).ID, info)
			}
		}
		return
	case "command_lifecycle":
		id, _ := msg["command_uuid"].(string)
		if id == "" {
			id, _ = msg["uuid"].(string)
		}
		m.mu.Lock()
		s.lifecycle = true
		m.mu.Unlock()
		switch msg["state"] {
		case "queued":
			m.delivered(s, id, "queued")
		case "started", "completed":
			m.delivered(s, id, "read")
		case "cancelled":
			m.delivered(s, id, "cancelled")
		}
		return
	case "system":
		if msg["subtype"] == "background_tasks_changed" {
			for _, k := range taskList(msg) {
				k := k
				m.host.Observe(s.thread, func(t *core.Thread, now time.Time) { core.TaskObserved(t, k, now) })
			}
			return
		}
		if k, ev, ok := taskFromSystem(msg); ok {
			var evs []Ev
			if ev != "" {
				evs = append(evs, Ev{"task", map[string]any{"kind": ev, "id": k.ID, "description": k.Description, "status": k.Status, "summary": k.Summary}})
			}
			m.host.Observe(s.thread, func(t *core.Thread, now time.Time) { core.TaskObserved(t, k, now) }, evs...)
			return
		}
		if msg["subtype"] == "init" {
			sid, _ := msg["session_id"].(string)
			model, _ := msg["model"].(string)
			m.host.Observe(s.thread, func(t *core.Thread, _ time.Time) {
				if sid != "" {
					t.SessionID = sid
				}
				if model != "" && t.Model == "" {
					t.Model = model
				}
			})
		}
		return
	case "assistant", "user":
		if typ == "user" && msg["isSynthetic"] == true {
			// The Stop hook's reason, echoed by the CLI (V0.37): never
			// logged; the forced turn starts here.
			m.mu.Lock()
			s.inForced = s.viewBlocked
			forced := s.inForced
			m.mu.Unlock()
			if forced {
				// In stream order, so the client can show the forced turn's
				// text as one quiet line (V0.37).
				m.host.Observe(s.thread, func(*core.Thread, time.Time) {}, Ev{"view.forced", map[string]any{}})
			}
			return
		}
		if typ == "assistant" {
			m.noteForced(s, msg)
		}
		o := s.digest.feed(msg)
		m.host.Observe(s.thread, func(t *core.Thread, now time.Time) {
			if o.Activity != "" {
				t.Activity = o.Activity
			}
			if o.Summary != "" {
				t.Summary = o.Summary
			}
			t.Subagents += o.Subagents
			if o.PRURL != "" && (t.PR == nil || t.PR.URL != o.PRURL) {
				repo, n, _ := ParsePR(o.PRURL)
				core.PRObserved(t, core.PR{URL: o.PRURL, Repo: repo, Number: n, State: "OPEN", Checks: "pending"}, now)
			}
		}, Ev{"msg", trimMsg(msg)})
	case "result":
		m.mu.Lock()
		ended := s.live
		interrupted := s.interrupt
		s.live, s.interrupt = false, false
		m.mu.Unlock()
		m.mu.Lock()
		noReports := !s.lifecycle
		restore := s.viewBlocked && !s.forcedWork
		summary := s.summary
		s.viewBlocked, s.inForced, s.forcedWork, s.summary = false, false, false, ""
		refresh := s.refresh && len(s.out) == 0
		m.mu.Unlock()
		if noReports {
			// An older CLI reports no delivery: its turn read every message.
			for _, id := range m.unread(s) {
				m.delivered(s, id, "read")
			}
		}
		if !ended {
			return // a stray result outside a turn
		}
		var f *core.Failure
		isErr, _ := msg["is_error"].(bool)
		if isErr && !interrupted {
			text, _ := msg["result"].(string)
			if text == "" {
				text, _ = msg["subtype"].(string)
			}
			f = &core.Failure{Class: classify(text), Message: clip(firstLine(text), 300)}
		}
		m.mu.Lock()
		waiters := s.results
		s.results = nil
		m.mu.Unlock()
		for _, ch := range waiters {
			ch <- msg
		}
		data := map[string]any{"interrupted": interrupted, "costUsd": msg["total_cost_usd"], "durationMs": msg["duration_ms"], "failure": f}
		facts := gitFacts(s.cwd)
		branch := facts.Branch
		m.host.Observe(s.thread, func(t *core.Thread, now time.Time) {
			if f != nil {
				f.At = now
			}
			core.RunEnded(t, f, now)
			t.Branch = branch
			core.GitObserved(t, facts, now) // V0.59 (3)
			if restore {
				// V0.37: a forced turn that only updated the view keeps the
				// run's own last words as the summary.
				t.Summary = summary
			}
		}, Ev{"run.ended", data})
		if refresh {
			m.mu.Lock()
			if m.sessions[s.thread] == s {
				delete(m.sessions, s.thread)
			}
			m.mu.Unlock()
			s.c.Close()
			return
		}
		m.armIdle(s)
	}
}

// viewStop answers the Stop hook (SPEC 0.5 V0.37): block once per run
// when the thread has a live view and the run did not touch it.
func (m *Manager) viewStop(s *session, in claude.Msg) claude.Msg {
	t, ok := m.host.Thread(s.thread)
	if !ok {
		return nil
	}
	bg, _ := in["background_tasks"].([]any)
	active, _ := in["stop_hook_active"].(bool)
	m.mu.Lock()
	defer m.mu.Unlock()
	d := core.ViewStop{Flag: s.viewTools && t.LiveView, Touched: core.ViewTouchedThisRun(&t),
		Blocked: s.viewBlocked, Active: active, Tasks: len(bg) + t.RunningTasks()}
	if !d.Block() {
		return nil
	}
	summary := firstLine(fmt.Sprint(orNil(in["last_assistant_message"])))
	if summary == "" {
		summary = t.Summary
	}
	s.viewBlocked, s.summary, s.inForced, s.forcedWork = true, summary, false, false
	return claude.Msg{"decision": "block", "reason": core.ViewStopReason}
}

// viewNudge answers the PostToolUse hook (SPEC 0.5 V0.52): when a turn has
// run ViewNudgeAfter without a view_* call, add a reminder to the context.
func (m *Manager) viewNudge(s *session) claude.Msg {
	t, ok := m.host.Thread(s.thread)
	if !ok {
		return nil
	}
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	n := core.ViewNudge{Flag: s.viewTools && t.LiveView, Nudged: s.viewNudged}
	if t.RunStartedAt != nil {
		n.RunStarted = *t.RunStartedAt
	}
	if t.Live != nil {
		n.Checked = t.Live.CheckedAt
	}
	if !n.Due(now) {
		return nil
	}
	s.viewNudged = now
	return claude.Msg{"hookSpecificOutput": claude.Msg{"hookEventName": "PostToolUse", "additionalContext": core.ViewNudgeReason}}
}

func orNil(v any) any {
	if v == nil {
		return ""
	}
	return v
}

// noteForced marks a forced turn that did more than update the view.
func (m *Manager) noteForced(s *session, msg claude.Msg) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !s.inForced || s.forcedWork {
		return
	}
	inner, _ := msg["message"].(map[string]any)
	blocks, _ := inner["content"].([]any)
	for _, x := range blocks {
		b, _ := x.(map[string]any)
		if b["type"] != "tool_use" {
			continue
		}
		if name, _ := b["name"].(string); !strings.HasPrefix(name, "mcp__fyr__view_") {
			s.forcedWork = true
		}
	}
}

// refreshView makes the next process start with the thread's current
// live view flag: an idle process ends now, a running one after its run.
func (m *Manager) refreshView(threadID string) {
	m.mu.Lock()
	s := m.sessions[threadID]
	if s == nil {
		m.mu.Unlock()
		return
	}
	idle := !s.live && len(s.asks) == 0 && len(s.out) == 0
	if idle {
		delete(m.sessions, threadID) // the next send starts a new process
	} else {
		s.refresh = true
	}
	m.mu.Unlock()
	if idle {
		s.c.Close()
	}
}

// turnStarted opens a run for a turn the CLI started without a send.
func (m *Manager) turnStarted(s *session) {
	m.mu.Lock()
	if s.live {
		m.mu.Unlock()
		return
	}
	s.live = true
	if s.idle != nil {
		s.idle.Stop()
	}
	m.mu.Unlock()
	m.host.Observe(s.thread, func(t *core.Thread, now time.Time) { core.RunStarted(t, now) },
		Ev{"run.started", map[string]any{"by": "agent"}})
}

func (m *Manager) armIdle(s *session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.idle != nil {
		s.idle.Stop()
	}
	s.idle = time.AfterFunc(m.cfg.IdleStop, func() {
		m.mu.Lock()
		quiet := !s.live && len(s.asks) == 0
		m.mu.Unlock()
		if quiet {
			s.c.Close() // V0.7: the next message resumes the session
		}
	})
}

// classify maps a result text to a failure class (SPEC 3.3).
func classify(text string) string {
	l := strings.ToLower(text)
	switch {
	case strings.Contains(l, "auth") || strings.Contains(l, "401") || strings.Contains(l, "login"):
		return "auth"
	case strings.Contains(l, "mcp"):
		return "mcp"
	case strings.Contains(l, "api error") || strings.Contains(l, "overloaded") || strings.Contains(l, "rate limit") || strings.Contains(l, "529") || strings.Contains(l, "500"):
		return "api"
	}
	return "error"
}

// trimMsg keeps a stream message small enough for the log: long tool
// results are cut, images are kept (the filmstrip needs them).
func trimMsg(msg claude.Msg) claude.Msg {
	out := claude.Msg{"type": msg["type"], "parent_tool_use_id": msg["parent_tool_use_id"]}
	inner, _ := msg["message"].(map[string]any)
	blocks, _ := inner["content"].([]any)
	var kept []any
	for _, x := range blocks {
		b, _ := x.(map[string]any)
		if b["type"] == "tool_result" {
			if s, ok := b["content"].(string); ok && len(s) > 20000 {
				b = copyMap(b)
				b["content"] = s[:20000] + "\n… (cut by Fyr)"
			}
		}
		if b["type"] == "thinking" {
			continue
		}
		kept = append(kept, b)
	}
	out["message"] = claude.Msg{"role": inner["role"], "content": kept}
	return out
}

func copyMap(m map[string]any) map[string]any {
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// canUseTool turns a permission request into a Decision and waits for the
// answer (SPEC 5.6, 12.3). AskUserQuestion becomes kind "ask".
func (m *Manager) canUseTool(s *session, req claude.Msg) claude.Msg {
	tool, _ := req["tool_name"].(string)
	input, _ := req["input"].(map[string]any)
	reqID, _ := req["request_id"].(string)
	agentID, _ := req["agent_id"].(string)
	d := core.Decision{ID: "d_" + randHex(6), Assignee: core.Owner, Agent: agentID}
	if tool == "AskUserQuestion" {
		d.Kind = "ask"
		b, _ := json.Marshal(input["questions"])
		json.Unmarshal(b, &d.Questions)
	} else {
		d.Kind, d.Tool, d.Input = "approval", tool, input
	}
	ch := make(chan core.Effect, 1)
	m.mu.Lock()
	s.asks[d.ID] = ch
	s.reqs[reqID] = d.ID
	m.mu.Unlock()
	m.host.Observe(s.thread, func(t *core.Thread, now time.Time) {
		d.CreatedAt = now
		core.DecisionOpened(t, d)
	}, Ev{"decision.opened", map[string]any{"decision": d}})

	ans, ok := <-ch
	m.mu.Lock()
	delete(s.asks, d.ID)
	delete(s.reqs, reqID)
	m.mu.Unlock()
	if !ok || ans.Kind == "cancel" {
		return claude.Msg{"behavior": "deny", "message": "The question was withdrawn."}
	}
	if d.Kind == "ask" {
		answers := map[string]any{}
		for k, v := range ans.Answers {
			answers[k] = v
		}
		in := copyMap(input)
		in["answers"] = answers
		return claude.Msg{"behavior": "allow", "updatedInput": in}
	}
	if ans.Allow {
		return claude.Msg{"behavior": "allow", "updatedInput": input}
	}
	return claude.Msg{"behavior": "deny", "message": "The user denied this tool call."}
}

func (m *Manager) answer(threadID string, e core.Effect) error {
	m.mu.Lock()
	s := m.sessions[threadID]
	var ch chan core.Effect
	if s != nil {
		ch = s.asks[e.DecisionID]
	}
	m.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("the question is no longer open in the agent")
	}
	ch <- e
	text := ""
	if e.Answers != nil {
		parts := []string{}
		for q, a := range e.Answers {
			parts = append(parts, q+" → "+a)
		}
		text = strings.Join(parts, "\n")
	} else if e.Allow {
		text = "allowed"
	} else {
		text = "denied"
	}
	m.host.Observe(threadID, func(*core.Thread, time.Time) {}, Ev{"decision.closed", map[string]any{"id": e.DecisionID, "answer": text}})
	return nil
}

func (m *Manager) cancelRequest(s *session, reqID string) {
	m.mu.Lock()
	id := s.reqs[reqID]
	ch := s.asks[id]
	m.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- core.Effect{Kind: "cancel"}:
	default:
	}
	m.host.Observe(s.thread, func(t *core.Thread, _ time.Time) { core.DecisionClosed(t, id) },
		Ev{"decision.closed", map[string]any{"id": id, "answer": "withdrawn"}})
}

func (m *Manager) doInterrupt(threadID string) error {
	m.mu.Lock()
	s := m.sessions[threadID]
	if s != nil {
		s.interrupt = true
	}
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return s.c.Interrupt(ctx)
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

// StopAll ends every process, for shutdown.
func (m *Manager) StopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
}

// gitFacts reads the branch facts of a checkout after a run (SPEC 0.5 V0.2,
// V0.59 (3)): branch, base, commits ahead of the base, unpushed commits and
// changed files. Outside git, or on a timeout, only what was read is set.
func gitFacts(dir string) core.GitFacts {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	git := func(args ...string) (string, bool) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
		return strings.TrimSpace(string(out)), err == nil
	}
	var g core.GitFacts
	g.Branch, _ = git("branch", "--show-current")
	if g.Branch == "" {
		return g
	}
	if u, ok := git("remote", "get-url", "origin"); ok {
		g.Repo = NormalizeRemote(u)
	} else if root, ok := git("rev-parse", "--show-toplevel"); ok {
		g.Repo = root
	}
	g.Head, _ = git("rev-parse", "--short", "HEAD")
	for _, b := range []string{"origin/HEAD", "origin/main", "origin/master"} {
		if mb, ok := git("merge-base", b, "HEAD"); ok && mb != "" {
			g.Base = b
			if n, ok := git("rev-list", "--count", mb+"..HEAD"); ok {
				g.Ahead, _ = strconv.Atoi(n)
			}
			break
		}
	}
	if n, ok := git("rev-list", "--count", "@{upstream}..HEAD"); ok {
		g.Unpushed, _ = strconv.Atoi(n)
	} else {
		g.Unpushed = g.Ahead // no upstream: nothing of the branch is pushed
	}
	if st, ok := git("status", "--porcelain"); ok && st != "" {
		g.Dirty = len(strings.Split(st, "\n"))
	}
	return g
}

// NormalizeRemote turns a git remote into host/owner/name: git@github.com:o/r.git
// and https://github.com/o/r are both github.com/o/r.
func NormalizeRemote(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimSuffix(strings.TrimSuffix(u, "/"), ".git")
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		u = strings.TrimPrefix(u, p)
	}
	if i := strings.Index(u, "@"); i >= 0 {
		u = u[i+1:]
	}
	if i := strings.Index(u, ":"); i >= 0 && !strings.Contains(u[:i], "/") {
		u = u[:i] + "/" + u[i+1:]
	}
	if i := strings.Index(u, "/"); i > 0 {
		u = strings.ToLower(u[:i]) + u[i:]
	}
	return u
}

// gitBranch is the current branch of a checkout, or "" (SPEC 0.5 V0.2).
func gitBranch(dir string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// initHooks is what a process registers at start: the merge gate always,
// and the Stop hook only with the live view (SPEC 0.5 V0.37), so other
// threads get no extra round trip at the end of each turn.
func initHooks(view bool) claude.Msg {
	h := claude.Msg{"PreToolUse": []claude.Msg{{"matcher": "Bash", "hookCallbackIds": []string{"gate"}}}}
	if view {
		h["Stop"] = []claude.Msg{{"hookCallbackIds": []string{"view"}}}
		// V0.52: after any tool call, remind the agent when the view went stale mid-turn.
		h["PostToolUse"] = []claude.Msg{{"hookCallbackIds": []string{"viewnudge"}}}
	}
	return h
}
