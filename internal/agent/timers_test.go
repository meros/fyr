package agent

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/cli"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

func timerManager(t *testing.T, a *mcpAdapter, mcp string) *Manager {
	m := New(stubHost{cwd: t.TempDir()}, Config{
		Profile:  func(id string) provider.Profile { return provider.Profile{ID: id, CLI: "stub"} },
		Adapters: map[string]cli.Adapter{"stub": a},
		Prompt:   "-",
		MCPURL:   mcp,
		RunDir:   t.TempDir(),
	})
	t.Cleanup(m.StopAll)
	return m
}

func (a *mcpAdapter) lastArgs() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.Join(a.args, " ")
}

// V0.wakeups-via-watch (1): a process with Fyr's tools starts without the
// CLI's own timers.
func TestTimersOffWithFyrTools(t *testing.T) {
	a := &mcpAdapter{stubAdapter: &stubAdapter{}}
	m := timerManager(t, a, "http://127.0.0.1:1/mcp")
	if _, err := m.ensure("t_1"); err != nil {
		t.Fatal(err)
	}
	if got := a.lastArgs(); !strings.Contains(got, "--disallowedTools CronCreate,CronDelete,ScheduleWakeup") {
		t.Fatalf("argv %s", got)
	}
	if m.sessions["t_1"].timers {
		t.Fatal("the session says it has the timers")
	}
}

// Without Fyr's tools there is no watch to snooze with: the timers stay.
func TestTimersOnWithoutFyrTools(t *testing.T) {
	a := &mcpAdapter{stubAdapter: &stubAdapter{}}
	m := timerManager(t, a, "")
	if _, err := m.ensure("t_1"); err != nil {
		t.Fatal(err)
	}
	if got := a.lastArgs(); strings.Contains(got, "--disallowedTools") {
		t.Fatalf("argv %s", got)
	}
}

// V0.wakeups-via-watch (1): the owner's /loop starts a process that keeps
// the timers; an idle process without them ends first.
func TestLoopKeepsTimers(t *testing.T) {
	a := &mcpAdapter{stubAdapter: &stubAdapter{}}
	m := timerManager(t, a, "http://127.0.0.1:1/mcp")
	if _, err := m.ensure("t_1"); err != nil {
		t.Fatal(err)
	}
	before := m.sessions["t_1"]
	if err := m.send("t_1", core.Effect{Kind: core.EffectSend, Text: "/loop 5m /babysit-prs"}, nil); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	after := m.sessions["t_1"]
	m.mu.Unlock()
	if after == before || !after.timers {
		t.Fatal("the /loop did not start a process with the timers")
	}
	if got := a.lastArgs(); strings.Contains(got, "--disallowedTools") {
		t.Fatalf("argv %s", got)
	}
}

func TestIsLoop(t *testing.T) {
	for in, want := range map[string]bool{"/loop": true, " /loop 5m /x": true, "/looper": false, "run /loop": false, "": false} {
		if isLoop(in) != want {
			t.Errorf("%q", in)
		}
	}
}

// V0.wakeups-via-watch (2): the prompt names the watch as the only snooze.
func TestFyrPromptSaysWakeUpIsAWatch(t *testing.T) {
	for _, want := range []string{"A wake-up, a reminder or \"check back at T\" is mcp__fyr__watch", `kind: "at"`, "the prompt to run then", "CronCreate, CronDelete and ScheduleWakeup are off"} {
		if !strings.Contains(FyrPrompt, want) {
			t.Errorf("the Fyr prompt lacks %q", want)
		}
	}
}
