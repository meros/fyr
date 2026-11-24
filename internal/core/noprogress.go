package core

import (
	"fmt"
	"slices"
	"time"
)

// V0.no-progress (#448): a live run that makes no progress for
// NoProgressAfter (no tool call, no output, no turn event) is blocked in
// Your move: a stuck tool or an endless wait looked like work forever.
// Waits Fyr knows to be long are not silence: an open Bash call (a build,
// an admitted heavy job, a frozen one), a background task, an open ask or
// Run card, a merge ask held for CI, an armed watch, the slot queue, a wait
// for the machine or the provider. The agent is told once; any progress
// clears the state, and the next silence counts from there.

// NoProgressAfter is how long a live run may make no progress before it is
// blocked.
const NoProgressAfter = 20 * time.Minute

// NoProgress is a live run's silence that Fyr holds blocked.
type NoProgress struct {
	// Since is the run's last progress, Last its activity then.
	Since time.Time `json:"since"`
	Last  string    `json:"last,omitempty"`
	At    time.Time `json:"at"`
}

// KindProgress is the agent process's output that is no message of its
// own: a provider's rate-limit or retry notice (the CLI waits, and says so).
const KindProgress = "agent.progress"

// The events of the state.
const (
	EventNoProgress = "agent.noProgress"
	EventProgress   = "agent.progress"
)

func init() {
	RegisterKind(&InputKind{Name: KindProgress, From: observed, Untouched: true, Reduce: func(Rec, Input, time.Time) (Result, error) { return Result{}, nil }})
}

// progressKinds are the agent's facts that are progress: its messages and
// tool calls (digest), its tasks, a turn's start, a message it took in, a
// tool approval it asks, its process's other output, and the slot of its
// heavy job.
var progressKinds = []string{KindDigest, KindTask, KindRunStarted, KindMessageState, KindSession,
	KindDecisionOpened, KindProgress, KindSlotGranted, KindSlotMemory, KindSlotOver, KindSlotPause}

// progressed reports an Input that is the run's progress: one of
// progressKinds, or any call of the agent's own tools.
func progressed(in Input) bool {
	return in.Source == FromAgent || slices.Contains(progressKinds, in.Kind)
}

// longWait is why the run's silence is a wait Fyr knows, or "".
func longWait(t *Thread, now time.Time) string {
	switch {
	case t.OpenBash > 0:
		return "a command runs"
	case t.RunningTasks() > 0:
		return "a background task runs"
	case len(t.Decisions) > 0:
		return "an ask is open"
	case PendingRequest(t) != nil || runRunning(t.Runs):
		return "a Run card"
	case len(t.HeldAsks) > 0 || ciWait(t) != "":
		return "waiting for CI"
	case WatchWaitText(t, now) != "":
		return "a watch is armed"
	case t.SlotQueued:
		return "queued for a slot"
	case t.GateHold != nil:
		return "held at the gate"
	case t.MachineWait != nil:
		return "waiting for the machine"
	case QuotaWaitText(t, now) != "":
		return "waiting for quota"
	case t.Unresponsive != nil || t.Handover != nil:
		return "held"
	}
	return ""
}

func runRunning(rs []ActionRun) bool {
	return slices.ContainsFunc(rs, func(r ActionRun) bool { return r.Running })
}

// noProgressAfter runs at the end of each reduce (the clock's included): it
// moves the run's last progress, and marks or clears the silence.
func noProgressAfter(before, t *Thread, in Input, now time.Time) []Event {
	if !t.Running {
		t.ProgressAt, t.NoProgress, t.OpenBash = nil, nil, 0
		return nil
	}
	if in.Kind == KindDigest {
		if a, ok := in.Args.(DigestArgs); ok && a.OpenBash != nil {
			t.OpenBash = *a.OpenBash
		}
	}
	// A wait Fyr knows counts as progress while it lasts, so the silence
	// after it counts from its end, not from the last tool call before it.
	if progressed(in) || !before.Running || longWait(before, now) != "" || longWait(t, now) != "" {
		t.ProgressAt = &now
		if t.NoProgress != nil {
			t.NoProgress = nil
			return []Event{{EventProgress, map[string]any{}}}
		}
		return nil
	}
	if t.NoProgress != nil {
		return nil
	}
	since := progressSince(t)
	if since == nil || now.Sub(*since) < NoProgressAfter {
		return nil
	}
	t.NoProgress = &NoProgress{Since: *since, Last: t.Activity, At: now}
	t.ActivityAt = now
	return []Event{{EventNoProgress, map[string]any{"since": *since, "last": t.Activity}}}
}

// progressSince is the run's last progress: a run from before Fyr kept it
// counts from its start.
func progressSince(t *Thread) *time.Time {
	if t.ProgressAt != nil {
		return t.ProgressAt
	}
	return t.RunStartedAt
}

// noProgressCheck is when the clock must re-reduce a live run to mark its
// silence, or nil. A wait Fyr knows needs no clock: the reduce that ends it
// moves the last progress.
func noProgressCheck(t *Thread, now time.Time) *time.Time {
	if !t.Running || t.NoProgress != nil || longWait(t, now) != "" {
		return nil
	}
	since := progressSince(t)
	if since == nil {
		return nil
	}
	at := since.Add(NoProgressAfter)
	return &at
}

// NoProgressWord is the row's word of the state; NoProgressSubject the line
// under it: the last activity and when, in the host's local time.
func NoProgressWord() string {
	return fmt.Sprintf("No progress for %d min", int(NoProgressAfter/time.Minute))
}

func NoProgressSubject(n *NoProgress) string {
	last := n.Last
	if last == "" {
		last = "the run started"
	}
	return "last: " + last + " at " + n.Since.Local().Format("15:04")
}

// noProgressNotices: the agent's one notice of its silence.
func noProgressNotices(r Reduced) []Notice {
	for _, ev := range r.Events {
		if ev.Type != EventNoProgress || r.After.NoProgress == nil {
			continue
		}
		return []Notice{{About: "thread", Key: "no-progress", What: fmt.Sprintf(
			"Your run made no progress for %d min (no tool call, no output; %s), so the owner sees this thread as blocked. "+
				"If a command or tool hangs, stop it. Run a long job in the background or as a Run card (mcp__fyr__run_request), "+
				"and wait for something with mcp__fyr__watch and end your turn, so Fyr counts it as work.",
			int(NoProgressAfter/time.Minute), NoProgressSubject(r.After.NoProgress))}}
	}
	return nil
}
