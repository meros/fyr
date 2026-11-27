package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
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
	guardDeps(t, h.e)
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
	guardDeps(t, h.e)
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
	guardDeps(t, h.e)
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
	ft.mu.Lock()
	ft.title = "Something else"
	ft.mu.Unlock()
	calls := ft.n() // Fyr's own start title (V0.auto-title) may have run too
	text, isErr = callAs(t, h.e, id, "thread_rename", map[string]any{"regenerate": true})
	if !isErr || !strings.Contains(text, "owner named") {
		t.Fatalf("regenerated over the owner's name: %v %q", isErr, text)
	}
	if ft.n() != calls {
		t.Fatalf("a title call for a name the owner keeps: %d calls", ft.n())
	}
	if th, _ := h.e.Thread(id); th.Title != "My name" {
		t.Fatalf("title %q", th.Title)
	}

	// A failed call keeps the title and tells the agent.
	ft.mu.Lock()
	ft.err = errors.New("timeout")
	ft.mu.Unlock()
	other := created(h, "second thread")
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

// V0.auto-title (1), (3): when the first run of a thread named from a vague
// first message ends, Fyr titles it from the conversation by itself, and
// the title stays Fyr's.
func TestAutoTitleAtTheFirstRunsEnd(t *testing.T) {
	h := newHarness(t, time.Minute)
	ft := &fakeTitler{title: "Toast unreadable in dark mode"}
	h.e.Titler = ft.make
	guardDeps(t, h.e)
	id := created(h, "Not nice")
	// The harness's agent answers and ends its first run by itself.
	th := h.wait(id, "the made title", func(th core.Thread, _ core.Attention) bool { return th.Title == "Toast unreadable in dark mode" })
	if th.TitleBy != "" || th.TitleAuto != core.TitleAutoStart || ft.n() != 1 {
		t.Fatalf("titleBy %q auto %q calls %d", th.TitleBy, th.TitleAuto, ft.n())
	}
	if !hasEvent(h.events(id), "thread.renamed", `"by":"fyr"`) {
		t.Fatal("no renamed event")
	}
}

// V0.auto-title (5): the title call reads an attachment as one, and leaves
// out Fyr's own notices.
func TestTitleTextNamesImages(t *testing.T) {
	ev := func(typ, data string) store.Event { return store.Event{Type: typ, Data: []byte(data)} }
	got := titleText([]store.Event{
		ev("user", `{"text":"","images":1,"attachments":[{"id":"a","name":"s.png","mediaType":"image/png"}]}`),
		ev("user", `{"text":"[Fyr] Handover: the owner handed this thread over"}`),
		ev("user", `{"text":"and this","attachments":[{"id":"b","name":"r.pdf","mediaType":"application/pdf"}]}`),
	})
	want := "Owner: [attached image]\n\nOwner: and this [attached file]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// V0.auto-title (2): when the thread's first change appears, the goal call
// reads the current title, to keep unless the goal moved, and the change's.
func TestAutoTitleAtTheGoalKeepsOrMoves(t *testing.T) {
	h := newHarness(t, time.Minute)
	ft := &fakeTitler{title: "Why the listing was not updated"}
	h.e.Titler = ft.make
	guardDeps(t, h.e)
	id := created(h, "Why was it not updated last night?")
	// The harness's agent ends the first run: the start title lands first.
	h.wait(id, "the start title", func(th core.Thread, _ core.Attention) bool { return th.Title == "Why the listing was not updated" })
	ft.mu.Lock()
	ft.title = "Remove sold listings the same night"
	ft.mu.Unlock()
	if _, err := h.e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindChangeOpen, Thread: id,
		Args: core.ChangeOpenArgs{Open: core.ChangeOpen{Key: "x", Kind: core.KindCode, Title: "Remove a confirmed-gone listing the same night", Branch: "x"}}}); err != nil {
		t.Fatal(err)
	}
	th := h.wait(id, "the goal title", func(th core.Thread, _ core.Attention) bool { return th.Title == "Remove sold listings the same night" })
	if th.TitleAuto != core.TitleAutoDone {
		t.Fatalf("auto %q", th.TitleAuto)
	}
	ft.mu.Lock()
	conv := ft.conv
	ft.mu.Unlock()
	if !strings.HasPrefix(conv, "The thread's current title: Why the listing was not updated\nIts changes: Remove a confirmed-gone listing the same night\n\n") ||
		!strings.Contains(conv, "Owner: Why was it not updated last night?") {
		t.Fatalf("the call read %q", conv)
	}
}

// V0.auto-title-timely (2): the goal call does not keep a title that is
// still the first message's line: no model made it.
func TestGoalTitleDoesNotKeepThePlaceholder(t *testing.T) {
	h := newHarness(t, time.Minute)
	guardDeps(t, h.e)
	id := created(h, "order snoozed is earliest deadline first")
	evs, err := h.e.st.EventsFor(id, 0, 1<<30, false)
	if err != nil {
		t.Fatal(err)
	}
	ph := firstLine(evs)
	if th, _ := h.e.Thread(id); th.Title != ph {
		t.Fatalf("title %q, placeholder %q", th.Title, ph)
	}
	if got := h.e.titleContext(id, true, ph); strings.Contains(got, "current title") {
		t.Fatalf("the goal call keeps the placeholder: %q", got)
	}
	if got := h.e.titleContext(id, true, "something else"); !strings.Contains(got, "The thread's current title: order snoozed is earliest deadline first") {
		t.Fatalf("a made title is not kept: %q", got)
	}
}

// V0.press-thread-title (1): the press notice that starts a thread is read
// as its brief, without Fyr's frame; later [Fyr] notices, a press on the
// thread among them, are still left out.
func TestTitleTextReadsTheStartingPress(t *testing.T) {
	ev := func(typ, text string) store.Event {
		b, _ := json.Marshal(map[string]any{"text": text})
		return store.Event{Type: typ, Data: b}
	}
	got := titleText([]store.Event{
		ev("user", core.PressNotice("Start #12 also", "this thread", "Fix the header that overlaps the menu.")),
		ev("user", core.PressNotice("Retry", "this thread", "Run the tests again.")),
		ev("user", "[Fyr] CI failed on #13."),
	})
	want := "Owner: Start #12 also\nFix the header that overlaps the menu."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
