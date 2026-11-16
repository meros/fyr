package agent

import (
	"strings"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/core"
)

// A turn: the process's stream, read into observations on the host (runs,
// activity, tasks, the PR), and how a run ends.

func (m *Manager) read(s *session) {
	for msg := range s.c.Out() {
		m.handle(s, msg)
		handled(s, msg) // V0.90: a later fyrd does not get it again
	}
	<-s.c.Done()
	if p := pipeOf(s); p != nil {
		p.Forget() // V0.90: this fyrd saw the exit; the runner drops it
	}
	m.releaseCgroup(s) // V0.80
	m.mu.Lock()
	if m.sessions[s.thread] == s {
		delete(m.sessions, s.thread)
	}
	delete(m.tokens, s.token)
	live := s.live || len(s.asks) > 0
	for _, w := range s.results {
		close(w.ch)
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
			msg += ": " + core.TruncHead(strings.TrimSpace(err.Error()), 600)
		}
		m.observe(s.thread, core.KindProcessLost, msg)
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
				m.observe(s.thread, core.KindTask, core.TaskArgs{Task: k})
			}
			return
		}
		if k, ev, ok := taskFromSystem(msg); ok {
			var evs []core.Event
			if ev != "" {
				evs = append(evs, core.Event{Type: "task", Data: map[string]any{"kind": ev, "id": k.ID, "description": k.Description, "status": k.Status, "summary": k.Summary}})
			}
			m.observe(s.thread, core.KindTask, core.TaskArgs{Task: k, Log: evs})
			return
		}
		if msg["subtype"] == "init" {
			sid, _ := msg["session_id"].(string)
			model, _ := msg["model"].(string)
			m.observe(s.thread, core.KindSession, core.SessionArgs{SessionID: sid, Model: model})
		}
		return
	case "assistant", "user":
		if typ == "user" && msg["isSynthetic"] == true {
			// The Stop hook's reason, echoed by the CLI (V0.37): never
			// logged; the forced turn starts here.
			m.mu.Lock()
			s.inForced = s.stopBlocked
			forced := s.inForced
			m.mu.Unlock()
			if forced {
				// In stream order, so the client can show the forced turn's
				// text as one quiet line (V0.37).
				m.observe(s.thread, core.KindRecord, []core.Event{{Type: "view.forced", Data: map[string]any{}}})
			}
			return
		}
		if typ == "assistant" {
			m.noteForced(s, msg)
		}
		o := s.digest.feed(msg)
		d := core.DigestArgs{Activity: o.Activity, Summary: o.Summary, Subagents: o.Subagents, Log: []core.Event{{Type: "msg", Data: trimMsg(msg)}}}
		if o.PRURL != "" {
			repo, n, _ := core.ParsePR(o.PRURL)
			d.PR = &core.PR{URL: o.PRURL, Repo: repo, Number: n, State: "OPEN", Checks: core.ChecksPending}
		}
		m.observe(s.thread, core.KindDigest, d)
	case "result":
		m.mu.Lock()
		ended := s.live
		interrupted := s.interrupt
		s.live, s.interrupt = false, false
		m.mu.Unlock()
		m.mu.Lock()
		noReports := !s.lifecycle
		restore := s.stopBlocked && !s.forcedWork
		summary := s.summary
		s.stopBlocked, s.inForced, s.forcedWork, s.summary = false, false, false, ""
		refresh := s.refresh && len(s.out) == 0
		m.mu.Unlock()
		if noReports {
			// An older CLI reports no delivery: its turn read every message.
			for _, id := range m.unread(s) {
				m.delivered(s, id, "read")
			}
		}
		m.dropAdopted(s) // V0.90
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
			f = &core.Failure{Class: classify(text), Message: core.TruncHead(firstLine(text), 300)}
		}
		m.mu.Lock()
		var due []chan claude.Msg
		kept := s.results[:0]
		for _, w := range s.results {
			if w.msg != "" && s.out[w.msg] != "" {
				kept = append(kept, w) // its message is still queued
			} else {
				due = append(due, w.ch)
			}
		}
		s.results = kept
		m.mu.Unlock()
		for _, ch := range due {
			ch <- msg
		}
		data := map[string]any{"interrupted": interrupted, "costUsd": msg["total_cost_usd"], "durationMs": msg["duration_ms"], "failure": f}
		end := core.RunEndedArgs{Failure: f, Git: gitFacts(s.cwd), Data: data} // V0.59 (3): the git facts
		if restore {
			// V0.37: a forced turn that only updated the view keeps the
			// run's own last words as the summary.
			end.Summary = &summary
		}
		m.observe(s.thread, core.KindRunEnded, end)
		if refresh {
			m.mu.Lock()
			if m.sessions[s.thread] == s {
				delete(m.sessions, s.thread)
			}
			m.mu.Unlock()
			s.c.Close()
			go m.feed(s.thread) // the next process takes what waits
			return
		}
		m.armIdle(s)
		go m.feed(s.thread) // the agent is idle: what waits starts a turn
	}
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
		if name, _ := b["name"].(string); !strings.HasPrefix(name, "mcp__fyr__view_") && !upkeepTool(name) {
			s.forcedWork = true
		}
	}
}

// upkeepTool is a change tool that keeps records current (V0.62): a forced
// turn that only called these did no new work.
func upkeepTool(name string) bool {
	switch strings.TrimPrefix(name, "mcp__fyr__") {
	case "change_update", "change_status", "change_revise", "change_attach", "change_unchanged", "change_list":
		return true
	}
	return false
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
	m.observe(s.thread, core.KindRunStarted, nil)
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
