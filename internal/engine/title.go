package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// Regenerate title (V0.108): Fyr asks a small model for a thread title from
// the thread's conversation so far. The owner's press runs it as an effect
// and lands its result as core.KindTitleMade; the agent's thread_rename
// {regenerate: true} runs it in the tool call.

// Titler makes a title from a thread's conversation (thread id, the text).
// *agent.Manager's Title implements it.
type Titler func(ctx context.Context, threadID, conversation string) (string, error)

const (
	// titleChars is the most of the conversation the call reads: the first
	// titleHead characters (what the thread was started for) and the newest
	// rest (what it is about now).
	titleChars = 12000
	titleHead  = 2000
	// titleTimeout bounds one title call.
	titleTimeout = 90 * time.Second
)

// clipConversation keeps the start and the end of a long conversation.
func clipConversation(s string) string {
	r := []rune(s)
	if len(r) <= titleChars {
		return s
	}
	return string(r[:titleHead]) + "\n…\n" + string(r[len(r)-(titleChars-titleHead):])
}

// titleText is the conversation the title call reads: the owner's words
// and the agent's text, without tool calls.
func titleText(evs []store.Event) string {
	var b strings.Builder
	for _, t := range readTurns(evs) {
		switch t.Kind {
		case turnUser:
			if s := strings.TrimSpace(t.Text); s != "" {
				fmt.Fprintf(&b, "%s: %s\n\n", speaker("Owner", t.From), s)
			}
		case turnAgent:
			for _, p := range t.Parts {
				if s := strings.TrimSpace(p.Text); p.Tool == "" && s != "" {
					fmt.Fprintf(&b, "Agent: %s\n\n", s)
				}
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// makeTitle runs one title call for a thread: at most one at a time per
// thread.
func (e *Engine) makeTitle(threadID string) (string, error) {
	if e.Titler == nil {
		return "", errors.New("this host has no title maker")
	}
	e.titleMu.Lock()
	if e.titling == nil {
		e.titling = map[string]bool{}
	}
	if e.titling[threadID] {
		e.titleMu.Unlock()
		return "", errors.New("a title is being made already")
	}
	e.titling[threadID] = true
	e.titleMu.Unlock()
	defer func() {
		e.titleMu.Lock()
		delete(e.titling, threadID)
		e.titleMu.Unlock()
	}()

	evs, err := e.st.EventsFor(threadID, 0, 1<<30, false)
	if err != nil {
		return "", err
	}
	conv := titleText(evs)
	if conv == "" {
		return "", errors.New("the thread has no conversation yet")
	}
	ctx, cancel := context.WithTimeout(context.Background(), titleTimeout)
	defer cancel()
	out, err := e.Titler(ctx, threadID, clipConversation(conv))
	if err != nil {
		return "", err
	}
	title := core.CleanTitle(out)
	if title == "" {
		return "", errors.New("the model gave no title")
	}
	return title, nil
}

// regenerateTitle is the owner's effect: its result lands as a made title,
// or as the reason the title stays.
func (e *Engine) regenerateTitle(threadID string) {
	title, err := e.makeTitle(threadID)
	made := core.TitleMade{Title: title, By: core.ByOwner}
	if err != nil {
		made = core.TitleMade{By: core.ByOwner, Error: err.Error()}
	}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsTitle), Kind: core.KindTitleMade, Thread: threadID, Args: made}); err != nil {
		log.Printf("thread %s: made title: %v", threadID, err)
	}
}
