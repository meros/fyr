package agent

import (
	"fmt"
	"strings"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/core"
)

// The hooks of the CLI, and the agent feed's delivery through them.

// The agent feed's delivery (decision 0020 part 5, #47): the thread's
// notice queue reaches the agent here and nowhere else.

// take takes the notices due at a point.
func (m *Manager) take(s *session, threadID string, at core.Point) []core.Notice {
	a := core.FeedTake{At: at}
	if s != nil {
		a.View, a.Changes = s.viewTools, s.changeTools
	}
	out, err := m.host.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindFeedTake, Thread: threadID, Args: a})
	if err != nil {
		return nil
	}
	ns, _ := out.([]core.Notice)
	return ns
}

// feed delivers to an idle agent: every pending notice in one message that
// starts a turn, when one of them may wake it (not on a settled or archived
// thread). A running agent takes them in its hooks, or here after its run.
func (m *Manager) feed(threadID string) error {
	m.mu.Lock()
	s := m.sessions[threadID]
	busy := m.starting[threadID] != nil || (s != nil && (s.live || len(s.out) > 0))
	m.mu.Unlock()
	if busy {
		return nil
	}
	ns := m.take(s, threadID, core.AtIdle)
	if len(ns) == 0 {
		return nil
	}
	return m.send(threadID, core.Effect{Kind: core.EffectSend, Text: core.NoticeText(ns)}, nil)
}

// hook answers a hook callback of s's process by its callback id. The
// feed's notices are for the thread's own agent (#49): a hook input that
// names a subagent (the CLI adds agent_id and agent_type to the input of a
// tool call made inside one) takes nothing and leaves the queue as it is.
// A subagent's stop is SubagentStop, which Fyr does not register; the
// check covers the Stop hook too.
func (m *Manager) hook(s *session, id string, in claude.Msg) claude.Msg {
	switch id {
	case "view", "viewnudge":
		if sub, _ := in["agent_id"].(string); sub != "" {
			return nil
		}
		if id == "view" {
			return m.stopHook(s, in)
		}
		return m.toolHook(s)
	case "compact":
		return m.compactHook(s)
	}
	name, _ := in["tool_name"].(string)
	ti, _ := in["tool_input"].(map[string]any)
	if r := core.Gate(name, ti); r != "" {
		return claude.Msg{"hookSpecificOutput": claude.Msg{"hookEventName": "PreToolUse",
			"permissionDecision": "deny", "permissionDecisionReason": r}}
	}
	m.mu.Lock()
	onBash := m.cfg.OnBash
	m.mu.Unlock()
	if c, _ := ti["command"].(string); name == "Bash" && c != "" && onBash != nil && s != nil {
		if r := onBash(s.thread, c); r != "" {
			return claude.Msg{"hookSpecificOutput": claude.Msg{"hookEventName": "PreToolUse",
				"permissionDecision": "deny", "permissionDecisionReason": r}}
		}
	}
	n := core.GateNudge(name, ti) // V0.143
	// V0.164: the first Bash call of a subagent of a thread that runs
	// undeclared heavy work carries the text too, once per episode; the main
	// agent's own notice is not used up.
	if sub, _ := in["agent_id"].(string); sub != "" && name == "Bash" && s != nil {
		m.mu.Lock()
		note := m.cfg.UndeclaredNote
		m.mu.Unlock()
		if note != nil {
			if u := note(s.thread); u != "" {
				n = strings.TrimSpace(n + "\n" + u)
			}
		}
	}
	if n != "" {
		return claude.Msg{"hookSpecificOutput": claude.Msg{"hookEventName": "PreToolUse", "additionalContext": n}}
	}
	return nil
}

// stopHook answers the Stop hook (SPEC 0.5 V0.37): the feed's stop notices
// (the upkeep of the view and the changes) block the Stop once per run, in
// one reason, never while background work runs.
func (m *Manager) stopHook(s *session, in claude.Msg) claude.Msg {
	t, ok := m.host.Thread(s.thread)
	if !ok {
		return nil
	}
	bg, _ := in["background_tasks"].([]any)
	active, _ := in["stop_hook_active"].(bool)
	m.mu.Lock()
	blocked := s.stopBlocked
	m.mu.Unlock()
	if blocked || active || len(bg)+t.RunningTasks() > 0 {
		return nil
	}
	ns := m.take(s, s.thread, core.AtStop)
	if len(ns) == 0 {
		return nil
	}
	summary := firstLine(fmt.Sprint(orNil(in["last_assistant_message"])))
	if summary == "" {
		summary = t.Summary
	}
	m.mu.Lock()
	s.stopBlocked, s.summary, s.inForced, s.forcedWork = true, summary, false, false
	m.mu.Unlock()
	reasons := make([]string, len(ns))
	for i, n := range ns {
		reasons[i] = n.What
	}
	return claude.Msg{"decision": "block", "reason": strings.Join(reasons, "\n")}
}

// toolHook answers the PostToolUse hook: the notices due at a tool call
// (the wake and next ones, and the upkeep reminders, V0.52) as context.
func (m *Manager) toolHook(s *session) claude.Msg {
	ns := m.take(s, s.thread, core.AtTool)
	if len(ns) == 0 {
		return nil
	}
	return claude.Msg{"hookSpecificOutput": claude.Msg{"hookEventName": "PostToolUse", "additionalContext": core.NoticeText(ns)}}
}

func orNil(v any) any {
	if v == nil {
		return ""
	}
	return v
}

// initHooks is what a process registers at start: the merge gate always,
// and the agent feed's Stop and PostToolUse hooks and the SessionStart
// hook after a compaction (V0.96) only for a process with
// Fyr's tools, so other processes get no extra round trip.
func initHooks(feed bool) claude.Msg {
	h := claude.Msg{"PreToolUse": []claude.Msg{{"matcher": "Bash", "hookCallbackIds": []string{"gate"}}}}
	if feed {
		h["Stop"] = []claude.Msg{{"hookCallbackIds": []string{"view"}}}
		h["PostToolUse"] = []claude.Msg{{"hookCallbackIds": []string{"viewnudge"}}}
		h["SessionStart"] = []claude.Msg{{"matcher": "compact", "hookCallbackIds": []string{"compact"}}}
	}
	return h
}

// compactHook answers the SessionStart hook after a compaction (SPEC V0.96
// (5)): one line per open change, so the agent keeps the changes' ids.
func (m *Manager) compactHook(s *session) claude.Msg {
	t, ok := m.host.Thread(s.thread)
	if !ok {
		return nil
	}
	lines := core.OpenChangesText(&t)
	if lines == "" {
		return nil
	}
	return claude.Msg{"hookSpecificOutput": claude.Msg{"hookEventName": "SessionStart", "additionalContext": lines}}
}
