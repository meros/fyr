package engine

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
)

// SPEC 0.5 V0.208: host_setup fills what the owner has not set, keeps the
// owner's fields, a cleared one too, and refuses values that are no icon or
// colour.
func TestHostSetupKeepsTheOwners(t *testing.T) {
	h := setupHarness(t)
	h.e.InstallID = "AAAA-1"
	id := h.create("hi")
	if text, isErr := callAs(t, h.e, id, "host_setup", map[string]any{"icon": "cloud", "color": "rose"}); isErr || !strings.Contains(text, "icon (cloud)") {
		t.Fatalf("host_setup: %q", text)
	}
	if m := h.e.AgentMark(); m.Icon != "cloud" || m.Color != "rose" {
		t.Fatalf("kept %+v", m)
	}
	for _, bad := range []map[string]any{{"icon": "a sentence"}, {"icon": "rocket"}, {"color": "#f00"}, {}} {
		if text, isErr := callAs(t, h.e, id, "host_setup", bad); !isErr {
			t.Fatalf("%v is taken: %q", bad, text)
		}
	}
	if text, isErr := callAs(t, h.e, id, "host_setup", map[string]any{"icon": "🍓"}); isErr {
		t.Fatalf("an emoji: %q", text)
	}
	marks := map[string]any{"AAAA-1": map[string]any{"color": "teal", "by": map[string]any{"icon": "owner"}}}
	if r := h.e.Command(api.Cmd{CommandID: "m", Action: core.KindHostMarksSet, Args: map[string]any{"marks": marks, "at": time.Now().UTC().Format(time.RFC3339Nano)}}); !r.OK {
		t.Fatalf("marks: %+v", r)
	}
	text, isErr := callAs(t, h.e, id, "host_setup", map[string]any{"icon": "box", "color": "blue"})
	if isErr || !strings.Contains(text, "Kept the icon, colour") {
		t.Fatalf("the owner's fields: %q", text)
	}
	if m := h.e.AgentMark(); m.Icon != "🍓" || m.Color != "rose" {
		t.Fatalf("the agent's mark changed: %+v", m)
	}
}

// SPEC 0.5 V0.208: Set up automatically starts a thread and records it, the
// automatic run happens once, and a run that could not start is tried again.
func TestHostSetupRun(t *testing.T) {
	h := setupHarness(t)
	h.e.InstallID = "AAAA-1"
	h.e.HostName = "desk"
	fail := true
	h.e.SystemAgentResolve = func(string) (string, string, error) {
		if fail {
			return "", "", errors.New("no provider")
		}
		return "", "claude-haiku-5-5", nil
	}
	if h.e.SetupHostOnce() {
		t.Fatal("a run without a system agent")
	}
	if s := h.e.AgentMark().Setup; s == nil || s.Error != "no provider" {
		t.Fatalf("the failure is not recorded: %+v", s)
	}
	fail = false
	if !h.e.SetupHostOnce() {
		t.Fatal("the failed run is not tried again")
	}
	s := h.e.AgentMark().Setup
	if s == nil || s.Thread == "" || s.Error != "" {
		t.Fatalf("the run: %+v", s)
	}
	th, ok := h.e.Thread(s.Thread)
	if !ok || th.Model != "claude-haiku-5-5" {
		t.Fatalf("the thread: %+v", th)
	}
	if h.e.SetupHostOnce() {
		t.Fatal("a second automatic run")
	}
	if r := h.cmd(core.KindHostSetupRun, "", core.Args{}); !r.OK || h.e.AgentMark().Setup.Thread == s.Thread {
		t.Fatalf("Run again: %+v", r)
	}
}
