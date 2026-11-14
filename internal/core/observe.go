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
func ProcessLost(t *Thread, class, msg string, now time.Time) {
	if !t.Running && len(t.Decisions) == 0 {
		return
	}
	t.Decisions = nil
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
