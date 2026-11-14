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
	"github.com/meros/fyr/internal/cli/claudecli"
	"github.com/meros/fyr/internal/core"
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
type Plan struct {
	Rung string `json:"rung"`
	Note string `json:"note,omitempty"`
}

// BriefTimeout bounds the source's handover turn (rung 3).
var BriefTimeout = 10 * time.Minute

// ConfigDirPath is where the profile's CLI keeps its sessions: the
// profile's configDir, else CLAUDE_CONFIG_DIR in its env, else the
// daemon's, else ~/.claude.
func (p Profile) ConfigDirPath() string {
	if p.ConfigDir != "" {
		return expandHome(p.ConfigDir)
	}
	for _, kv := range p.Env {
		if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok && v != "" {
			return expandHome(v)
		}
	}
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return expandHome(v)
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".claude")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return h + p[1:]
	}
	return p
}

// sessionFile finds <config>/projects/<dir>/<session>.jsonl (the claude
// adapter's layout, V0.47).
func sessionFile(cfgDir, cwd, sid string) string { return claudecli.SessionFile(cfgDir, cwd, sid) }

func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	sa, err1 := os.Stat(a)
	sb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(sa, sb)
}

// resumable reports whether the target can resume the thread's session,
// and the source transcript; it changes nothing.
func resumable(t core.Thread, from Profile) (string, error) {
	if t.SessionID == "" {
		return "", fmt.Errorf("The thread has no session yet")
	}
	src := sessionFile(from.ConfigDirPath(), t.Cwd, t.SessionID)
	if src == "" {
		return "", fmt.Errorf("No session transcript for this thread in %s", from.ConfigDirPath())
	}
	return src, nil
}

// HandoverPlan says which rung a handover to profile would use now.
func (m *Manager) HandoverPlan(threadID, profile string) Plan {
	t, ok := m.host.Thread(threadID)
	if !ok {
		return Plan{Rung: RungAdopt, Note: "No such thread"}
	}
	if _, err := resumable(t, m.cfg.Profile(t.Profile)); err != nil {
		return Plan{Rung: RungAdopt, Note: err.Error()}
	}
	return Plan{Rung: RungResume}
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

// stopNow ends a thread's process and forgets it at once, so the next
// start makes a new one (the reader may still be draining the old one).
func (m *Manager) stopNow(threadID string) {
	m.mu.Lock()
	s := m.sessions[threadID]
	delete(m.sessions, threadID)
	m.mu.Unlock()
	if s == nil {
		return
	}
	s.c.Close()
	select {
	case <-s.c.Done():
	case <-time.After(5 * time.Second):
		s.c.Kill()
		<-s.c.Done()
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
	var notes []string
	fail := func(err error) {
		log.Printf("thread %s: handover to %s: %v", threadID, e.Profile, err)
		m.host.Observe(threadID, func(t *core.Thread, _ time.Time) { t.Handover = nil },
			Ev{"effect.error", map[string]any{"kind": "handover", "error": err.Error()}})
	}
	done := func(rung, sid, path string, first string) {
		data := map[string]any{"from": t.Profile, "to": e.Profile, "model": model, "rung": rung}
		if path != "" {
			data["path"] = path
		}
		if len(notes) > 0 {
			data["note"] = strings.Join(notes, "; ")
		}
		m.host.Observe(threadID, func(t *core.Thread, _ time.Time) {
			t.Profile, t.Model, t.SessionID, t.Handover = e.Profile, model, sid, nil
			t.Failure = nil
		}, Ev{"handover", data})
		m.mu.Lock()
		s := m.sessions[threadID]
		m.mu.Unlock()
		if first == "" {
			if s != nil {
				m.armIdle(s)
			}
			return
		}
		if err := m.send(threadID, core.Effect{Kind: "send", Text: first, From: FromHandover}); err != nil {
			log.Printf("thread %s: handover first message: %v", threadID, err)
		}
	}

	// Rung 1: resume the same session on the target.
	if src, err := resumable(t, from); err != nil {
		notes = append(notes, err.Error())
	} else if from.CLI != to.CLI {
		notes = append(notes, "The target runs another CLI")
	} else if ad, aerr := m.adapter(to); aerr != nil {
		notes = append(notes, aerr.Error())
	} else {
		fromDir, toDir := from.ConfigDirPath(), to.ConfigDirPath()
		var cerr error
		if !sameDir(fromDir, toDir) {
			cerr = ad.CopySession(src, fromDir, toDir, t.SessionID)
		}
		if cerr != nil {
			notes = append(notes, "Could not copy the session: "+cerr.Error())
		} else {
			m.stopNow(threadID)
			if _, err := m.ensureAs(threadID, &as{profile: to, model: model, session: t.SessionID, resume: true,
				effort: cli.EffortFor(to.ModelCaps, model, t.Effort, to.DefaultEffort)}); err != nil {
				notes = append(notes, "Resume failed: "+clip(firstLine(err.Error()), 300))
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
		m.stopNow(threadID)
		sid := newUUID()
		if _, err := m.ensureAs(threadID, &as{profile: to, model: model, session: sid, addDirs: []string{filepath.Dir(path)}}); err != nil {
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
	m.stopNow(threadID)
	sid := newUUID()
	o := &as{profile: to, model: model, session: sid}
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

func name(p Profile) string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

// brief runs one turn on the thread's own profile that writes the handover
// as its reply, and returns that reply.
func (m *Manager) brief(threadID string, to Profile) (string, error) {
	s, err := m.ensure(threadID)
	if err != nil {
		return "", err
	}
	ch := make(chan claude.Msg, 1)
	m.mu.Lock()
	s.results = append(s.results, ch)
	m.mu.Unlock()
	text := fmt.Sprintf("[Fyr] Handover: the owner is handing this thread over to another agent (%s) that starts without this conversation. Write the handover now as your whole reply, in Markdown: the goal, what is done, the current state (branch, PR, uncommitted changes), open items and decisions, and exactly how to continue. Do not change files or run anything else.", name(to))
	if err := m.send(threadID, core.Effect{Kind: "send", Text: text, From: FromHandover}); err != nil {
		return "", err
	}
	select {
	case res, ok := <-ch:
		if !ok {
			return "", fmt.Errorf("the agent process ended")
		}
		out, _ := res["result"].(string)
		if isErr, _ := res["is_error"].(bool); isErr {
			return "", fmt.Errorf("%s", clip(firstLine(out), 300))
		}
		if strings.TrimSpace(out) == "" {
			return "", fmt.Errorf("the reply was empty")
		}
		return out, nil
	case <-time.After(BriefTimeout):
		return "", fmt.Errorf("no reply after %s", BriefTimeout)
	}
}
