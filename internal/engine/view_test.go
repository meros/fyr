package engine

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

func viewEvents(h *harness, id, typ string) []map[string]any {
	var out []map[string]any
	for _, ev := range h.events(id) {
		if ev.Type == typ {
			var m map[string]any
			json.Unmarshal(ev.Data, &m)
			out = append(out, m)
		}
	}
	return out
}

func toolNames(t *testing.T, e *Engine, as string) []string {
	srv := mcp.New(func(string) (string, bool) { return as, true })
	e.Tools(srv)
	req := httptest.NewRequest("POST", "/mcp", bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)))
	req.Header.Set("Authorization", "Bearer x")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	var out struct {
		Result struct{ Tools []struct{ Name string } }
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	var names []string
	for _, x := range out.Result.Tools {
		names = append(names, x.Name)
	}
	return names
}

// SPEC 0.5 V0.37: the view tools exist only for a thread with the flag.
func TestViewToolsOnlyWithFlag(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.e.Attach = h.att
	plain := h.create("hello")
	h.wait(plain, "done", state(core.Done))
	if names := strings.Join(toolNames(t, h.e, plain), " "); strings.Contains(names, "view_") {
		t.Fatalf("plain thread lists %s", names)
	}
	if out, isErr := callAs(t, h.e, plain, "view_state", map[string]any{"patch": map[string]any{}}); !isErr {
		t.Fatalf("plain thread called view_state: %s", out)
	}
	if evs := viewEvents(h, plain, "view.published"); len(evs) != 0 {
		t.Fatalf("plain thread got a view: %v", evs)
	}
	if !h.m.Live(plain) {
		t.Fatal("no process after the run")
	}
	r := h.cmd("thread.setView", plain, core.Args{"on": true})
	if !r.OK {
		t.Fatalf("setView: %+v", r)
	}
	// The idle process ends, so the next one starts with the view's prompt and tools.
	deadline := time.Now().Add(5 * time.Second)
	for h.m.Live(plain) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if h.m.Live(plain) {
		t.Fatal("setView kept the old process")
	}
	if names := strings.Join(toolNames(t, h.e, plain), " "); !strings.Contains(names, "view_publish") || !strings.Contains(names, "view_unchanged") {
		t.Fatalf("flagged thread lists %s", names)
	}
	if out, isErr := callAs(t, h.e, plain, "view_state", map[string]any{"patch": map[string]any{}}); !isErr || !strings.Contains(out, "view_publish first") {
		t.Fatalf("state before any page: %s", out)
	}
	if r := h.cmd("thread.setView", plain, core.Args{}); r.Status != 400 {
		t.Fatalf("setView without on: %+v", r)
	}
}

