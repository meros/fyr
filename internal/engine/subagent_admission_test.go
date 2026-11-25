// V0.subagent-admission: the admission of a heavy job reaches the actor whose command was refused. A running subagent
// hears it at its next tool call and the thread's agent is not woken; once that subagent has ended, the thread's agent
// is told, with the subagent named.
package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/guard/resource"
)

// subEngine is heavyEngine with one slot, a held by thread a, and a subagent
// of thread b queued behind it; live says which subagents run.
func subEngine(t *testing.T) (e *Engine, now *time.Time, a, b string, live map[string]bool) {
	t.Helper()
	e, now, a, b = heavyEngine(t, 1)
	live = map[string]bool{"a1": true}
	e.SubagentLive = func(thread, agent string) bool { return thread == b && live[agent] }
	guardDeps(t, e)
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1"}); r != "" {
		t.Fatalf("not admitted: %q", r)
	}
	if r := e.HeavyGate(HeavyCall{Thread: b, Class: "go test ./...", ToolUse: "tu2", Subagent: true, Agent: "a1", AgentType: "worker"}); !strings.Contains(r, "queued") {
		t.Fatalf("not queued: %q", r)
	}
	return e, now, a, b, live
}

func admissionNotices(e *Engine, id string) []core.Notice {
	th, _ := e.Thread(id)
	var out []core.Notice
	for _, n := range th.Feed.Queue {
		if strings.Contains(n.What, "admitted now") || strings.Contains(n.What, "Memory has recovered") {
			out = append(out, n)
		}
	}
	return out
}

func TestSubagentAdmissionARunningSubagentHearsItAndTheParentIsNotWoken(t *testing.T) {
	e, _, a, b, _ := subEngine(t)
	e.HeavyDone(a, "tu1") // the slot goes to b
	if !holds(e, b) {
		t.Fatal("b was not admitted")
	}
	if ns := admissionNotices(e, b); len(ns) != 0 {
		t.Fatalf("the thread's agent was told: %+v", ns)
	}
	if n := e.SubagentNote(b, "other"); n != "" {
		t.Fatalf("another subagent got %q", n)
	}
	if n := e.SubagentNote(b, "a1"); n != "[Fyr] Your test job is admitted now: run the command again." {
		t.Fatalf("the subagent got %q", n)
	}
	if n := e.SubagentNote(b, "a1"); n != "" {
		t.Fatalf("told twice: %q", n)
	}
	e.SubagentEnded(b, "a1")
	if ns := admissionNotices(e, b); len(ns) != 0 {
		t.Fatalf("the thread's agent was told after the subagent heard it: %+v", ns)
	}
}

func TestSubagentAdmissionASubagentThatRunsItAgainUsesIt(t *testing.T) {
	e, _, a, b, _ := subEngine(t)
	e.HeavyDone(a, "tu1")
	// The subagent runs the command again by itself before its next tool
	// call's result told it: the gate admits it and nothing is left to say.
	if r := e.HeavyGate(HeavyCall{Thread: b, Class: "go test ./...", ToolUse: "tu3", Subagent: true, Agent: "a1", AgentType: "worker"}); r != "" {
		t.Fatalf("refused: %q", r)
	}
	if n := e.SubagentNote(b, "a1"); n != "" {
		t.Fatalf("told after it ran: %q", n)
	}
	e.SubagentEnded(b, "a1")
	if ns := admissionNotices(e, b); len(ns) != 0 {
		t.Fatalf("the thread's agent was told: %+v", ns)
	}
}

func TestSubagentAdmissionASubagentThatEndsFirstIsNamedToTheParent(t *testing.T) {
	e, _, a, b, _ := subEngine(t)
	e.HeavyDone(a, "tu1")
	e.SubagentEnded(b, "a1") // it handed back before another tool call
	ns := admissionNotices(e, b)
	if len(ns) != 1 || ns[0].Urgency != core.Wake {
		t.Fatalf("the thread's agent: %+v", ns)
	}
	want := "The test job your subagent worker (a1) was queued for is admitted now, but that subagent has ended without running it: run the command again yourself if you still need its result."
	if ns[0].What != want {
		t.Fatalf("notice %q", ns[0].What)
	}
	if n := e.SubagentNote(b, "a1"); n != "" {
		t.Fatalf("an ended subagent still has %q", n)
	}
}

