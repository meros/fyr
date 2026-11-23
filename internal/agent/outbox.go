package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/meros/fyr/internal/claude"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/usage"
)

// The outbox (SPEC 0.5 V0.33): a message to the agent, its attachments, and
// its delivery state until the CLI reads it.

// inlineImage is the set of image types Claude takes as image blocks.
var inlineImage = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// withAttachments adds image blocks for small images and lists every
// attachment with its path in the text (SPEC 0.5 V0.18).
func (m *Manager) withAttachments(e core.Effect) (core.Effect, []map[string]any) {
	var metas []map[string]any
	if len(e.Attachments) == 0 || m.cfg.Attachments == nil {
		return e, nil
	}
	var lines []string
	for _, id := range e.Attachments {
		a, err := m.cfg.Attachments(id)
		if err != nil {
			continue
		}
		metas = append(metas, map[string]any{"id": a.ID, "name": a.Name, "mediaType": a.MediaType, "size": a.Size})
		if inlineImage[a.MediaType] && a.Size <= 5<<20 {
			if b, err := os.ReadFile(a.Path); err == nil {
				e.Images = append(e.Images, core.Image{MediaType: a.MediaType, Data: base64.StdEncoding.EncodeToString(b)})
			}
		}
		lines = append(lines, fmt.Sprintf("Attached: %s (%s, %s) at %s", a.Name, a.MediaType, humanSize(a.Size), a.Path))
	}
	if len(lines) > 0 {
		if e.Text != "" {
			e.Text += "\n\n"
		}
		e.Text += strings.Join(lines, "\n")
	}
	return e, metas
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d kB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
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

// send writes a user message; a non-nil result gets the result of the turn
// that reads it.
func (m *Manager) send(threadID string, e core.Effect, result chan claude.Msg) error {
	return m.sendAs(threadID, e, result, triggerOf(e))
}

// sendAs is send with what the message starts when it starts a turn
// (V0.usage-ledger).
func (m *Manager) sendAs(threadID string, e core.Effect, result chan claude.Msg, trig string) error {
	if e.MessageID == "" {
		e.MessageID = newUUID()
	}
	userText := e.Text
	e, metas := m.withAttachments(e)
	// V0.message-before-start: the owner's message is in the conversation
	// (Sent) before the process starts, so a start that waits for the
	// machine (V0.153, up to PressureMaxWait) shows it with the wait line.
	ud := map[string]any{"id": e.MessageID, "text": userText, "images": len(e.Images)}
	if metas != nil {
		ud["attachments"] = metas
	}
	if e.From != "" {
		ud["from"] = e.From
	}
	m.observe(threadID, core.KindMessageSent, core.MessageSentArgs{ID: e.MessageID, Log: []core.Event{{Type: "user", Data: ud}}})
	if e.From == "" && isLoop(userText) {
		m.noteLoop(threadID) // V0.wakeups-via-watch: /loop keeps the CLI's timers
	}
	s, err := m.ensure(threadID)
	if err != nil {
		// V0.33: the message stays in the log, marked lost.
		m.observe(threadID, core.KindSendFailed, core.SendFailedArgs{Message: "Could not start the agent: " + err.Error(),
			Log: []core.Event{{Type: "message.state", Data: map[string]any{"id": e.MessageID, "state": "lost"}}, {Type: "run.ended", Data: map[string]any{"failure": map[string]any{"class": "process", "message": err.Error()}}}}})
		return err
	}
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	m.mu.Lock()
	first := !s.live
	s.live = true
	noteTrigger(s, e.MessageID, trig, first)
	if s.idle != nil {
		s.idle.Stop()
	}
	s.out[e.MessageID] = "sent"
	// V0.143: a closed input reads nothing; the message waits for the
	// next process.
	hold := s.inputClosed
	if hold {
		s.held[e.MessageID] = true
	}
	holdFor := hold && s.bg > 0
	if result != nil {
		// Under the same lock as out, so no result can see the waiter
		// before it sees the message as unread.
		s.results = append(s.results, waiter{e.MessageID, result})
	}
	m.mu.Unlock()
	if e.From == FromHandover {
		// V0.45: Fyr's own words, no prefix.
	} else if e.From == FromView {
		// V0.37: a press in the live view; the user pressed it.
		e.Text = "[From the live view] " + e.Text
	} else if e.From != "" {
		// V0.22: tell the receiving agent which thread wrote this.
		label := e.From
		if ft, ok := m.host.Thread(e.From); ok && ft.Title != "" {
			label = fmt.Sprintf("%q (%s)", ft.Title, e.From)
		}
		e.Text = "[From thread " + label + "] " + e.Text
	}
	// V0.slash-first-turn: the CLI runs a command or a skill only when the
	// message's text starts with "/", so a notice in front of it turned
	// the owner's /command into plain text. Its notices stay pending: the
	// run's hooks take them, or the feed after the run.
	command := e.From == "" && strings.HasPrefix(strings.TrimSpace(e.Text), "/")
	noticeBytes := 0
	if first && !command {
		// V0.95 (1): the turn this message starts carries the pending
		// notices, before its words; they are taken, so no tool call
		// repeats them.
		if ns := m.take(s, threadID, core.AtTurn); len(ns) > 0 {
			nt := core.NoticeText(ns)
			e.Text = strings.TrimSuffix(nt+"\n\n"+e.Text, "\n\n")
			noticeBytes = len(nt)
		}
	}
	// V0.usage-overhead: Fyr's words in the message (its notices, a notice
	// turn, a handover's ask) are Fyr's in the context.
	if trig == usage.TriggerNotice || trig == usage.TriggerWake || trig == usage.TriggerHandover {
		noticeBytes = len(e.Text)
	}
	m.ovInject(s, len(e.Text), noticeBytes)
	var evs []core.Event
	if first {
		evs = append(evs, core.Event{Type: "run.started", Data: map[string]any{}})
	}
	m.observe(threadID, core.KindMessageSent, core.MessageSentArgs{ID: e.MessageID, First: first, Log: evs, Text: e.Text})
	if hold {
		if holdFor {
			m.markUnresponsive(s, "its input is closed, so it reads no message; it lives on only for its background work")
		}
		return nil
	}
	// V0.background-on-message: the owner's message during a long
	// foreground command moves it to the background. Only the owner's own
	// words (not a notice, a view press or another thread), and not a
	// /command, whose text must start with "/". The line goes only to the
	// agent: the outbox keeps the owner's text (above) for a resend.
	var move func()
	if e.From == "" && !first && !command {
		var line string
		if line, move = m.toBackground(s); line != "" {
			e.Text = line + "\n\n" + e.Text
		}
	}
	if err := m.write(s, e.MessageID, content(e)); err != nil || move == nil {
		return err
	}
	move()
	return nil
}

// delivered moves a message to a new state (V0.33) and records it once.
// It ignores a message that is no longer unread, and a "cancelled" report
// for a message whose cancel request is still out (cancelMessage records it).
func (m *Manager) delivered(s *session, id, state string) {
	m.mu.Lock()
	cur := s.out[id]
	if cur == "" || cur == state || (state == "cancelled" && s.cancelling[id]) {
		m.mu.Unlock()
		return
	}
	if state == "queued" {
		s.out[id] = state
	} else {
		delete(s.out, id)
	}
	m.mu.Unlock()
	m.observe(s.thread, core.KindMessageState, core.MessageStateArgs{ID: id, State: state})
}

// unread lists the messages the agent has not read.
func (m *Manager) unread(s *session) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids := make([]string, 0, len(s.out))
	for id := range s.out {
		ids = append(ids, id)
	}
	return ids
}

// cancelMessage withdraws a message from the CLI's queue (V0.33).
func (m *Manager) cancelMessage(threadID, id string) error {
	m.mu.Lock()
	s := m.sessions[threadID]
	waiting := s != nil && s.out[id] != ""
	if waiting {
		s.cancelling[id] = true
	}
	m.mu.Unlock()
	if !waiting {
		return fmt.Errorf("The agent has already read this message")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ok, err := s.c.CancelMessage(ctx, id)
	m.mu.Lock()
	delete(s.cancelling, id)
	m.mu.Unlock()
	if err != nil {
		return fmt.Errorf("The agent cannot withdraw this message: %v", err)
	}
	if !ok {
		return fmt.Errorf("The agent has already read this message")
	}
	m.delivered(s, id, "cancelled")
	return nil
}
