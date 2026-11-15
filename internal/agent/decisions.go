package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/core"
)

// Decisions (SPEC 4): a permission request becomes a Decision on the
// thread; the owner's answer goes back to the CLI.

// canUseTool turns a permission request into a Decision and waits for the
// answer (SPEC 5.6, 12.3). AskUserQuestion becomes kind "ask".
func (m *Manager) canUseTool(s *session, req claude.Msg) claude.Msg {
	tool, _ := req["tool_name"].(string)
	input, _ := req["input"].(map[string]any)
	reqID, _ := req["request_id"].(string)
	agentID, _ := req["agent_id"].(string)
	d := core.Decision{ID: "d_" + randHex(6), Assignee: core.Owner, Agent: agentID}
	if tool == "AskUserQuestion" {
		d.Kind = core.DecisionAsk
		b, _ := json.Marshal(input["questions"])
		json.Unmarshal(b, &d.Questions)
	} else {
		d.Kind, d.Tool, d.Input = core.DecisionApproval, tool, input
	}
	ch := make(chan core.Effect, 1)
	m.mu.Lock()
	s.asks[d.ID] = ch
	s.reqs[reqID] = d.ID
	m.mu.Unlock()
	m.observe(s.thread, core.KindDecisionOpened, d)

	ans, ok := <-ch
	m.mu.Lock()
	delete(s.asks, d.ID)
	delete(s.reqs, reqID)
	m.mu.Unlock()
	if !ok || ans.Kind == core.EffectCancel {
		return claude.Msg{"behavior": "deny", "message": "The question was withdrawn."}
	}
	if d.Kind == core.DecisionAsk {
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
	// Send under the lock and never block: read() closes the ask channels
	// under the same lock when the process ends, and the channel holds one
	// answer, so a second answer finds it full (review 2026-11-15).
	m.mu.Lock()
	s := m.sessions[threadID]
	var ch chan core.Effect
	if s != nil {
		ch = s.asks[e.DecisionID]
	}
	sent := false
	if ch != nil {
		select {
		case ch <- e:
			sent = true
		default:
		}
	}
	m.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("the question is no longer open in the agent")
	}
	if !sent {
		return fmt.Errorf("the question was already answered")
	}
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
	m.observe(threadID, core.KindRecord, []core.Event{{Type: "decision.closed", Data: map[string]any{"id": e.DecisionID, "answer": text}}})
	return nil
}

func (m *Manager) cancelRequest(s *session, reqID string) {
	m.mu.Lock()
	id := s.reqs[reqID]
	ch := s.asks[id]
	if ch != nil {
		select {
		case ch <- core.Effect{Kind: core.EffectCancel}:
		default:
		}
	}
	m.mu.Unlock()
	if ch == nil {
		return
	}
	m.observe(s.thread, core.KindDecisionGone, id)
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
