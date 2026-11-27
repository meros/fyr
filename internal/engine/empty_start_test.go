package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

// V0.empty-thread-start: thread.create with no message makes the thread
// idle (no run, no agent session); its first send starts the agent and
// names it.
func TestEmptyThreadStart(t *testing.T) {
	h := newHarness(t, time.Minute)
	r := h.cmd("thread.create", "", core.Args{"cwd": h.dir})
	if !r.OK {
		t.Fatalf("create: %+v", r)
	}
	th, _ := h.e.Thread(r.ThreadID)
	if th.Running || th.Starting != nil || th.SessionID != "" || th.Title != core.EmptyTitle || th.TitleAuto != core.TitleAutoEmpty {
		t.Fatalf("empty thread: %+v", th)
	}
	if hasEvent(h.events(r.ThreadID), "user", "") {
		t.Fatal("an empty start wrote a message")
	}
	if bad := h.cmd("thread.send", r.ThreadID, core.Args{"text": "  "}); bad.OK {
		t.Fatalf("an empty send went through: %+v", bad)
	}
	if s := h.cmd("thread.send", r.ThreadID, core.Args{"text": "hello there"}); !s.OK {
		t.Fatalf("send: %+v", s)
	}
	th = h.wait(r.ThreadID, "done", state(core.Done))
	if th.SessionID == "" || th.Summary != "echo: hello there" || th.Title != "hello there" || th.TitleAuto == core.TitleAutoEmpty {
		t.Fatalf("after the first send: %+v", th)
	}

	// No folder: it starts in a fresh scratch folder, as with a message.
	s := h.cmd("thread.create", "", core.Args{})
	if !s.OK {
		t.Fatalf("scratch: %+v", s)
	}
	if st, _ := h.e.Thread(s.ThreadID); st.Cwd == "" {
		t.Fatalf("no scratch folder: %+v", st)
	}

	// A title given at the start stays when the first message comes.
	n := h.cmd("thread.create", "", core.Args{"cwd": h.dir, "title": "Kept"})
	h.cmd("thread.send", n.ThreadID, core.Args{"text": "other words"})
	if nt := h.wait(n.ThreadID, "done", state(core.Done)); nt.Title != "Kept" {
		t.Fatalf("named thread renamed: %q", nt.Title)
	}
}
