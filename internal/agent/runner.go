package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/runner"
)

// The runner (SPEC 0.5 V0.90, decision 0023): with Config.Runner set, each
// thread's process runs in the launcher's runner, outside fyrd. A restart of
// fyrd (a deploy, a crash) leaves it running; the next fyrd adopts it.

// procMeta is what fyrd tells the runner about a process; Adopt reads it
// back. Keep the fields: a newer fyrd reads what an older one wrote.
type procMeta struct {
	Thread      string `json:"thread"`
	CLI         string `json:"cli,omitempty"`
	Token       string `json:"token,omitempty"`
	Effort      string `json:"effort,omitempty"`
	ViewTools   bool   `json:"viewTools,omitempty"`
	ChangeTools bool   `json:"changeTools,omitempty"`
	Cgroup      bool   `json:"cgroup,omitempty"`
	// Tools: the hash of the MCP tool list it started with
	// (V0.tool-reload); "" for a process from before.
	Tools string `json:"tools,omitempty"`
	// Timers: the process has the CLI's own timers (V0.wakeups-via-watch).
	Timers bool `json:"timers,omitempty"`
}

// threadOf is the thread a runner process belongs to: its key, else the
// thread in its note.
func threadOf(p runner.Proc) string {
	if p.Key != "" {
		return p.Key
	}
	var meta procMeta
	json.Unmarshal(p.Meta, &meta)
	return meta.Thread
}

// Kept lists the threads whose processes the runner holds, from its list.
// The engine's restart repair leaves their runs and questions live.
func Kept(procs []runner.Proc) map[string]bool {
	out := map[string]bool{}
	for _, p := range procs {
		if t := threadOf(p); t != "" && !p.Exited {
			out[t] = true
		}
	}
	return out
}

// Adopt takes over the processes the runner kept (V0.90 (3)). Call it once,
// after the engine loaded and before anything sends: a thread's process
// goes on where the last fyrd left it. Each thread keeps one process: the
// live one, the newest if there were ever two. The others end. An exited
// process is read to its end only when its thread has no live one, so its
// last output and its exit reach the thread.
func (m *Manager) Adopt(procs []runner.Proc) {
	// #167: a runner none of whose processes belongs to a thread here is
	// another install's (a fyrd an agent started with the live install's
	// variables). Ending them would end the live install's runs: leave
	// them alone; the runner's own rules end what nobody collects.
	known := false
	for _, p := range procs {
		if _, ok := m.host.Thread(threadOf(p)); ok {
			known = true
			break
		}
	}
	if len(procs) > 0 && !known {
		log.Printf("runner: none of its %d processes belongs to a thread here; another install's runner, left alone", len(procs))
		return
	}
	best := map[string]runner.Proc{}
	for _, p := range procs {
		t := threadOf(p)
		b, seen := best[t]
		if !seen || better(p, b) {
			best[t] = p
		}
	}
	for _, p := range procs {
		t := threadOf(p)
		if best[t].ID == p.ID {
			continue
		}
		if conn, err := m.cfg.Runner.Attach(p.ID); err == nil {
			conn.Kill()
			conn.Forget()
			log.Printf("runner: %s: ended, thread %s has %s", p.ID, t, best[t].ID)
		}
	}
	for t, p := range best {
		s, err := m.attach(t, p)
		if err != nil {
			log.Printf("runner: %s: %v", p.ID, err)
			continue
		}
		m.mu.Lock()
		taken := m.sessions[t] != nil
		if !taken {
			m.sessions[t] = s
		}
		m.mu.Unlock()
		if taken {
			m.drop(s, "the thread has a process")
			continue
		}
		live := s.live // read before the reader can change it
		go m.read(s)
		if !live {
			m.armIdle(s)
		}
		m.watchTools(s)
		log.Printf("runner: adopted %s for thread %s (pid %d, running %v)", p.ID, t, p.Pid, live)
	}
}

// better: a live process wins over an exited one, then the newer one.
func better(a, b runner.Proc) bool {
	if a.Exited != b.Exited {
		return !a.Exited
	}
	return a.Started.After(b.Started)
}

