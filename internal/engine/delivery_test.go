package engine

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// messageID is the id the host gave a sent message (Reply.Result.messageId).
func messageID(t *testing.T, r api.Reply) string {
	t.Helper()
	m, _ := r.Result.(map[string]any)
	id, _ := m["messageId"].(string)
	if !r.OK || id == "" {
		t.Fatalf("send reply without a message id: %+v", r)
	}
	return id
}

func outboxState(th core.Thread, id string) string {
	for _, m := range th.Outbox {
		if m.ID == id {
			return m.State
		}
	}
	return ""
}

// stateEvent finds the message.state event of one message with one state.
func stateEvent(evs []store.Event, id, state string) bool {
	for _, e := range evs {
		if e.Type != "message.state" {
			continue
		}
		var d struct{ ID, State string }
		json.Unmarshal(e.Data, &d)
		if d.ID == id && d.State == state {
			return true
		}
	}
	return false
}

func (h *harness) waitEvent(id, msgID, state string) {
	h.t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if stateEvent(h.events(id), msgID, state) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatalf("no message.state %s for %s", state, msgID)
}

// SPEC 0.5 V0.33: a message sent during a step waits in the agent's queue,
// is read at the next tool boundary, and leaves the outbox.
func TestMessageDeliveryStates(t *testing.T) {
	h := newHarness(t, time.Minute)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "slow 700"})
	first := messageID(t, r)
	id := r.ThreadID
	h.waitEvent(id, first, "read")
	if !hasEvent(h.events(id), "user", `"id":"`+first+`"`) {
		t.Fatal("the user event carries no id")
	}
	steer := messageID(t, h.cmd("thread.send", id, core.Args{"text": "steer A"}))
	h.wait(id, "queued", func(th core.Thread, _ core.Attention) bool { return outboxState(th, steer) == "queued" })
	h.waitEvent(id, steer, "read")
	th := h.wait(id, "done", state(core.Done))
	if th.Summary != "took in: steer A" {
		t.Fatalf("summary %q", th.Summary)
	}
	if len(th.Outbox) != 0 {
		t.Fatalf("outbox after the run: %+v", th.Outbox)
	}
}

// SPEC 0.5 V0.33: Cancel withdraws an unread message; the agent never sees
// it; a read message cannot be cancelled (409).
func TestCancelUnreadMessage(t *testing.T) {
	h := newHarness(t, time.Minute)
	// The turn takes its queue in only after "release" below: under load a
	// fixed delay ran out before the cancel, and B was read (409).
	id := h.create("hold")
	h.wait(id, "working", state(core.Working))
	a := messageID(t, h.cmd("thread.send", id, core.Args{"text": "steer A"}))
	b := messageID(t, h.cmd("thread.send", id, core.Args{"text": "steer B"}))
	h.wait(id, "both queued", func(th core.Thread, _ core.Attention) bool {
		return outboxState(th, a) == "queued" && outboxState(th, b) == "queued"
	})
	if r := h.cmdID("cancel-b", "thread.cancelMessage", id, core.Args{"messageId": b}); !r.OK {
		t.Fatalf("cancel: %+v", r)
	}
	th, _ := h.e.Thread(id)
	if outboxState(th, b) != "" || !stateEvent(h.events(id), b, "cancelled") {
		t.Fatalf("cancelled message still pending: %+v", th.Outbox)
	}
	if r := h.cmdID("cancel-b", "thread.cancelMessage", id, core.Args{"messageId": b}); !r.OK {
		t.Fatalf("same command twice: %+v", r)
	}
	h.cmd("thread.send", id, core.Args{"text": "release"})
	th = h.wait(id, "done", state(core.Done))
	if th.Summary != "took in: steer A, release" {
		t.Fatalf("the agent saw %q", th.Summary)
	}
	if r := h.cmd("thread.cancelMessage", id, core.Args{"messageId": a}); r.OK || r.Status != 409 {
		t.Fatalf("cancel of a read message: %+v", r)
	}
	if r := h.cmd("thread.cancelMessage", id, core.Args{"messageId": "nope"}); r.OK || r.Status != 409 {
		t.Fatalf("cancel of an unknown message: %+v", r)
	}
}

// SPEC 0.5 V0.33: an unread message dies with the process: lost.
func TestUnreadMessageLostWithProcess(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hang")
	h.wait(id, "working", state(core.Working))
	m := messageID(t, h.cmd("thread.send", id, core.Args{"text": "later"}))
	h.wait(id, "queued", func(th core.Thread, _ core.Attention) bool { return outboxState(th, m) == "queued" })
	h.m.Stop(id)
	h.waitEvent(id, m, "lost")
	h.wait(id, "outbox empty", func(th core.Thread, _ core.Attention) bool { return len(th.Outbox) == 0 })
}

// SPEC 0.5 V0.33, V0.6: after a host restart, unread messages are lost,
// not resent.
func TestUnreadMessageLostOnRestart(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hang")
	h.wait(id, "working", state(core.Working))
	m := messageID(t, h.cmd("thread.send", id, core.Args{"text": "later"}))
	h.wait(id, "queued", func(th core.Thread, _ core.Attention) bool { return outboxState(th, m) == "queued" })
	h.e.st.Close() // the host dies first; its processes follow
	h.m.StopAll()
	st, _ := store.Open(h.db)
	defer st.Close()
	e2 := New(st)
	if err := e2.Load(); err != nil {
		t.Fatal(err)
	}
	th, _ := e2.Thread(id)
	if len(th.Outbox) != 0 {
		t.Fatalf("outbox after restart: %+v", th.Outbox)
	}
	evs, _ := e2.Events(id, 0, 1000)
	if !stateEvent(evs, m, "lost") {
		t.Fatal("no lost event after restart")
	}
}

// SPEC 0.5 V0.33: a message the agent could not start for is not dropped
// from the log: it is lost.
func TestMessageLostWhenAgentCannotStart(t *testing.T) {
	h := newHarness(t, time.Minute)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir + "/missing", "text": "hello"})
	m := messageID(t, r)
	h.waitEvent(r.ThreadID, m, "lost")
	if !hasEvent(h.events(r.ThreadID), "user", "hello") {
		t.Fatal("the message is missing from the log")
	}
}
