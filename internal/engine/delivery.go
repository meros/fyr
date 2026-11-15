package engine

import (
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// cancelMessage withdraws a message the agent has not read (SPEC 0.5 V0.33).
// It answers only after the agent confirmed, so a 200 means the agent never
// sees the message; a message already read, unknown or not withdrawable is a
// 409 without a receipt.
func (e *Engine) cancelMessage(c Cmd) Reply {
	id := c.Args.Str("messageId")
	if id == "" {
		return Reply{Error: "messageId is required", Status: 400}
	}
	e.press.Lock() // the receipt check and the receipt are one step
	defer e.press.Unlock()
	if r, done := e.replay(c.CommandID); done {
		return r
	}
	e.mu.Lock()
	t := e.threads[c.ThreadID]
	if t == nil {
		e.mu.Unlock()
		return Reply{Error: "no thread " + c.ThreadID, Status: 404}
	}
	waiting := core.OutboxState(t, id) != ""
	e.mu.Unlock()
	if !waiting {
		return Reply{Error: "The agent has already read this message", Status: 409}
	}
	if e.Agents == nil {
		return Reply{Error: "No agent runs this thread", Status: 409}
	}
	if err := e.Agents.Do(c.ThreadID, core.Effect{Kind: core.EffectCancelMessage, MessageID: id}); err != nil {
		return Reply{Error: err.Error(), Status: 409}
	}
	reply := Reply{OK: true, ThreadID: c.ThreadID, Status: 200}
	e.mu.Lock()
	now := e.Now()
	evs, err := e.st.Write(func(tx *store.Tx) error {
		if err := tx.Append(c.ThreadID, "command", map[string]any{"action": c.Action, "commandId": c.CommandID}, now); err != nil {
			return err
		}
		return tx.PutReceipt(c.CommandID, reply, now)
	})
	e.mu.Unlock()
	if err != nil {
		return Reply{Error: err.Error(), Status: 500}
	}
	e.Hub.Events(evs)
	return reply
}