// adoptExisting is the start of a thread whose process the runner already
// holds (its spawn was refused, V0.90 (1)): fyrd links to that process
// instead of starting a second one.
func (m *Manager) adoptExisting(threadID, procID string) (*session, error) {
	procs, err := m.cfg.Runner.List()
	if err != nil {
		return nil, err
	}
	for _, p := range procs {
		if p.ID == procID {
			s, err := m.attach(threadID, p)
			if err == nil {
				log.Printf("runner: thread %s already had %s; linked to it", threadID, p.ID)
				m.watchTools(s)
			}
			return s, err
		}
	}
	return nil, fmt.Errorf("the runner holds a process for this thread, but it is gone")
}

// attach links to a runner process and builds its session from the thread
// and the process's note. The caller registers it and starts read.
func (m *Manager) attach(threadID string, p runner.Proc) (*session, error) {
	var meta procMeta
	json.Unmarshal(p.Meta, &meta)
	conn, err := m.cfg.Runner.Attach(p.ID)
	if err != nil {
		return nil, err
	}
	drop := func(why string) (*session, error) {
		conn.Kill()
		conn.Forget()
		return nil, fmt.Errorf("ended: %s", why)
	}
	t, ok := m.host.Thread(threadID)
	if !ok {
		return drop("no thread " + threadID)
	}
	id := meta.CLI
	if id == "" {
		id = claudecli.ID
	}
	ad, ok := m.cfg.Adapters[id]
	if !ok && id == claudecli.ID {
		ad, ok = claudecli.Adapter{}, true
	}
	re, _ := ad.(cli.Reattacher)
	if !ok || re == nil {
		return drop("no adapter can reattach " + id)
	}
	s := &session{thread: t.ID, proc: p.ID, cwd: t.Cwd, token: meta.Token, asks: map[string]chan core.Effect{}, reqs: map[string]string{},
		digest: newDigest(), out: map[string]string{}, cancelling: map[string]bool{}, effort: meta.Effort, toolsHash: meta.Tools, timers: meta.Timers,
		viewTools: meta.ViewTools, changeTools: meta.ChangeTools, live: t.Running, lifecycle: true,
		bg: t.RunningTasks(), // V0.133: until the CLI reports its tasks again
		// V0.143: a process whose input an earlier fyrd closed reads
		// nothing Fyr writes; the runner knows.
		inputClosed: p.InputClosed, lastOut: t.ActivityAt, sentAt: map[string]time.Time{}, held: map[string]bool{}, gone: make(chan struct{})}
	if s.lastOut.IsZero() {
		s.lastOut = time.Now()
	}
	for _, o := range t.Outbox {
		s.out[o.ID] = o.State
	}
	// The open questions come again from the CLI's kept requests; an answer
	// that comes first waits in its channel (V0.90 (4)).
	s.adopted = map[string]bool{}
	for _, d := range t.Decisions {
		if d.NeedsProcess() {
			s.asks[d.ID] = make(chan core.Effect, 1)
			s.adopted[d.ID] = true
		}
	}
	if meta.Cgroup && m.cfg.Cgroups != nil {
		m.cfg.Cgroups.Hold(t.ID) // the hold this process had in the last fyrd
		s.cgroup = true
	}
	var w io.Writer
	if m.cfg.LogDir != "" {
		if f, err := os.OpenFile(filepath.Join(m.cfg.LogDir, t.ID+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			s.log, w = f, f
		}
	}
	if s.token != "" {
		m.mu.Lock()
		m.tokens[s.token] = t.ID // the CLI calls Fyr's tools with the token it started with
		m.mu.Unlock()
	}
	c, err := re.Reattach(conn, w, cli.Handlers{
		CanUseTool: func(req claude.Msg) claude.Msg { return m.canUseTool(s, req) },
		Cancel:     func(reqID string) { m.cancelRequest(s, reqID) },
		Hook:       func(id string, in claude.Msg) claude.Msg { return m.hook(s, id, in) },
	})
	if err != nil {
		m.unhold(s)
		return drop(err.Error())
	}
	s.c = c
	return s, nil
}

// unhold gives back what attach took for a session that never ran.
func (m *Manager) unhold(s *session) {
	m.mu.Lock()
	delete(m.tokens, s.token)
	m.mu.Unlock()
	m.releaseCgroup(s)
	if s.log != nil {
		s.log.Close()
	}
}

// drop ends an attached session that lost to another one.
func (m *Manager) drop(s *session, why string) {
	m.unhold(s)
	if p := pipeOf(s); p != nil {
		p.Kill()
		p.Forget()
	}
	log.Printf("runner: %s: ended: %s", s.proc, why)
}

// pipeOf is the runner link of a session, or nil for a child of fyrd.
func pipeOf(s *session) *runner.Conn {
	if s == nil || s.c == nil {
		return nil
	}
	pp, ok := s.c.(interface{ Pipe() runner.Pipe })
	if !ok {
		return nil
	}
	c, _ := pp.Pipe().(*runner.Conn)
	return c
}

// handled acks a message the manager is done with (V0.90 (2)).
func handled(s *session, msg claude.Msg) {
	if h, ok := s.c.(interface{ Handled(claude.Msg) }); ok {
		h.Handled(msg)
	}
}

// decisionID names the Decision of one permission request of one process:
// the runner asks a restarted fyrd again, and the same request must find
// the same Decision.
func decisionID(proc, reqID string) string {
	sum := sha256.Sum256([]byte(proc + "/" + reqID))
	return "d_" + hex.EncodeToString(sum[:6])
}

// dropAdopted withdraws the questions an adopted process had open that the
// runner never asked again: the CLI withdrew them while no fyrd listened.
// It runs when the turn ends.
func (m *Manager) dropAdopted(s *session) {
	m.mu.Lock()
	var gone []string
	for id := range s.adopted {
		if ch := s.asks[id]; ch != nil {
			close(ch)
			delete(s.asks, id)
		}
		gone = append(gone, id)
	}
	s.adopted = nil
	m.mu.Unlock()
	for _, id := range gone {
		m.observe(s.thread, core.KindDecisionGone, id)
	}
}

// watchTools checks an adopted process for an older build's MCP tools
// once ToolsGrace has passed (V0.tool-reload).
func (m *Manager) watchTools(s *session) {
	if m.cfg.Tools != nil && s.token != "" && s.toolsHash != m.cfg.Tools() {
		time.AfterFunc(m.cfg.ToolsGrace, func() { m.reloadTools(s) })
	}
}

// reloadTools ends an adopted process that still has an older build's MCP
// tools (V0.tool-reload): at once when it is idle, else when its turn ends;
// never in a turn. The next message resumes the session with the new
// tools. A process that read this build's tools, or holds the server
// stream open, is left alone: its CLI took the new list itself.
func (m *Manager) reloadTools(s *session) {
	if m.cfg.Tools == nil || m.cfg.ToolsCurrent == nil || s.token == "" {
		return
	}
	if s.toolsHash == m.cfg.Tools() || m.cfg.ToolsCurrent(s.token) {
		return
	}
	why := "Fyr restarted this agent's process to load the new tools"
	if m.cfg.Build != "" {
		why += " (build " + m.cfg.Build + ")"
	}
	// As refreshView, on this session only: the thread may have another
	// process by now.
	m.mu.Lock()
	if m.sessions[s.thread] != s || s.inputClosed {
		m.mu.Unlock()
		return
	}
	s.reloadWhy = why
	idle := !s.live && len(s.asks) == 0 && len(s.out) == 0 && s.bg == 0 // V0.133
	if idle {
		delete(m.sessions, s.thread) // the next send starts a new process
	} else {
		s.refresh = true
	}
	m.mu.Unlock()
	log.Printf("thread %s: %s", s.thread, why)
	if idle {
		m.closeInput(s)
		m.refreshed(s)
	}
}

// refreshed reports a refresh that loaded the new tools, once its process
// ended (V0.tool-reload).
func (m *Manager) refreshed(s *session) {
	m.mu.Lock()
	why := s.reloadWhy
	s.reloadWhy = ""
	m.mu.Unlock()
	if why != "" {
		m.observe(s.thread, core.KindRestarted, core.RestartedArgs{Why: why})
	}
}
