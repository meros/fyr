package core

import "time"

// Observations are facts reported by the agent or by GitHub. They change a
// thread without a user command. Each function is pure.

func RunStarted(t *Thread, now time.Time) {
	t.Running = true
	t.RunStartedAt = &now
	t.Activity = "Starting"
	t.Subagents = 0
	t.Failure = nil
}

// RunEnded closes the run. A nil failure means it finished normally or was
// interrupted by the user.
func RunEnded(t *Thread, f *Failure, now time.Time) {
	t.Running = false
	t.RunStartedAt = nil
	t.Activity = ""
	t.ActivityAt = now
	t.Failure = f
	if f == nil && t.SettleWhenDone && len(t.Decisions) == 0 {
		t.SettledAt = &now
		t.SettleWhenDone = false
	}
	if f != nil {
		t.SettleWhenDone = false
	}
}

// ProcessLost is called when the agent process exits or the host restarts
// while work was live. Open decisions die with the process.
// Effect Decisions (5.10) do not need the process and stay open.
func ProcessLost(t *Thread, class, msg string, now time.Time) {
	var keep []Decision
	live := t.Running
	for _, d := range t.Decisions {
		if d.Kind == "effect" {
			keep = append(keep, d)
		} else {
			live = true
		}
	}
	for i := range t.Tasks {
		if t.Tasks[i].Status == "running" {
			t.Tasks[i].Status, t.Tasks[i].UpdatedAt = "stopped", now
		}
	}
	if !live {
		return
	}
	t.Decisions = keep
	RunEnded(t, &Failure{Class: class, Message: msg, At: now}, now)
}

func DecisionOpened(t *Thread, d Decision) {
	t.Decisions = append(t.Decisions, d)
	t.ActivityAt = d.CreatedAt
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

// PRObserved stores a new PR snapshot. It moves ActivityAt only when the PR
// starts to block, merges or closes, so polling alone never marks a thread done.
func PRObserved(t *Thread, p PR, now time.Time) {
	old := t.PR
	t.PR = &p
	changed := old == nil || old.State != p.State || old.Blocking() != p.Blocking()
	if changed && (p.Blocking() != "" || p.State != "OPEN") {
		t.ActivityAt = now
	}
	if p.State == "MERGED" && t.SettleWhenMerged && t.SettledAt == nil {
		t.SettledAt = &now
		t.SettleWhenMerged = false
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
		ended := cur.Status == "running" && k.Status != "" && k.Status != "running"
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
		cur.UpdatedAt = now
		if ended {
			t.ActivityAt = now
		}
		return
	}
	if k.Status == "" {
		k.Status = "running"
	}
	if k.StartedAt == nil {
		k.StartedAt = &now
	}
	t.Tasks = append(t.Tasks, k)
	// Keep the list short: the 20 newest.
	if len(t.Tasks) > 20 {
		t.Tasks = t.Tasks[len(t.Tasks)-20:]
	}
}
