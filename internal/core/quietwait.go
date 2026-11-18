package core

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// SPEC 0.5 V0.195: an agent that ended its turn to wait for a background
// command depends on that command ending. A wrong hand-written wait never
// does, and the thread then just reads as waiting. The engine measures the
// thread's CPU (engine/quietwait.go); here is what the measure means.

// backgroundCommand: a Bash the agent left running, Monitor included.
func (k Task) backgroundCommand() bool {
	return k.Status == TaskRunning && k.Background && k.Type == "local_bash"
}

// mayStall: a running background command that should end by itself. A
// Monitor is quiet by design and has its own timeout.
func (k Task) mayStall() bool { return k.backgroundCommand() && k.Tool != "Monitor" }

// waitReason is the Waiting line of a thread with n running tasks: what it
// waits for when that is one command, else how many.
func waitReason(t *Thread, n int) string {
	var cmds []Task
	for _, k := range t.Tasks {
		if k.backgroundCommand() {
			cmds = append(cmds, k)
		}
	}
	switch {
	case len(cmds) == 1 && n == 1 && cmds[0].Description != "":
		return "Background: " + cmds[0].Description
	case len(cmds) > 0:
		return fmt.Sprintf("%d background tasks", n)
	case n == 1:
		return "1 agent"
	}
	return fmt.Sprintf("%d agents", n)
}

// QuietCandidates are the tasks whose wait the CPU can judge: the turn is
// over, nothing else will wake the agent, and it has no unread message. A
// running subagent reports by itself when it ends, so it rules the thread out.
func QuietCandidates(t *Thread) []Task {
	if t.Running || t.MachineWait != nil || t.Unresponsive != nil || len(t.Outbox) > 0 || len(t.Decisions) > 0 {
		return nil
	}
	var out []Task
	for _, k := range t.Tasks {
		switch {
		case k.mayStall():
			out = append(out, k)
		case k.Status == TaskRunning && !k.backgroundCommand():
			return nil
		}
	}
	return out
}

func reduceTaskQuiet(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[TaskQuietArgs](in)
	if err != nil {
		return Result{}, err
	}
	if a.Since == nil {
		for i := range r.T.Tasks {
			r.T.Tasks[i].QuietSince = nil
		}
		return Result{}, nil
	}
	if r.T.Running {
		return Result{}, nil // a turn began after the measure
	}
	var told []string
	for i := range r.T.Tasks {
		k := &r.T.Tasks[i]
		if !k.mayStall() || !slices.Contains(a.IDs, k.ID) {
			continue
		}
		if k.QuietSince == nil {
			k.QuietSince = a.Since
		}
		if !k.QuietTold {
			k.QuietTold = true
			told = append(told, k.label())
		}
	}
	if len(told) == 0 {
		return Result{}, nil
	}
	return Result{Events: []Event{{"task", map[string]any{"kind": "quiet", "description": strings.Join(told, ", "),
		"minutes": int(now.Sub(*a.Since) / time.Minute)}}}}, nil
}

func (k Task) label() string {
	if k.Description != "" {
		return k.Description
	}
	return k.ID
}

// taskQuietNotices wakes the agent to look at its wait. The wording leaves
// room for a wait that is right: Fyr cannot see what the command waits for.
func taskQuietNotices(r Reduced) []Notice {
	var out []Notice
	for _, ev := range r.Events {
		d, _ := ev.Data.(map[string]any)
		if ev.Type != "task" || d["kind"] != "quiet" {
			continue
		}
		m, _ := d["minutes"].(int)
		out = append(out, Notice{About: "thread", Key: "task.quiet", What: fmt.Sprintf("You are waiting for the background command `%s`, and this thread has used almost no CPU for %d min. "+
			"If what it waits for is over, its wait is stuck: check the real state yourself, stop it with TaskStop and go on. If it really waits for something outside this host, leave it; Fyr does not ask again about it.", str(d["description"]), m)})
	}
	return out
}
