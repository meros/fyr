package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
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
			s := strings.TrimSpace(t.Text)
			if strings.HasPrefix(s, "[Fyr]") {
				continue // V0.auto-title (5): Fyr's own notices say nothing of the work
			}
			// V0.auto-title (5): an attachment is named once, so a message that
			// is only a screenshot reads as one and the title comes from the work.
			// A pasted image is counted and also stored as a file.
			img, other := t.Images > 0, false
			for _, f := range t.Files {
				if strings.HasPrefix(f.MediaType, "image/") {
					img = true
				} else {
					other = true
				}
			}
			if img {
				s = strings.TrimSpace(s + " [attached image]")
			}
			if other {
				s = strings.TrimSpace(s + " [attached file]")
			}
			if s != "" {
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

// makeTitle runs one title call for a thread: one at a time per thread. keep gives the call the current title to keep unless the goal
// moved (V0.auto-title (2)).
func (e *Engine) makeTitle(threadID string, keep bool) (string, error) {
	if e.Titler == nil {
		return "", errors.New("this host has no title maker")
	}
	// V0.auto-title: a call waits for the one in flight (Fyr's start title
	// and the goal's, or an owner's press, can meet), for as long as a call
	// may take.
	for deadline := time.Now().Add(titleTimeout); ; time.Sleep(100 * time.Millisecond) {
		e.titleMu.Lock()
		if e.titling == nil {
			e.titling = map[string]bool{}
		}
		if !e.titling[threadID] {
			e.titling[threadID] = true
			e.titleMu.Unlock()
			break
		}
		e.titleMu.Unlock()
		if time.Now().After(deadline) {
			return "", errors.New("a title is being made already")
		}
	}
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
	out, err := e.Titler(ctx, threadID, e.titleContext(threadID, keep)+clipConversation(conv))
	if err != nil {
		return "", err
	}
	title := core.CleanTitle(out)
	if title == "" {
		return "", errors.New("the model gave no title")
	}
	return title, nil
}

// titleContext is what the title call reads before the conversation
// (V0.auto-title (2)): the current title when it is to be kept, and the
// titles of the thread's changes, which say what is being built.
func (e *Engine) titleContext(threadID string, keep bool) string {
	th, ok := e.Thread(threadID)
	if !ok {
		return ""
	}
	var b strings.Builder
	if keep && th.Title != "" {
		fmt.Fprintf(&b, "The thread's current title: %s\n", th.Title)
	}
	var chs []string
	for _, c := range th.Changes {
		if s := strings.TrimSpace(c.Title); s != "" && !slices.Contains(chs, s) {
			chs = append(chs, s)
		}
	}
	if len(chs) > 0 {
		fmt.Fprintf(&b, "Its changes: %s\n", strings.Join(chs, "; "))
	}
	if b.Len() == 0 {
		return ""
	}
	return b.String() + "\n"
}

// regenerateTitle is the owner's effect: its result lands as a made title,
// or as the reason the title stays.
func (e *Engine) regenerateTitle(threadID string) {
	title, err := e.makeTitle(threadID, false)
	made := core.TitleMade{Title: title, By: core.ByOwner}
	if err != nil {
		made = core.TitleMade{By: core.ByOwner, Error: err.Error()}
	}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsTitle), Kind: core.KindTitleMade, Thread: threadID, Args: made}); err != nil {
		log.Printf("thread %s: made title: %v", threadID, err)
	}
}

// autoTitle is Fyr's own title call (V0.auto-title): its result lands as a
// made title by Fyr; a failure is only logged.
func (e *Engine) autoTitle(threadID, when string) {
	title, err := e.makeTitle(threadID, when == core.TitleAutoGoal)
	if err != nil {
		log.Printf("thread %s: automatic title (%s): %v", threadID, when, err)
		return
	}
	made := core.TitleMade{Title: title, By: core.ByFyr, Auto: when}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsTitle), Kind: core.KindTitleMade, Thread: threadID, Args: made}); err != nil {
		log.Printf("thread %s: automatic title: %v", threadID, err)
	}
}
