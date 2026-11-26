package heavy

import (
	"testing"
	"time"
)

func TestCallsCauseIsTheNewestCommandThatNamesWork(t *testing.T) {
	var c Calls
	t0 := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	c.Note("a", "git status", t0)                                                        // light
	c.Note("a", "cd web && yarn turbo run lint 2>&1 | tail -20", t0.Add(10*time.Second)) // the cause
	c.Note("a", "cat notes.md", t0.Add(40*time.Second))                                  // light, newer
	if got := c.Cause("a", t0.Add(50*time.Second)); got != "cd web && yarn turbo run lint 2>&1 | tail -20" {
		t.Errorf("Cause = %q", got)
	}
	if got := c.Cause("a", t0.Add(10*time.Minute)); got != "" {
		t.Errorf("a command from 10 minutes before is no cause: %q", got)
	}
	if got := c.Cause("b", t0); got != "" {
		t.Errorf("another thread's: %q", got)
	}
	c.Forget("a")
	if got := c.Cause("a", t0.Add(50*time.Second)); got != "" {
		t.Errorf("after Forget: %q", got)
	}
}

// The point: three episodes of a lint run taught by the agent's text become a
// class the gate matches the next time, whatever worktree they ran in.
func TestCallsTeachAClassTheGateMatches(t *testing.T) {
	b, m, _ := newBook(t)
	b.Load()
	var c Calls
	t0 := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	for i, wt := range []string{"fix-a", "feat-b", "perf-c"} {
		at := t0.Add(time.Duration(i) * time.Hour)
		c.Note("t", "cd /w/"+wt+" && yarn lint:typecheck 2>&1 | tail -5", at)
		cmd := c.Cause("t", at.Add(45*time.Second))
		if cmd == "" {
			t.Fatalf("episode %d: no cause", i)
		}
		b.Saw(Seen{Thread: "t", Command: cmd, At: at})
	}
	if class := m.Class("cd /w/other && yarn lint:typecheck > out.log 2>&1"); class == "" {
		t.Fatal("the learned class did not match the next lint run")
	}
}

func TestCallsRepeatIsTheSameCommandWithinTenMinutes(t *testing.T) {
	var c Calls
	t0 := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	if got := c.Repeat("a", "cd web && yarn turbo run lint 2>&1 | tail", t0); got != "" {
		t.Fatalf("the first run is no repeat: %q", got)
	}
	c.Note("a", "cd web && yarn turbo run lint 2>&1 | tail", t0)
	if got := c.Repeat("a", "yarn turbo run lint > out.log", t0.Add(30*time.Second)); got != "yarn turbo" {
		t.Errorf("a retry of it is a repeat: %q", got)
	}
	if got := c.Repeat("a", "yarn turbo run lint", t0.Add(11*time.Minute)); got != "" {
		t.Errorf("11 minutes later is none: %q", got)
	}
	if got := c.Repeat("b", "yarn turbo run lint", t0.Add(30*time.Second)); got != "" {
		t.Errorf("another thread's: %q", got)
	}
	if got := c.Repeat("a", "git status", t0.Add(time.Second)); got != "" {
		t.Errorf("a light command is no class: %q", got)
	}
}
