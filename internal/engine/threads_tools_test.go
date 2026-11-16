package engine

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// callAs calls a Fyr tool as the agent of thread `as`.
func callAs(t *testing.T, e *Engine, as, tool string, args map[string]any) (string, bool) {
	t.Helper()
	srv := mcp.New(func(string) (string, bool) { return as, true })
	e.Tools(srv)
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var out struct {
		Result struct {
			Content []struct{ Text string }
			IsError bool
		}
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Result.Content) == 0 {
		t.Fatalf("no result: %s", w.Body.String())
	}
	return out.Result.Content[0].Text, out.Result.IsError
}

// SPEC 0.5 V0.22: list and read other threads.
func TestThreadsListAndRead(t *testing.T) {
	h := newHarness(t, time.Minute)
	a := h.create("hello from A")
	b := h.create("lasso work")
	h.wait(a, "done", state(core.Done))
	h.wait(b, "done", state(core.Done))
	list, isErr := callAs(t, h.e, a, "threads", map[string]any{})
	if isErr || !strings.Contains(list, b) || !strings.Contains(list, "lasso work") || !strings.Contains(list, "done") {
		t.Fatalf("%v %q", isErr, list)
	}
	// V0.104: the tool shows the owner's pin.
	if r := h.cmd("thread.pin", b, nil); !r.OK {
		t.Fatalf("pin: %+v", r)
	}
	list, _ = callAs(t, h.e, a, "threads", map[string]any{})
	if !strings.Contains(list, b+" · pinned · ") || strings.Contains(list, a+" · pinned") {
		t.Fatalf("the pin is not shown: %q", list)
	}
	only, _ := callAs(t, h.e, a, "threads", map[string]any{"query": "lasso"})
	if strings.Contains(only, "hello from A") {
		t.Fatalf("query did not filter: %q", only)
	}
	text, isErr := callAs(t, h.e, a, "thread_read", map[string]any{"id": b})
	if isErr || !strings.Contains(text, "lasso work") || !strings.Contains(text, "echo: lasso work") || len(text) > 8000 {
		t.Fatalf("%v %q", isErr, text)
	}
}

// SPEC 0.5 V0.22: send to another thread starts its run and says who sent it.
func TestThreadSend(t *testing.T) {
	h := newHarness(t, time.Minute)
	a := h.create("coordinator")
	b := h.create("worker")
	h.wait(b, "done", state(core.Done))
	if _, isErr := callAs(t, h.e, a, "thread_send", map[string]any{"id": b, "text": "please rebase"}); isErr {
		t.Fatal("send failed")
	}
	th := h.wait(b, "second turn", func(t core.Thread, _ core.Attention) bool { return strings.Contains(t.Summary, "please rebase") })
	if !strings.Contains(th.Summary, "From thread") {
		t.Fatalf("the receiving agent cannot tell who sent it: %q", th.Summary)
	}
	if !hasEvent(h.events(b), "user", `"from":"`+a+`"`) {
		t.Fatal("the user event does not record the sender")
	}
	asking := h.create("ask")
	h.wait(asking, "asking", state(core.Asking))
	if text, isErr := callAs(t, h.e, a, "thread_send", map[string]any{"id": asking, "text": "Blue"}); !isErr || !strings.Contains(text, "question") {
		t.Fatalf("an agent could talk past another thread's open question: %v %q", isErr, text)
	}
}

// SPEC 0.5 V0.22: new threads record their parent; at most 5 an hour.
func TestThreadNewAndLimit(t *testing.T) {
	h := newHarness(t, time.Minute)
	a := h.create("parent")
	text, isErr := callAs(t, h.e, a, "thread_new", map[string]any{"cwd": h.dir, "text": "child work", "title": "Child"})
	if isErr {
		t.Fatalf("%q", text)
	}
	var child string
	for _, d := range h.e.Snapshot() {
		if d.Title == "Child" {
			child = d.ID
			if d.Parent != a {
				t.Fatalf("parent %q", d.Parent)
			}
		}
	}
	h.wait(child, "child done", state(core.Done))
	for i := 0; i < 4; i++ {
		if _, isErr := callAs(t, h.e, a, "thread_new", map[string]any{"cwd": h.dir, "text": "more"}); isErr {
			t.Fatalf("new %d refused early", i+2)
		}
	}
	if text, isErr := callAs(t, h.e, a, "thread_new", map[string]any{"cwd": h.dir, "text": "too many"}); !isErr || !strings.Contains(text, "limit") {
		t.Fatalf("limit not enforced: %v %q", isErr, text)
	}
}