// SPEC 0.5 V0.37: publish stores a page once by hash, view_state merges a
// patch (null deletes, arrays replace), and both keep the size caps.
func TestViewTools(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.e.Attach = h.att
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "view first", "view": true})
	id := r.ThreadID
	h.wait(id, "done", state(core.Done))

	// The first run ended without a view, so the Stop hook had the fake publish one.
	if out, isErr := callAs(t, h.e, id, "view_state", map[string]any{"patch": map[string]any{"a": 1}}); isErr {
		t.Fatalf("state after the forced publish: %s", out)
	}
	html := `<ul id="steps"></ul><button data-send="Ship it">Ship</button>`
	pub := func(state map[string]any) (string, bool) {
		return callAs(t, h.e, id, "view_publish", map[string]any{"html": html, "state": state})
	}
	if out, isErr := pub(map[string]any{"headline": "Building", "steps": []any{"a", "b"}, "note": "x"}); isErr {
		t.Fatal(out)
	}
	th, _ := h.e.Thread(id)
	page1 := th.Live.PageID
	if _, isErr := pub(map[string]any{"headline": "Again"}); isErr {
		t.Fatal("republish failed")
	}
	th, _ = h.e.Thread(id)
	if th.Live.PageID != page1 {
		t.Fatalf("same HTML stored twice: %s, %s", page1, th.Live.PageID)
	}
	m, err := h.att.Get(page1)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(m.Path)
	if !strings.Contains(string(b), "ui/notifications/size-changed") || !strings.Contains(string(b), "--accent") || !strings.Contains(string(b), html) {
		t.Fatalf("page lacks the bridge, the theme or the fragment")
	}

	if out, isErr := callAs(t, h.e, id, "view_state", map[string]any{"patch": map[string]any{"steps": []any{"c"}, "headline": "Testing", "pr": map[string]any{"n": 4}}}); isErr {
		t.Fatal(out)
	}
	if out, isErr := callAs(t, h.e, id, "view_state", map[string]any{"patch": map[string]any{"pr": nil}}); isErr {
		t.Fatal(out)
	}
	th, _ = h.e.Thread(id)
	want := `{"headline":"Testing","steps":["c"]}`
	if got, _ := json.Marshal(th.Live.State); string(got) != want || th.Live.Headline != "Testing" {
		t.Fatalf("state %s headline %q, want %s", got, th.Live.Headline, want)
	}
	// The event carries the full state, so a fold is the last event.
	evs := viewEvents(h, id, "view.state")
	if got, _ := json.Marshal(evs[len(evs)-1]["state"]); string(got) != want {
		t.Fatalf("last view.state %s", got)
	}

	big := strings.Repeat("x", core.ViewStateMax)
	if out, isErr := callAs(t, h.e, id, "view_state", map[string]any{"patch": map[string]any{"log": big}}); !isErr || !strings.Contains(out, "at most") {
		t.Fatalf("oversized state: %s", out)
	}
	if out, isErr := callAs(t, h.e, id, "view_publish", map[string]any{"html": strings.Repeat("x", 513<<10)}); !isErr || !strings.Contains(out, "512 kB") {
		t.Fatalf("oversized page: %s", out)
	}
	if out, isErr := callAs(t, h.e, id, "view_publish", map[string]any{"html": "<p>", "state": "nope"}); !isErr {
		t.Fatalf("state not an object: %s", out)
	}
	if out, isErr := callAs(t, h.e, id, "view_state", map[string]any{"patch": []any{1}}); !isErr {
		t.Fatalf("patch not an object: %s", out)
	}
	th, _ = h.e.Thread(id)
	if th.Live.Headline != "Testing" {
		t.Fatalf("a failed call changed the view: %+v", th.Live)
	}

	p := filepath.Join(h.dir, "v.html")
	os.WriteFile(p, []byte("<!doctype html><html><head><title>t</title></head><body>file</body></html>"), 0o644)
	if out, isErr := callAs(t, h.e, id, "view_publish", map[string]any{"path": p}); isErr {
		t.Fatal(out)
	}
	th, _ = h.e.Thread(id)
	m, _ = h.att.Get(th.Live.PageID)
	b, _ = os.ReadFile(m.Path)
	if !strings.HasPrefix(string(b), "<!doctype html><html><head><script>") {
		t.Fatalf("bridge not first in head: %.80s", b)
	}

	// The DTO carries the view (API: thread.live).
	var dto DTO
	for _, d := range h.e.Snapshot() {
		if d.ID == id {
			dto = d
		}
	}
	raw, _ := json.Marshal(dto)
	if !strings.Contains(string(raw), `"live":{"pageId":"`+th.Live.PageID) || !strings.Contains(string(raw), `"liveView":true`) {
		t.Fatalf("DTO: %s", raw)
	}
}

