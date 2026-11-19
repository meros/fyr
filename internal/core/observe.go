package core

import "time"

// Observations are facts reported by the agent or by GitHub. They change a
// thread without a user command. Each function is pure.

func RunStarted(t *Thread, now time.Time) {
	t.Running = true
	t.Starting = nil
	t.RunStartedAt = &now
	t.Activity = "Starting"
	t.Subagents = 0
	t.Failure = nil
	t.Stopped = false
}

// RunEnded closes the run. A nil failure means it finished normally or was
// interrupted by the user.
func RunEnded(t *Thread, f *Failure, now time.Time) {
	t.Running = false
	t.Starting = nil
	if f != nil {
		t.Unresponsive = nil // V0.143: the process is gone
	}
	t.RunStartedAt = nil
	t.Activity = ""
	t.ActivityAt = now
	t.Failure = f
	t.RunEnds++
	t.RunEndedAt = &now // V0.101: Stopped on a failure counts from here
	if f != nil {
		dropSettleDone(t) // the run did not finish
	}
}

// ProcessLost is called when the agent process exits or the host restarts
// while work was live. Open decisions die with the process.
// Effect Decisions (5.10) and questions asked with Fyr's tool (V0.ask-tool)
// do not need the process and stay open.
func ProcessLost(t *Thread, class, msg string, now time.Time) {
	t.Unresponsive = nil // V0.143: the process is gone
	t.MachineWait = nil  // V0.153
	t.Starting = nil     // V0.starting-moves: the start died with it
	var keep []Decision
	live := t.Running
	for _, d := range t.Decisions {
		if !d.NeedsProcess() {
			keep = append(keep, d)
		} else {
			live = true
		}
	}
	for i := range t.Tasks {
		if t.Tasks[i].Status == TaskRunning {
			t.Tasks[i].Status, t.Tasks[i].UpdatedAt = TaskStopped, now
		}
	}
	if !live {
		return
	}
	t.Decisions = keep
	RunEnded(t, &Failure{Class: class, Message: msg, At: now}, now)
}

// Restarted: Fyr ended an unresponsive process to start the session again
// (V0.143). Its background tasks ended with it; the run ends without a
// failure, and the unread messages stay in the outbox for the next process.
func Restarted(t *Thread, now time.Time) {
	t.Unresponsive = nil
	for i := range t.Tasks {
		if t.Tasks[i].Status == TaskRunning {
			t.Tasks[i].Status, t.Tasks[i].UpdatedAt = TaskStopped, now
		}
	}
	var keep []Decision
	for _, d := range t.Decisions {
		if !d.NeedsProcess() {
			keep = append(keep, d)
		}
	}
	t.Decisions = keep
	if t.Running {
		RunEnded(t, nil, now)
	}
}

func DecisionOpened(t *Thread, d Decision) {
	t.Decisions = append(t.Decisions, d)
	t.ActivityAt = d.CreatedAt
	t.QuietMerge = "" // V0.200: this is news of its own
}

// DecisionClosed removes a decision that the agent withdrew (for example
// after an interrupt). It is a no-op when the user already answered it.
func DecisionClosed(t *Thread, id string) {
	for i, d := range t.Decisions {
		if d.ID == id {
			t.Decisions = append(t.Decisions[:i:i], t.Decisions[i+1:]...)
			break
		}
	}
	if len(t.Decisions) == 0 {
		t.Decisions = nil
	}
}

// PRObserved links a PR to the thread as its main PR (#32): the agent opened
// or named it. Its code change keeps the snapshot and becomes the thread's
// main change. A PR the owner dismissed never comes back (V0.36), and a link
// never overwrites what GitHub already reported about the PR.
func PRObserved(t *Thread, p PR, now time.Time) {
	c := changeByPR(t, p.URL)
	if c != nil && c.Dismissed {
		return
	}
	if c == nil || c.PR == nil || c.PR.CheckedAt == nil || p.CheckedAt != nil {
		c = ChangePRObserved(t, p, now)
	}
	if c != nil {
		t.MainChange = c.ID
	}
}

// TaskObserved merges a task report into the thread: non-empty fields of k
// replace the stored ones. A task that ends moves ActivityAt, so the thread
// is done when its last agent finishes (SPEC 0.5 V0.16).
func TaskObserved(t *Thread, k Task, now time.Time) {
	k.UpdatedAt = now
	for i := range t.Tasks {
		cur := &t.Tasks[i]
		if cur.ID != k.ID {
			continue
		}
		ended := cur.Status == TaskRunning && k.Status != "" && k.Status != TaskRunning
		if k.Description != "" {
			cur.Description = k.Description
		}
		if k.Type != "" {
			cur.Type = k.Type
		}
		if k.Status != "" {
			cur.Status = k.Status
		}
		if k.Activity != "" {
			cur.Activity = k.Activity
		}
		if k.Summary != "" {
			cur.Summary = k.Summary
		}
		if k.ToolUses > 0 {
			cur.ToolUses = k.ToolUses
		}
		if k.Tokens > 0 {
			cur.Tokens = k.Tokens
		}
		cur.Background = cur.Background || k.Background
		if k.Tool != "" {
			cur.Tool = k.Tool
		}
		cur.UpdatedAt = now
		if ended {
			t.ActivityAt = now
		}
		return
	}
	if k.Status == "" {
		k.Status = TaskRunning
	}
	if k.StartedAt == nil {
		k.StartedAt = &now
	}
	t.Tasks = append(t.Tasks, k)
	t.Tasks = keepLast(t.Tasks, 20) // keep the list short
}
