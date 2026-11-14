// Package agent runs one `claude` process per thread (SPEC 0.5 V0.3,
// V0.7, V0.12). It turns the stream into observations on the host and
// turns permission requests into Decisions.
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/core"
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
}

// FyrPrompt is the short block that steers agents to Fyr tools (V0.12).
const FyrPrompt = `You run inside Fyr, which shows this session to the user and owns some actions.
- To merge a pull request, call mcp__fyr__effect_request with action "pr.merge" and args {"pr": <url>, "method": "squash"|"merge"|"rebase"}. The user approves it; Fyr merges and sends you the result in a later message. Never run gh pr merge.
- When you open a PR some other way than gh pr create, call mcp__fyr__pr_link with its URL.
- When the user says the thread is finished once something happens, call mcp__fyr__settle_when.`

type Manager struct {
	host Host
	cfg  Config

	mu       sync.Mutex
	sessions map[string]*session
	tokens   map[string]string // MCP token → thread id
}

type session struct {
	thread    string
	cwd       string
	token     string
	c         *claude.Client
	turns     int
	interrupt bool
	asks      map[string]chan core.Effect // decision id → answer
	reqs      map[string]string           // control request id → decision id
	digest    *digest
	idle      *time.Timer
	log       io.Closer
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
	case "setModel":
		m.mu.Lock()
		s := m.sessions[threadID]
		m.mu.Unlock()
		if s != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			return s.c.SetModel(ctx, e.Model)
		}
		return nil // the next start passes --model
	}
	return fmt.Errorf("agent: unknown effect %q", e.Kind)
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
	s, err := m.ensure(threadID)
	if err != nil {
		m.host.Observe(threadID, func(t *core.Thread, now time.Time) {
			core.RunEnded(t, &core.Failure{Class: "process", Message: "Could not start the agent: " + err.Error(), At: now}, now)
		}, Ev{"run.ended", map[string]any{"failure": map[string]any{"class": "process", "message": err.Error()}}})
		return err
	}
	m.mu.Lock()
	s.turns++
	first := s.turns == 1
	if s.idle != nil {
		s.idle.Stop()
	}
	m.mu.Unlock()
	evs := []Ev{{"user", map[string]any{"text": e.Text, "images": len(e.Images)}}}
	if first {
		evs = append(evs, Ev{"run.started", map[string]any{}})
	}
	m.host.Observe(threadID, func(t *core.Thread, now time.Time) {
		if first {
			core.RunStarted(t, now)
		}
	}, evs...)
	return s.c.SendUser(content(e))
}

var mcpNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// ensure returns the live session of a thread, starting the process if needed.
func (m *Manager) ensure(threadID string) (*session, error) {
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
	p := m.cfg.Profile(t.Profile)
	args := []string{}
	if t.Model != "" {
		args = append(args, "--model", t.Model)
	}
	if t.SessionID != "" {
		args = append(args, "--resume", t.SessionID)
	} else {
		args = append(args, "--session-id", newUUID())
	}
	mode := p.PermissionMode
	if mode == "" {
		mode = "auto"
	}
	args = append(args, "--permission-mode", mode)
	s := &session{thread: threadID, cwd: t.Cwd, asks: map[string]chan core.Effect{}, reqs: map[string]string{}, digest: newDigest()}
	if m.cfg.MCPURL != "" {
		s.token = randHex(24)
		cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{"fyr": map[string]any{
			"type": "http", "url": m.cfg.MCPURL, "headers": map[string]string{"Authorization": "Bearer " + s.token}}}})
		args = append(args, "--mcp-config", string(cfg), "--allowedTools", "mcp__fyr__*")
	}
	if m.cfg.Prompt != "-" {
		args = append(args, "--append-system-prompt", m.cfg.Prompt)
	}
	spec := claude.Spec{Binary: p.Binary, Args: args, Dir: t.Cwd, Env: p.Env}
	if m.cfg.LogDir != "" {
		os.MkdirAll(m.cfg.LogDir, 0o700)
		f, err := os.OpenFile(filepath.Join(m.cfg.LogDir, threadID+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err == nil {
			spec.Log, s.log = f, f
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
	c, err := claude.Start(spec)
	if err != nil {
		dropToken()
		return nil, err
	}
	s.c = c
	c.OnCanUseTool = func(req claude.Msg) claude.Msg { return m.canUseTool(s, req) }
	c.OnCancel = func(reqID string) { m.cancelRequest(s, reqID) }
	c.OnHook = func(_ string, in claude.Msg) claude.Msg {
		name, _ := in["tool_name"].(string)
		ti, _ := in["tool_input"].(map[string]any)
		if r := core.Gate(name, ti); r != "" {
			return claude.Msg{"hookSpecificOutput": claude.Msg{"hookEventName": "PreToolUse",
				"permissionDecision": "deny", "permissionDecisionReason": r}}
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx, claude.Msg{"PreToolUse": []claude.Msg{{"matcher": "Bash", "hookCallbackIds": []string{"gate"}}}}); err != nil {
		c.Kill()
		<-c.Done()
		dropToken()
		return nil, fmt.Errorf("initialize: %w", err)
	}
	m.mu.Lock()
	m.sessions[threadID] = s
	m.mu.Unlock()
	go m.read(s)
	return s, nil
}

func (m *Manager) read(s *session) {
	for msg := range s.c.Out {
		m.handle(s, msg)
	}
	<-s.c.Done()
	m.mu.Lock()
	if m.sessions[s.thread] == s {
		delete(m.sessions, s.thread)
	}
	delete(m.tokens, s.token)
	live := s.turns > 0 || len(s.asks) > 0
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
	switch typ {
	case "system":
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
		s.turns--
		ended := s.turns <= 0
		interrupted := s.interrupt
		if ended {
			s.turns = 0
			s.interrupt = false
		}
		m.mu.Unlock()
		if !ended {
			return
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
		data := map[string]any{"interrupted": interrupted, "costUsd": msg["total_cost_usd"], "durationMs": msg["duration_ms"], "failure": f}
		branch := gitBranch(s.cwd)
		m.host.Observe(s.thread, func(t *core.Thread, now time.Time) {
			if f != nil {
				f.At = now
			}
			core.RunEnded(t, f, now)
			t.Branch = branch
		}, Ev{"run.ended", data})
		m.armIdle(s)
	}
}

func (m *Manager) armIdle(s *session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.idle != nil {
		s.idle.Stop()
	}
	s.idle = time.AfterFunc(m.cfg.IdleStop, func() {
		m.mu.Lock()
		quiet := s.turns == 0 && len(s.asks) == 0
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
