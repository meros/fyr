package engine

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// fakeTitler records what the title call got and answers with title or err.
type fakeTitler struct {
	mu     sync.Mutex
	title  string
	err    error
	calls  int
	thread string
	conv   string
}

func (f *fakeTitler) make(_ context.Context, thread, conv string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.thread, f.conv = thread, conv
	return f.title, f.err
}

// created makes a thread and waits until its first message is in the log.
func created(h *harness, text string) string {
	h.t.Helper()
	id := h.create(text)
	deadline := time.Now().Add(5 * time.Second)
	for !hasEvent(h.events(id), "user", text) {
		if time.Now().After(deadline) {
			h.t.Fatal("the first message never reached the log")
		}
		time.Sleep(20 * time.Millisecond)
	}
	return id
}

func (f *fakeTitler) n() int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls }

// V0.108 (1): the owner's Regenerate title names the thread from its
// conversation, also when the owner named it before.
func TestOwnerRegeneratesTheTitle(t *testing.T) {
	h := newHarness(t, time.Minute)
	ft := &fakeTitler{title: "\"Move the login to OAuth.\""}
	h.e.Titler = ft.make
	id := created(h, "fix the login page")
	if r := h.cmd("thread.rename", id, core.Args{"title": "My name"}); !r.OK {
		t.Fatalf("rename: %+v", r)
	}
	if r := h.cmd("thread.regenerateTitle", id, nil); !r.OK {
		t.Fatalf("regenerate: %+v", r)
	}
	th := h.wait(id, "the new title", func(th core.Thread, _ core.Attention) bool { return th.Title == "Move the login to OAuth" })
	if th.TitleBy != core.ByOwner {
		t.Fatalf("titleBy %q", th.TitleBy)
	}
	if ft.thread != id || !strings.Contains(ft.conv, "fix the login page") {
		t.Fatalf("the call got thread %q and %q", ft.thread, ft.conv)
	}
	if !hasEvent(h.events(id), "thread.renamed", `"regenerated":true`) {
		t.Fatal("no renamed event")
	}
}

// V0.108 (3): a failed call keeps the title and says why in the thread.
func TestFailedRegenerateKeepsTheTitle(t *testing.T) {
	h := newHarness(t, time.Minute)
	ft := &fakeTitler{err: errors.New("the provider needs a login")}
	h.e.Titler = ft.make
	id := created(h, "fix the login page")
	if r := h.cmd("thread.regenerateTitle", id, nil); !r.OK {
		t.Fatalf("regenerate: %+v", r)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !hasEvent(h.events(id), "effect.error", "could not regenerate the title: the provider needs a login") {
		if time.Now().After(deadline) {
			t.Fatal("no failure line in the thread")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if th, _ := h.e.Thread(id); th.Title != "fix the login page" {
		t.Fatalf("title %q", th.Title)
	}
}

// V0.108 (2): thread_rename {regenerate: true} names the thread from its
// conversation, and never replaces a name the owner typed (no call is made).
func TestAgentRegeneratesTheTitle(t *testing.T) {
	h := newHarness(t, time.Minute)
	ft := &fakeTitler{title: "Move the login to OAuth"}
	h.e.Titler = ft.make
	id := created(h, "fix the login page")
	text, isErr := callAs(t, h.e, id, "thread_rename", map[string]any{"regenerate": true})
	if isErr || !strings.Contains(text, "Move the login to OAuth") {
		t.Fatalf("%v %q", isErr, text)
	}
	if th, _ := h.e.Thread(id); th.Title != "Move the login to OAuth" || th.TitleBy != core.ByAgent {
		t.Fatalf("title %q by %q", th.Title, th.TitleBy)
	}

	if r := h.cmd("thread.rename", id, core.Args{"title": "My name"}); !r.OK {
		t.Fatalf("rename: %+v", r)
	}
	ft.title = "Something else"
	text, isErr = callAs(t, h.e, id, "thread_rename", map[string]any{"regenerate": true})
	if !isErr || !strings.Contains(text, "owner named") {
		t.Fatalf("regenerated over the owner's name: %v %q", isErr, text)
	}
	if ft.n() != 1 {
		t.Fatalf("a title call for a name the owner keeps: %d calls", ft.n())
	}
	if th, _ := h.e.Thread(id); th.Title != "My name" {
		t.Fatalf("title %q", th.Title)
	}

	// A failed call keeps the title and tells the agent.
	other := created(h, "second thread")
	ft.err = errors.New("timeout")
	text, isErr = callAs(t, h.e, other, "thread_rename", map[string]any{"regenerate": true})
	if !isErr || !strings.Contains(text, "The title stays") {
		t.Fatalf("%v %q", isErr, text)
	}
	if th, _ := h.e.Thread(other); th.Title != "second thread" {
		t.Fatalf("title %q", th.Title)
	}
}

// V0.108 (2): the conversation the call reads keeps the first message and
// the newest turns, at most titleChars characters.
func TestTitleConversationKeepsTheStartAndTheEnd(t *testing.T) {
	long := "first words " + strings.Repeat("middle ", 4000) + " last words"
	got := clipConversation(long)
	if len([]rune(got)) > titleChars+20 || !strings.HasPrefix(got, "first words") || !strings.HasSuffix(got, "last words") || !strings.Contains(got, "…") {
		t.Fatalf("clip: %d chars, %q…%q", len([]rune(got)), got[:20], got[len(got)-20:])
	}
	if clipConversation("short") != "short" {
		t.Fatal("a short conversation changed")
	}
}