// SPEC 0.5 V0.69: thread_rename names this thread or a thread it started,
// never another of the owner's threads.
func TestThreadRenameTool(t *testing.T) {
	h := newHarness(t, time.Minute)
	a := h.create("parent")
	other := h.create("other")
	if text, isErr := callAs(t, h.e, a, "thread_rename", map[string]any{"title": "Norban token"}); isErr {
		t.Fatalf("%q", text)
	}
	if th, _ := h.e.Thread(a); th.Title != "Norban token" {
		t.Fatalf("title %q", th.Title)
	}
	if _, isErr := callAs(t, h.e, a, "thread_new", map[string]any{"cwd": h.dir, "text": "child work", "title": "Child"}); isErr {
		t.Fatal("thread_new")
	}
	var child string
	for _, d := range h.e.Snapshot() {
		if d.Title == "Child" {
			child = d.ID
		}
	}
	if text, isErr := callAs(t, h.e, a, "thread_rename", map[string]any{"id": child, "title": "Child: tests"}); isErr {
		t.Fatalf("rename child: %q", text)
	}
	if th, _ := h.e.Thread(child); th.Title != "Child: tests" {
		t.Fatalf("child title %q", th.Title)
	}
	before, _ := h.e.Thread(other)
	if text, isErr := callAs(t, h.e, a, "thread_rename", map[string]any{"id": other, "title": "Taken"}); !isErr || !strings.Contains(text, "started") {
		t.Fatalf("renamed an unrelated thread: %v %q", isErr, text)
	}
	if th, _ := h.e.Thread(other); th.Title != before.Title {
		t.Fatalf("other title %q", th.Title)
	}
}

// #48: thread_send and thread_new send agent-source Inputs that name the
// sending thread, so the field rights apply: the agent's message does not
// mark the owner's view seen, and an agent cannot file a thread in a
// project or turn on its live view.
func TestAgentThreadToolsAreAgentInputs(t *testing.T) {
	h := newHarness(t, time.Minute)
	a := h.create("coordinator")
	b := h.create("worker")
	h.wait(b, "done", state(core.Done))
	seenOf := func(id string) time.Time {
		for _, d := range h.e.Snapshot() {
			if d.ID == id {
				return d.View.SeenAt
			}
		}
		t.Fatalf("no thread %s", id)
		return time.Time{}
	}
	seen := seenOf(b)
	if _, isErr := callAs(t, h.e, a, "thread_send", map[string]any{"id": b, "text": "please rebase"}); isErr {
		t.Fatal("send failed")
	}
	h.wait(b, "second turn", func(t core.Thread, _ core.Attention) bool { return strings.Contains(t.Summary, "please rebase") })
	if got := seenOf(b); !got.Equal(seen) {
		t.Fatalf("the agent's message marked the owner's view seen: %v → %v", seen, got)
	}
	if !hasEvent(h.events(b), "command", `"from":"`+a+`"`) {
		t.Fatal("the receiving thread's Input does not name the sending thread")
	}

	p := h.cmd("project.create", "", core.Args{"name": "Owner's"})
	pid, _ := p.Result.(map[string]any)["id"].(string)
	for _, args := range []core.Args{{"projectId": pid}, {"view": true}} {
		args["cwd"], args["text"] = h.dir, "child"
		r := h.e.submit(core.Input{Source: core.FromAgent, From: a, Kind: "thread.create", Args: args, CmdID: NewID("agent_")})
		if r.OK || r.Status != 403 {
			t.Fatalf("an agent wrote an owner-only field with %v: %+v", args, r)
		}
	}
	text, isErr := callAs(t, h.e, a, "thread_new", map[string]any{"cwd": h.dir, "text": "child work", "title": "Child"})
	if isErr {
		t.Fatalf("%q", text)
	}
	child := strings.TrimSuffix(strings.TrimPrefix(text, "Started thread "), ".")
	if th, _ := h.e.Thread(child); th.Parent != a || th.ProjectManual {
		t.Fatalf("child: parent %q manual %v", th.Parent, th.ProjectManual)
	}
}

// V0.106: the agent's settle_when on a pinned thread says it waits for the
// owner.
func TestSettleWhenOnAPinnedThreadSaysItWaits(t *testing.T) {
	h := newHarness(t, time.Minute)
	a := h.create("coordinator")
	h.wait(a, "done", state(core.Done))
	if out, _ := callAs(t, h.e, a, "settle_when", map[string]any{"condition": "done"}); strings.Contains(out, "pinned") {
		t.Fatalf("an unpinned thread got the pin note: %q", out)
	}
	if r := h.cmd("thread.pin", a, nil); !r.OK {
		t.Fatalf("pin: %+v", r)
	}
	out, isErr := callAs(t, h.e, a, "settle_when", map[string]any{"condition": "done"})
	if isErr || !strings.Contains(out, "The owner pinned this thread") {
		t.Fatalf("%v %q", isErr, out)
	}
	if th, _ := h.e.Thread(a); th.Settle.At != nil {
		t.Fatal("a pinned thread settled")
	}
}