// SPEC 0.5 V0.37: a run that ends without touching the view is blocked
// once; the forced turn updates it, the synthetic message never shows, and
// the run's own last words stay the summary.
func TestViewStopHookBlocksOnce(t *testing.T) {
	h := newHarness(t, time.Minute)
	h.e.Attach = h.att
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "text": "hello", "view": true})
	id := r.ThreadID
	th := h.wait(id, "done", state(core.Done))
	if th.Live == nil || th.Live.Headline != "after stop hook" {
		t.Fatalf("view after the forced turn: %+v", th.Live)
	}
	if th.Summary != "echo: hello" {
		t.Fatalf("summary %q, want the run's own words", th.Summary)
	}
	if n := len(viewEvents(h, id, "view.published")); n != 1 {
		t.Fatalf("%d publishes, want 1 (blocked once)", n)
	}
	for _, ev := range h.events(id) {
		if strings.Contains(string(ev.Data), "Stop hook feedback") {
			t.Fatalf("synthetic message in the log: %s %s", ev.Type, ev.Data)
		}
	}
	if n := len(viewEvents(h, id, "run.ended")); n != 1 {
		t.Fatalf("%d run.ended, want 1", n)
	}
	// The forced turn is marked in stream order, after the run's own last message,
	// so the client can show its text as one quiet line (V0.37, owner's choice).
	var forced []int64
	var lastOwn int64
	for _, ev := range h.events(id) {
		if ev.Type == "view.forced" {
			forced = append(forced, ev.Seq)
		}
		if ev.Type == "msg" && strings.Contains(string(ev.Data), "echo: hello") {
			lastOwn = ev.Seq
		}
	}
	if len(forced) != 1 {
		t.Fatalf("%d view.forced, want 1", len(forced))
	}
	if lastOwn == 0 || forced[0] < lastOwn {
		t.Fatalf("view.forced at %d before the run's own answer at %d", forced[0], lastOwn)
	}

	// A run that updates the view itself is not blocked.
	h.cmd("thread.send", id, core.Args{"text": "view Ready"})
	h.wait(id, "next done", func(t core.Thread, a core.Attention) bool { return !t.Running && len(t.Outbox) == 0 && t.Live.Headline == "Ready" })
	time.Sleep(100 * time.Millisecond)
	if n := len(viewEvents(h, id, "view.published")); n != 2 {
		t.Fatalf("%d publishes, want 2", n)
	}

	// Without the flag the hook never blocks.
	plain := h.create("hi")
	h.wait(plain, "done", state(core.Done))
	if p, _ := h.e.Thread(plain); p.Live != nil {
		t.Fatalf("plain thread got a view: %+v", p.Live)
	}
}

// SPEC 0.5 V0.37: a press in the view is a message from:"view"; the agent
// reads it labelled, the log keeps the plain text.
func TestViewPressSendsMessage(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	r := h.cmd("thread.send", id, core.Args{"text": "Ship it", "from": "view"})
	if !r.OK {
		t.Fatalf("send: %+v", r)
	}
	th := h.wait(id, "echo", func(t core.Thread, _ core.Attention) bool { return strings.Contains(t.Summary, "Ship it") })
	if th.Summary != "echo: [From the live view] Ship it" {
		t.Fatalf("agent read %q", th.Summary)
	}
	users := viewEvents(h, id, "user")
	last := users[len(users)-1]
	if last["from"] != "view" || last["text"] != "Ship it" {
		t.Fatalf("user event %v", last)
	}
}

// SPEC 0.5 V0.37: the client's mock serves the same bridge the host puts in
// every view page, so the layout test exercises the real one.
func TestMockBridgeMatches(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "web", "src", "mock.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "export const VIEW_BRIDGE = `"+viewBridge+"`;") {
		t.Fatal("web/src/mock.ts VIEW_BRIDGE differs from viewBridge; copy it over")
	}
}

// SPEC 0.5 V0.37: every view gives a pressed button immediate feedback, whatever the
// agent wrote: busy at once, then Sent or Not sent from the host's reply.
func TestViewBridgeGivesPressFeedback(t *testing.T) {
	page := viewPage("<button data-send=\"Go\">Go</button>")
	for _, want := range []string{`[data-fyr=busy]`, `Sent`, `Not sent`, `setAttribute("data-fyr","busy")`, `wait[d.id]`} {
		if !strings.Contains(page, want) {
			t.Fatalf("the bridge lacks %q", want)
		}
	}
}
