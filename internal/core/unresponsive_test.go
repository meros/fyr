package core

import (
	"strings"
	"testing"
	"time"
)

// SPEC 0.5 V0.143: an unresponsive agent puts its thread in Needs you with
// one line that says since when and why, ahead of Working.
func TestUnresponsiveAttention(t *testing.T) {
	since := time.Date(2026, 11, 17, 8, 58, 0, 0, time.Local)
	th := &Thread{}
	th.Running = true
	th.Unresponsive = &Unresponsive{Since: since, Reason: "it did not take a message within 1 min"}
	a := Compute(th, nil, since.Add(time.Hour))
	if a.State != Blocked || a.Section != NeedsYou {
		t.Fatalf("attention %+v", a)
	}
	if want := "Agent not responding since 08:58: it did not take a message within 1 min"; a.Reason != want {
		t.Fatalf("reason %q, want %q", a.Reason, want)
	}
}

// V0.143: Restart is there only while the agent does not respond; a
// restart ends the run without a failure, stops the tasks and keeps the
// unread messages.
func TestRestartActionAndReduce(t *testing.T) {
	th := &Thread{}
	if why := Registry["thread.restart"].Available(th, nil, time.Now()); why == "" {
		t.Fatal("restart available while responding")
	}
	now := time.Now()
	th.Running = true
	th.Unresponsive = &Unresponsive{Since: now, Reason: "x"}
	th.Tasks = []Task{{ID: "b1", Status: TaskRunning}}
	th.Outbox = []OutMsg{{ID: "m1", State: "sent", Text: "status?"}}
	if why := Registry["thread.restart"].Available(th, nil, now); why != "" {
		t.Fatalf("restart unavailable: %s", why)
	}
	Restarted(th, now)
	if th.Running || th.Failure != nil || th.Unresponsive != nil || th.Tasks[0].Status != TaskStopped || len(th.Outbox) != 1 {
		t.Fatalf("after restart: %+v", th.Conversation)
	}
}

// V0.143 (5): the gate nudges a background Bash of an hour or more, and
// nothing else.
func TestGateNudgesLongBackgroundBash(t *testing.T) {
	long := map[string]any{"command": "cloud-sql-proxy", "run_in_background": true, "timeout": float64(7200000)}
	if n := GateNudge("Bash", long); !strings.Contains(n, "setsid nohup timeout") {
		t.Fatalf("nudge %q", n)
	}
	for _, in := range []map[string]any{
		{"command": "make test", "run_in_background": true, "timeout": float64(600000)},
		{"command": "sleep 9000", "timeout": float64(7200000)},
	} {
		if n := GateNudge("Bash", in); n != "" {
			t.Fatalf("nudge for %v: %q", in, n)
		}
	}
	if Gate("Bash", long) != "" {
		t.Fatal("a long background Bash is denied, not nudged")
	}
}
