package agent

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/core"
)

// SPEC 0.5 V0.142: the watchdog. Fyr writes a message or a control request
// and the CLI acknowledges it (command_lifecycle, control_response). When
// it does not, the process is unresponsive: the thread says since when and
// why, the owner can restart it, and Fyr restarts it itself after
// RestartAfter. A restart ends the process with its background tasks,
// resumes the same session in a new one and delivers the unread messages
// again.

// write sends a user message and arms its acknowledgement check.
func (m *Manager) write(s *session, id string, content []claude.Msg) error {
	m.mu.Lock()
	s.sentAt[id] = time.Now()
	watch := s.lifecycle // an older CLI reports no delivery
	m.mu.Unlock()
	if watch {
		time.AfterFunc(m.cfg.AckTimeout, func() {
			m.mu.Lock()
			waiting := s.out[id] == "sent" && s.restarting == ""
			m.mu.Unlock()
			if waiting {
				m.markUnresponsive(s, fmt.Sprintf("it did not take a message within %s", short(m.cfg.AckTimeout)))
			}
		})
	}
	return s.c.SendUser(content, id)
}

// heard notes output from the process. It clears the unresponsive state
// unless the input is closed or a message still waits past its ack time.
func (m *Manager) heard(s *session) {
	now := time.Now()
	m.mu.Lock()
	s.lastOut = now
	clear := s.unresponsive && !s.inputClosed && s.restarting == ""
	for id, st := range s.out {
		if st == "sent" && now.Sub(s.sentAt[id]) >= m.cfg.AckTimeout {
			clear = false
		}
	}
	if clear {
		s.unresponsive = false
		if s.restartAt != nil {
			s.restartAt.Stop()
		}
	}
	m.mu.Unlock()
	if clear {
		m.observe(s.thread, core.KindResponsive, nil)
	}
}

// markUnresponsive reports the process unresponsive once and arms the
// automatic restart.
func (m *Manager) markUnresponsive(s *session, why string) {
	m.mu.Lock()
	if s.unresponsive || s.restarting != "" || m.sessions[s.thread] != s {
		m.mu.Unlock()
		return
	}
	s.unresponsive = true
	since := s.lastOut
	after := m.cfg.RestartAfter
	s.restartAt = time.AfterFunc(after, func() {
		m.mu.Lock()
		still := s.unresponsive && m.sessions[s.thread] == s
		m.mu.Unlock()
		if still {
			if err := m.restart(s.thread, "automatically, after "+short(after)+" without a response"); err != nil {
				log.Printf("thread %s: restart: %v", s.thread, err)
			}
		}
	})
	m.mu.Unlock()
	if t, ok := m.host.Thread(s.thread); ok {
		if ts := runningTasks(t); len(ts) > 0 {
			why += "; a background task holds the process: " + strings.Join(ts, ", ")
		}
	}
	log.Printf("thread %s: agent not responding: %s", s.thread, why)
	m.observe(s.thread, core.KindUnresponsive, core.UnresponsiveArgs{Since: since, Reason: why})
}

// restart ends the thread's process and resumes its session in a new one,
// delivering the unread messages again.
func (m *Manager) restart(threadID, why string) error {
	m.mu.Lock()
	s := m.sessions[threadID]
	if s == nil {
		// The process is gone already (a fact left from it): start the
		// session again with what waits.
		m.mu.Unlock()
		m.observe(threadID, core.KindRestarted, core.RestartedArgs{Why: why})
		return m.redeliver(threadID, why, nil)
	}
	if s.restarting != "" {
		m.mu.Unlock()
		return fmt.Errorf("the agent is restarting")
	}
	s.restarting = why
	if s.restartAt != nil {
		s.restartAt.Stop()
	}
	delete(m.sessions, threadID)
	m.mu.Unlock()
	var tasks []string
	if t, ok := m.host.Thread(threadID); ok {
		tasks = runningTasks(t)
	}
	log.Printf("thread %s: restarting the agent: %s", threadID, why)
	s.c.Kill()
	select {
	case <-s.gone:
	case <-time.After(15 * time.Second):
		return fmt.Errorf("the agent process did not end")
	}
	m.observe(threadID, core.KindRestarted, core.RestartedArgs{Why: why, Tasks: tasks})
	return m.redeliver(threadID, why, tasks)
}

// redeliver starts the thread's session again and writes its unread
// messages, the first with Fyr's note of what happened. The conversation
// already shows them; only their delivery state moves.
func (m *Manager) redeliver(threadID, why string, tasks []string) error {
	t, ok := m.host.Thread(threadID)
	if !ok {
		return fmt.Errorf("no thread %s", threadID)
	}
	note := "[Fyr] Your previous process could not read messages (its input was closed). This process resumes the same session; the messages below waited for you."
	if why != "" {
		note = "[Fyr] Fyr restarted your process (" + why + ") because it stopped taking messages. This process resumes the same session."
	}
	if len(tasks) > 0 {
		note += " Its background tasks ended with it: " + strings.Join(tasks, ", ") + ". Start again only what you still need, detached with a bounded lifetime."
	}
	pending := append([]core.OutMsg(nil), t.Outbox...)
	s, err := m.ensure(threadID)
	if err != nil {
		for _, o := range pending {
			m.observe(threadID, core.KindMessageState, core.MessageStateArgs{ID: o.ID, State: "lost"})
		}
		return err
	}
	if len(pending) == 0 {
		pending = []core.OutMsg{{ID: newUUID()}}
	}
	for i, o := range pending {
		text := o.Text
		if i == 0 {
			text = strings.TrimSuffix(note+"\n\n"+text, "\n\n")
		} else if text == "" {
			m.observe(threadID, core.KindMessageState, core.MessageStateArgs{ID: o.ID, State: "lost"})
			continue
		}
		m.mu.Lock()
		first := !s.live
		s.live = true
		s.out[o.ID] = "sent"
		m.mu.Unlock()
		var evs []core.Event
		if first {
			evs = []core.Event{{Type: "run.started", Data: map[string]any{}}}
		}
		m.observe(threadID, core.KindMessageSent, core.MessageSentArgs{ID: o.ID, First: first, Log: evs, Text: o.Text})
		if err := m.write(s, o.ID, []claude.Msg{{"type": "text", "text": text}}); err != nil {
			return err
		}
	}
	return nil
}

// runningTasks names the thread's running background tasks.
func runningTasks(t core.Thread) []string {
	var out []string
	for _, k := range t.Tasks {
		if k.Status == core.TaskRunning {
			d := k.Description
			if d == "" {
				d = k.ID
			}
			out = append(out, d)
		}
	}
	return out
}

// short writes a duration as "60 s" or "10 min".
func short(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf("%d min", int(d/time.Minute))
	}
	if d >= time.Second {
		return fmt.Sprintf("%d s", int(d.Round(time.Second)/time.Second))
	}
	return d.String()
}
