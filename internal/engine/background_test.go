package engine

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
	"github.com/meros/fyr/internal/store"
)

// eventAt is the index of the first event of type typ whose data holds
// every one of parts, or -1.
func eventAt(evs []store.Event, typ string, parts ...string) int {
	for i, e := range evs {
		if e.Type != typ {
			continue
		}
		all := true
		for _, p := range parts {
			all = all && strings.Contains(string(e.Data), p)
		}
		if all {
			return i
		}
	}
	return -1
}

func backgroundHarness(t *testing.T, env ...string) *harness {
	dir := t.TempDir()
	return startHarnessWith(t, dir, filepath.Join(dir, "fyr.db"), time.Minute, nil, nil, func(c *agent.Config) {
		// Every open command counts as long: the test orders its steps by
		// events, not by a wait.
		c.BackgroundAfter = time.Nanosecond
		if len(env) > 0 {
			bin := c.Profile("").Binary
			c.Profile = func(string) provider.Profile {
				return provider.Profile{ID: "fake", Binary: bin, Env: append([]string{"FYR_FAKE_CLAUDE=1"}, env...)}
			}
		}
	})
}

// SPEC 0.5 V0.background-on-message (#19): the owner's message during a long
// foreground command moves it to the background; the agent reads the
// message, with one line naming the command, before the command ends, and
// the command's end still arrives.
func TestMessageMovesLongCommandToBackground(t *testing.T) {
	h := backgroundHarness(t)
	id := h.create("long 3000")
	h.wait(id, "the command runs", func(core.Thread, core.Attention) bool { return eventAt(h.events(id), "msg", "tu_long") >= 0 })
	steer := messageID(t, h.cmd("thread.send", id, core.Args{"text": "steer A"}))
	h.waitEvent(id, steer, "read")
	h.wait(id, "the command ended", func(core.Thread, core.Attention) bool {
		return eventAt(h.events(id), "task", `"id":"bl1"`, `"ended"`) >= 0
	})
	evs := h.events(id)
	read := eventAt(evs, "message.state", steer, `"read"`)
	ended := eventAt(evs, "task", `"id":"bl1"`, `"ended"`)
	if read < 0 || ended < 0 || read > ended {
		t.Fatalf("the message was read at event %d, the command ended at %d", read, ended)
	}
	if eventAt(evs, "msg", "took in: [Fyr] So that this message reaches you now", "`sleep 3`", "steer A") < 0 {
		t.Fatal("the agent did not get the line and the message at the moved call's boundary")
	}
	if eventAt(evs, "user", "[Fyr]") >= 0 {
		t.Fatal("the owner's message in the thread carries Fyr's line for the agent")
	}
}

// SPEC 0.5 V0.background-on-message: a CLI that cannot move the command
// (before 2.1.327) keeps today's delivery: the message waits for the
// command's end and the agent gets no line.
func TestMessageWaitsWhenCLICannotBackground(t *testing.T) {
	h := backgroundHarness(t, "FYR_FAKE_OLD_CLI=1")
	id := h.create("long 1500")
	h.wait(id, "the command runs", func(core.Thread, core.Attention) bool { return eventAt(h.events(id), "msg", "tu_long") >= 0 })
	steer := messageID(t, h.cmd("thread.send", id, core.Args{"text": "steer A"}))
	h.waitEvent(id, steer, "read")
	th := h.wait(id, "done", state(core.Done))
	if th.Summary != "took in: steer A" {
		t.Fatalf("summary %q", th.Summary)
	}
	evs := h.events(id)
	if eventAt(evs, "msg", `"slept"`) < 0 || eventAt(evs, "task", "bl1") >= 0 {
		t.Fatal("the command did not run to its end in the foreground")
	}
}
