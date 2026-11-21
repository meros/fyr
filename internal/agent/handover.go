package agent

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/proc"
	"github.com/meros/fyr/internal/provider"
)

// Handover (SPEC 0.5 V0.45, decision 0013): the owner moves a thread to
// another profile. The context goes over by the first rung that works.
const (
	RungResume = "resume" // the target resumes the same session
	RungAdopt  = "adopt"  // the target reads Fyr's export of the conversation
	RungBrief  = "brief"  // the source writes a handover, the target starts from it
)

// Conversations is what a host offers when it can write a thread's
// conversation as Markdown (rung 2). The engine implements it.
type Conversations interface {
	Conversation(threadID string) (string, error)
}

// Plan is the rung a handover would use now, and why rung 1 does not apply.
// CLI and Git are populated for cross-host handover inspection (decision 0039,
// V0.trans-host-handover).
type Plan struct {
	Rung string       `json:"rung"`
	Note string       `json:"note,omitempty"`
	CLI  string       `json:"cli,omitempty"`
	Git  *HandoverGit `json:"git,omitempty"`
}

// HandoverGit summarizes the source checkout's git state so the handover
// dialog can inform the owner about uncommitted/unpushed work and the target
// host can inject host-move context (decision 0039, V0.trans-host-handover).
type HandoverGit struct {
	Repo     string `json:"repo,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Default  string `json:"default,omitempty"`
	Head     string `json:"head,omitempty"`
	Ahead    int    `json:"ahead,omitempty"`
	Unpushed int    `json:"unpushed,omitempty"`
	Dirty    int    `json:"dirty,omitempty"`
	Worktree bool   `json:"worktree,omitempty"`
}

// BriefTimeout bounds the source's handover turn (rung 3).
var BriefTimeout = 10 * time.Minute

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

func (m *Manager) sessionsDir(p provider.Profile, ad cli.Adapter) string {
	// Legacy profiles may pass CLAUDE_CONFIG_DIR in Env rather than ConfigDir.
	if p.ConfigDir == "" && p.Auth.ConfigDir == "" && len(p.Env) > 0 && cliID(p.CLI) == "claude" {
		return p.ConfigDirPath()
	}
	return ad.SessionsDir(authOf(p))
}

// resumable reports whether the target can resume the thread's session,
// and the source transcript; it changes nothing.
func (m *Manager) resumable(t core.Thread, from provider.Profile) (string, error) {
	if t.SessionID == "" {
		return "", fmt.Errorf("The thread has no session yet")
	}
	ad, err := m.adapter(from)
	if err != nil {
		return "", err
	}
	dir := m.sessionsDir(from, ad)
	src := ad.SessionFile(dir, t.Cwd, t.SessionID)
	if src == "" {
		return "", fmt.Errorf("No session transcript for this thread in %s", dir)
	}
	return src, nil
}

// HandoverPlan says which rung a handover to profile would use now.
func (m *Manager) HandoverPlan(threadID, profile string) Plan {
	t, ok := m.host.Thread(threadID)
	if !ok {
		return Plan{Rung: RungAdopt, Note: "No such thread"}
	}
	from := m.cfg.Profile(t.Profile)
	p := Plan{Rung: RungResume, CLI: cliID(from.CLI), Git: readHandoverGit(t.Cwd)}
	if profile != "" {
		to := m.cfg.Profile(profile)
		if cliID(from.CLI) != cliID(to.CLI) {
			p.Rung, p.Note = RungAdopt, "The target runs another CLI"
			return p
		}
	}
	if _, err := m.resumable(t, from); err != nil {
		p.Rung, p.Note = RungAdopt, err.Error()
		return p
	}
	return p
}

func (m *Manager) handoverDir() string {
	if m.cfg.HandoverDir != "" {
		return m.cfg.HandoverDir
	}
	return filepath.Join(os.TempDir(), "fyr-handover")
}

// writeHandoverFile saves text as <dir>/<thread>-<time>-<kind>.md.
func (m *Manager) writeHandoverFile(threadID, kind, text string) (string, error) {
	dir := m.handoverDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	p := filepath.Join(dir, fmt.Sprintf("%s-%s-%s.md", threadID, time.Now().Format("20060102-150405"), kind))
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		return "", err
	}
	return p, nil
}

// end stops a process ensureAs has already taken out of the session map
// (the reader may still be draining it).
func (m *Manager) end(threadID string, s *session) {
	m.closeInput(s)
	select {
	case <-s.c.Done():
	case <-time.After(5 * time.Second):
		s.c.Kill()
		select { // #39: Kill ends it; never wait without a bound
		case <-s.c.Done():
		case <-time.After(proc.WaitDelay + time.Second):
			log.Printf("thread %s: the old process did not end after kill", threadID)
		}
	}
}

func (m *Manager) handover(threadID string, e core.Effect) {
	t, ok := m.host.Thread(threadID)
	if !ok {
		return
	}
	from, to := m.cfg.Profile(t.Profile), m.cfg.Profile(e.Profile)
	model := e.Model
	if model == "" {
		model = to.DefaultModel
	}
	// V0.model-choice: the effort picked for the target, else the thread's; every rung starts with it.
	asked := t.Effort
	if e.Effort != nil {
		asked = *e.Effort
	}
	effort := cli.EffortFor(to.ModelCaps, model, asked, to.DefaultEffort)
	var notes []string
	fail := func(err error) {
		log.Printf("thread %s: handover to %s: %v", threadID, e.Profile, err)
		m.observe(threadID, core.KindHandoverFailed, err.Error())
	}
	done := func(rung, sid, path string, first string) {
		data := map[string]any{"from": t.Profile, "to": e.Profile, "model": model, "rung": rung}
		if effort != "" {
			data["effort"] = effort
		}
		if path != "" {
			data["path"] = path
		}
		if len(notes) > 0 {
			data["note"] = strings.Join(notes, "; ")
		}
		m.observe(threadID, core.KindHandoverDone, core.HandoverDoneArgs{Profile: e.Profile, Model: model, SessionID: sid, Effort: e.Effort, Data: data})
		m.mu.Lock()
		s := m.sessions[threadID]
		m.mu.Unlock()
		if first == "" {
			if s != nil {
				m.armIdle(s)
			}
			return
		}
		// SPEC V0.96 (4): the new session learns the open changes' ids.
		if lines := core.OpenChangesText(&t); lines != "" {
			first += "\n\n" + lines
		}
		if err := m.send(threadID, core.Effect{Kind: core.EffectSend, Text: first, From: FromHandover}, nil); err != nil {
			log.Printf("thread %s: handover first message: %v", threadID, err)
		}
	}

	// Rung 1: resume the same session on the target.
	if src, err := m.resumable(t, from); err != nil {
		notes = append(notes, err.Error())
	} else if cliID(from.CLI) != cliID(to.CLI) {
		notes = append(notes, "The target runs another CLI")
	} else if ad, aerr := m.adapter(to); aerr != nil {
		notes = append(notes, aerr.Error())
	} else {
		fromDir, toDir := m.sessionsDir(from, ad), m.sessionsDir(to, ad)
		var cerr error
		if !sameDir(fromDir, toDir) {
			cerr = ad.CopySession(src, fromDir, toDir, t.SessionID)
		}
		if cerr != nil {
			notes = append(notes, "Could not copy the session: "+cerr.Error())
		} else {
			if _, err := m.ensureAs(threadID, &as{profile: to, model: model, session: t.SessionID, resume: true,
				effort: effort}); err != nil {
				notes = append(notes, "Resume failed: "+core.TruncHead(firstLine(err.Error()), 300))
			} else {
				done(RungResume, t.SessionID, "", "")
				return
			}
		}
	}

	// Rung 2: the target reads the old conversation from a file.
	var conv string
	var err error
	if c, ok := m.host.(Conversations); ok {
		conv, err = c.Conversation(threadID)
	} else {
		err = fmt.Errorf("this host cannot export conversations")
	}
	if err == nil && strings.TrimSpace(conv) == "" {
		err = fmt.Errorf("the conversation is empty")
	}
	var path string
	if err == nil {
		path, err = m.writeHandoverFile(threadID, "conversation", conv)
	}
	if err == nil {
		sid := newUUID()
		if _, err := m.ensureAs(threadID, &as{profile: to, model: model, session: sid, effort: effort, addDirs: []string{filepath.Dir(path)}}); err != nil {
			fail(fmt.Errorf("%s could not start: %v", name(to), err))
			return
		}
		done(RungAdopt, sid, path, fmt.Sprintf("[Fyr] Handover: the owner handed this thread over to you from %s. Your session is new: the conversation so far is in %s (Fyr wrote it from the thread's log). Read it, then continue the work from where it stopped. Start your reply with a short summary of where things stand.", name(from), path))
		return
	}
	notes = append(notes, "Could not write the conversation: "+err.Error())

	// Rung 3: one turn on the source writes a handover.
	brief, err := m.brief(threadID, to)
	if err != nil {
		fail(fmt.Errorf("the current agent could not write a handover: %v", err))
		return
	}
	path, perr := m.writeHandoverFile(threadID, "brief", brief)
	sid := newUUID()
	o := &as{profile: to, model: model, session: sid, effort: effort}
	saved := ""
	if perr == nil {
		o.addDirs = []string{filepath.Dir(path)}
		saved = " (also saved at " + path + ")"
	}
	if _, err := m.ensureAs(threadID, o); err != nil {
		fail(fmt.Errorf("%s could not start: %v", name(to), err))
		return
	}
	done(RungBrief, sid, path, fmt.Sprintf("[Fyr] Handover: the owner handed this thread over to you from %s. Your session is new; the previous agent wrote this handover%s:\n\n%s\n\nContinue the work from there. Start your reply with a short summary of where things stand.", name(from), saved, brief))
}

func name(p provider.Profile) string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

// brief runs one turn on the thread's own profile that writes the handover
// as its reply, and returns that reply. A turn that runs already ends
// first, and only the result of the turn that read the brief's message
// counts (#40).
func (m *Manager) brief(threadID string, to provider.Profile) (string, error) {
	s, err := m.ensure(threadID)
	if err != nil {
		return "", err
	}
	deadline := time.After(BriefTimeout)
	wait := func(ch chan claude.Msg) (claude.Msg, error) {
		select {
		case res, ok := <-ch:
			if !ok {
				return nil, fmt.Errorf("the agent process ended")
			}
			return res, nil
		case <-deadline:
			return nil, fmt.Errorf("no reply after %s", BriefTimeout)
		}
	}
	m.mu.Lock()
	var running chan claude.Msg
	if s.live {
		running = make(chan claude.Msg, 1)
		s.results = append(s.results, waiter{ch: running})
	}
	m.mu.Unlock()
	if running != nil {
		if _, err := wait(running); err != nil {
			return "", err
		}
	}
	ch := make(chan claude.Msg, 1)
	text := fmt.Sprintf("[Fyr] Handover: the owner is handing this thread over to another agent (%s) that starts without this conversation. Write the handover now as your whole reply, in Markdown: the goal, what is done, the current state (branch, PR, uncommitted changes), open items and decisions, and exactly how to continue. Do not change files or run anything else.", name(to))
	if err := m.send(threadID, core.Effect{Kind: core.EffectSend, Text: text, From: FromHandover}, ch); err != nil {
		return "", err
	}
	res, err := wait(ch)
	if err != nil {
		return "", err
	}
	out, _ := res["result"].(string)
	if isErr, _ := res["is_error"].(bool); isErr {
		return "", fmt.Errorf("%s", core.TruncHead(firstLine(out), 300))
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("the reply was empty")
	}
	return out, nil
}
