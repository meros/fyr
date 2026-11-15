package engine

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/attach"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/store"
)

// bare is an engine without agents, on a clock the test moves.
func bare(t *testing.T) (*Engine, *store.Store, *time.Time) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "fyr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	e := New(st)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)
	e.Now = func() time.Time { return now }
	return e, st, &now
}

func bareThread(t *testing.T, e *Engine) string {
	t.Helper()
	r := e.Command(Cmd{CommandID: NewID("c"), Action: "thread.create", Args: core.Args{"cwd": t.TempDir(), "text": "hi"}})
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	return r.ThreadID
}

// frames drains what the hub sent so far, by event name.
func frames(ch chan Frame) map[string][]Frame {
	out := map[string][]Frame{}
	for {
		select {
		case f := <-ch:
			out[f.Event] = append(out[f.Event], f)
		default:
			return out
		}
	}
}

// #36: an event alone does not move the thread's UpdatedAt, so it does not
// jump to the top of the list.
func TestObserveEventOnlyKeepsUpdatedAt(t *testing.T) {
	e, _, now := bare(t)
	id := bareThread(t, e)
	before, _ := e.Thread(id)
	*now = now.Add(time.Hour)
	ch := e.Hub.Subscribe()
	coretest.Patch(e, id, func(*core.Thread, time.Time) []core.Event {
		return []core.Event{{Type: "effect.result", Data: map[string]any{"ok": true}}}
	})
	after, _ := e.Thread(id)
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("UpdatedAt moved: %v → %v", before.UpdatedAt, after.UpdatedAt)
	}
	got := frames(ch)
	if len(got["event"]) != 1 || len(got["thread"]) != 0 {
		t.Fatalf("frames: %d events, %d threads", len(got["event"]), len(got["thread"]))
	}
	coretest.Patch(e, id, func(t *core.Thread, _ time.Time) []core.Event { t.Summary = "moved"; return nil })
	if th, _ := e.Thread(id); !th.UpdatedAt.Equal(*now) {
		t.Fatalf("a change did not move UpdatedAt: %v", th.UpdatedAt)
	}
}

// #36: a change tool is one observation: one write of the thread with its
// change event, one thread broadcast.
func TestChangeToolIsOneObservation(t *testing.T) {
	e, st, _ := bare(t)
	id := bareThread(t, e)
	ch := e.Hub.Subscribe()
	if out, isErr := callAs(t, e, id, "change_open", map[string]any{"key": "doc:x", "kind": "doc", "title": "X"}); isErr {
		t.Fatalf("open: %s", out)
	}
	got := frames(ch)
	if len(got["thread"]) != 1 || len(got["event"]) != 1 {
		t.Fatalf("frames: %d threads, %d events", len(got["thread"]), len(got["event"]))
	}
	evs, _ := st.EventsFor(id, 0, 100, false)
	if last := evs[len(evs)-1]; last.Type != "change" || !strings.Contains(string(last.Data), `"title":"X"`) {
		t.Fatalf("last event %s %s", last.Type, last.Data)
	}
	// A failing tool writes nothing.
	n := len(evs)
	if _, isErr := callAs(t, e, id, "change_status", map[string]any{"change": "nope", "phase": "live"}); !isErr {
		t.Fatal("status of a missing change passed")
	}
	if evs, _ := st.EventsFor(id, 0, 100, false); len(evs) != n {
		t.Fatalf("a failed tool wrote %d events", len(evs)-n)
	}
}

// #36: a thread that had only background tasks running when the host
// stopped gets its tasks stopped, and no run.ended: there was no run.
func TestLoadTasksOnlyNoRunEnded(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "fyr.db")
	st, err := store.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	th := &core.Thread{ID: "t_tasks", Owner: core.Owner, Cwd: dir, Profile: "claude",
		Conversation: core.Conversation{Tasks: []core.Task{{ID: "k1", Status: core.TaskRunning}}}}
	if _, err := st.Write(func(tx *store.Tx) error { return tx.PutThread(th) }); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, _ = store.Open(db)
	defer st.Close()
	e := New(st)
	if err := e.Load(); err != nil {
		t.Fatal(err)
	}
	got, _ := e.Thread("t_tasks")
	if got.Tasks[0].Status != core.TaskStopped || got.Failure != nil {
		t.Fatalf("thread after Load: %+v %+v", got.Tasks, got.Failure)
	}
	evs, _ := st.EventsFor("t_tasks", 0, 100, false)
	for _, ev := range evs {
		if ev.Type == "run.ended" {
			t.Fatalf("Load appended run.ended: %s", ev.Data)
		}
	}
}

