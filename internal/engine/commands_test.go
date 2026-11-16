package engine

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
)

func names(cs []core.Command) map[string]core.Command {
	out := map[string]core.Command{}
	for _, c := range cs {
		out[c.Name] = c
	}
	return out
}

// SPEC 0.5 V0.79 (1), (2): the commands of the CLI's initialize answer reach
// the thread (the fake answers with a built-in, a skill and /clear); the
// terminal-only ones stay out; the newest list per provider and folder
// serves a thread without a session, also after a restart.
func TestCommandsFromInitialize(t *testing.T) {
	h := newHarness(t, time.Minute)
	id := h.create("hello")
	h.wait(id, "done", state(core.Done))
	cs, from, ok := h.e.ThreadCommands(id, "fake")
	if !ok || from != "thread" {
		t.Fatalf("from %q ok %v", from, ok)
	}
	got := names(cs)
	if c := got["fake-skill"]; c.Description != "A skill of the test folder" || c.ArgumentHint != "<topic>" {
		t.Fatalf("skill %+v in %+v", c, cs)
	}
	if _, ok := got["compact"]; !ok {
		t.Fatalf("no compact in %+v", cs)
	}
	for _, n := range []string{"clear", "login", "config"} {
		if _, ok := got[n]; ok {
			t.Fatalf("terminal-only %s in %+v", n, cs)
		}
	}
	if fc := h.e.FolderCommands("fake", h.dir); len(fc) != len(cs) {
		t.Fatalf("folder list %+v", fc)
	}
	if fc := h.e.FolderCommands("other", h.dir); len(fc) != 0 {
		t.Fatalf("another provider's list %+v", fc)
	}

	// The next host, on the same database: no session yet, the folder's list.
	h.m.StopAll()
	h2 := startHarness(t, h.dir, h.db, time.Minute)
	cs2, from2, ok2 := h2.e.ThreadCommands(id, "fake")
	if !ok2 || from2 != "folder" || len(cs2) != len(cs) {
		t.Fatalf("after restart: %q %v %+v", from2, ok2, cs2)
	}
	if _, _, ok := h2.e.ThreadCommands("nope", "fake"); ok {
		t.Fatal("an unknown thread has commands")
	}
}