func TestSubagentAdmissionAGrantAfterTheSubagentEndedGoesToTheParentNamed(t *testing.T) {
	e, _, a, b, live := subEngine(t)
	live["a1"] = false // it handed back while queued
	e.HeavyDone(a, "tu1")
	if !told(e, b, "your subagent worker (a1) was queued for is admitted now") {
		t.Fatalf("the thread's agent was not told: %+v", admissionNotices(e, b))
	}
}

func TestSubagentAdmissionTheTickHandsAGoneSubagentsWaitToTheParent(t *testing.T) {
	e, now, a, b, live := subEngine(t)
	e.HeavyDone(a, "tu1")
	live["a1"] = false // its process ended; no SubagentStop came
	*now = now.Add(time.Second)
	e.Tick()
	if !told(e, b, "your subagent worker (a1) was queued for is admitted now") {
		t.Fatalf("the thread's agent was not told: %+v", admissionNotices(e, b))
	}
}

// The thread's own refused command keeps the plain admission.
func TestSubagentAdmissionTheThreadsOwnCommandIsUnchanged(t *testing.T) {
	e, _, a, b, _ := subEngine(t)
	if r := e.HeavyGate(HeavyCall{Thread: b, Class: "go test ./...", ToolUse: "tu3"}); !strings.Contains(r, "queued") {
		t.Fatalf("not queued: %q", r)
	}
	e.HeavyDone(a, "tu1")
	if !told(e, b, "Your test job is admitted now: run the command again.") {
		t.Fatalf("not told: %+v", admissionNotices(e, b))
	}
	if n := e.SubagentNote(b, "a1"); n != "" {
		t.Fatalf("the subagent got the thread's admission: %q", n)
	}
}

// A command the gate held while memory was short: its recovery reaches the
// subagent the same way.
func TestSubagentAdmissionMemoryRecoveryReachesTheSubagent(t *testing.T) {
	e, now, a, _ := heavyEngine(t, 2)
	live := map[string]bool{"a1": true, "a2": true}
	e.SubagentLive = func(thread, agent string) bool { return thread == a && live[agent] }
	free := 0.5
	e.SlotPolicy = resource.DefaultPolicy
	e.SlotSignals = func() resource.Signals {
		return resource.Signals{Provider: "cgroups", Headroom: &free, Stall: resource.StallOK, CPU: 8, Memory: 16 << 30}
	}
	e.SlotFreeze = func(string) bool { return true }
	e.SlotThaw = func(string) {}
	guardDeps(t, e)
	e.HeavyGate(HeavyCall{Thread: a, Class: "make test", ToolUse: "tu1"})
	free = 0.04
	e.Tick()
	if r := e.HeavyGate(HeavyCall{Thread: a, Class: "make e2e", ToolUse: "tu2", Subagent: true, Agent: "a1", AgentType: "worker"}); !strings.Contains(r, "memory has recovered while you still run") {
		t.Fatalf("held: %q", r)
	}
	free = 0.5
	*now = now.Add(10 * time.Second)
	e.Tick()
	if ns := admissionNotices(e, a); len(ns) != 0 {
		t.Fatalf("the thread's agent was told: %+v", ns)
	}
	if n := e.SubagentNote(a, "a1"); n != "[Fyr] Memory has recovered on this host: run the heavy command Fyr held again." {
		t.Fatalf("the subagent got %q", n)
	}
	// One that has ended by then: the thread's agent is told, naming it.
	free = 0.04
	*now = now.Add(10 * time.Second)
	e.Tick()
	e.HeavyGate(HeavyCall{Thread: a, Class: "make e2e", ToolUse: "tu3", Subagent: true, Agent: "a2", AgentType: "worker"})
	live["a2"] = false
	free = 0.5
	*now = now.Add(10 * time.Second)
	e.Tick()
	if !told(e, a, "the heavy command Fyr held for your subagent worker (a2) can run now, but that subagent has ended") {
		t.Fatalf("the thread's agent was not told: %+v", admissionNotices(e, a))
	}
}