// #36: the receipt check and the receipt are one step: the same command id
// sent at once from many clients makes one project.
func TestReplayUnderLock(t *testing.T) {
	e, _, _ := bare(t)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r := e.Command(Cmd{CommandID: "same", Action: "project.create", Args: core.Args{"name": "Lasso"}}); !r.OK {
				t.Errorf("create: %+v", r)
			}
		}()
	}
	wg.Wait()
	if n := len(e.Projects()); n != 1 {
		t.Fatalf("%d projects from one command id", n)
	}
	r := e.Command(Cmd{CommandID: "same", Action: "project.create", Args: core.Args{"name": "Other"}})
	b, _ := json.Marshal(r)
	if !r.OK || r.Status != 200 || !strings.Contains(string(b), `"id":"p_`) {
		t.Fatalf("replay: %s", b)
	}
}

// #36: one sliding-window limiter serves the screenshot and the thread
// limits.
func TestWindowLimiter(t *testing.T) {
	w := window{max: 2, span: time.Minute}
	t0 := time.Now()
	if !w.allow("a", t0) || !w.allow("a", t0.Add(time.Second)) || w.allow("a", t0.Add(2*time.Second)) {
		t.Fatal("the third call in the window passed")
	}
	if !w.allow("b", t0) {
		t.Fatal("keys share a window")
	}
	if !w.allow("a", t0.Add(time.Minute+time.Second)) {
		t.Fatal("the window did not slide")
	}
}

// #36: storeOnce saves a content key once, and again when its file is gone.
func TestStoreOnce(t *testing.T) {
	e, _, _ := bare(t)
	e.Attach = &attach.Store{Dir: t.TempDir()}
	saves := 0
	save := func() (string, error) {
		saves++
		m, err := e.Attach.Save("a.html", "text/html", strings.NewReader("<p>x</p>"))
		return m.ID, err
	}
	a, _ := e.storeOnce("k", save)
	b, _ := e.storeOnce("k", save)
	if a != b || saves != 1 {
		t.Fatalf("ids %s %s, %d saves", a, b, saves)
	}
	e.st.SetKV("k", "gone")
	if c, _ := e.storeOnce("k", save); c == "gone" || saves != 2 {
		t.Fatalf("a missing file was not saved again: %s, %d saves", c, saves)
	}
}

// #36: thread_read and the handover conversation read the log with one
// parser, so both know every sender: the View tab and a handover included.
func TestOneTurnParser(t *testing.T) {
	e, st, _ := bare(t)
	id := bareThread(t, e)
	e.record(id,
		core.Event{Type: "user", Data: map[string]any{"text": "typed on the view", "from": agent.FromViewTyped}},
		core.Event{Type: "user", Data: map[string]any{"text": "carry on", "from": agent.FromHandover}},
		core.Event{Type: "msg", Data: map[string]any{"type": "assistant", "parent_tool_use_id": nil, "message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "Done åäö"}, map[string]any{"type": "tool_use", "name": "Bash", "input": map[string]any{"command": "ls"}}}}}})
	evs, _ := st.EventsFor(id, 0, 100, false)
	text := threadText(evs)
	conv, _ := e.Conversation(id)
	for _, want := range []string{"User (View tab): typed on the view", "Fyr: carry on", "Agent: Done åäö", "(1 tool calls)"} {
		if !strings.Contains(text, want) {
			t.Fatalf("thread_read lacks %q:\n%s", want, text)
		}
	}
	for _, want := range []string{"## Owner (View tab)", "## Fyr", "Done åäö\n- Bash `ls`"} {
		if !strings.Contains(conv, want) {
			t.Fatalf("conversation lacks %q:\n%s", want, conv)
		}
	}
}
