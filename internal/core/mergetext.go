package core

import "time"

// A merge asked in text (V0.settle-merge-fixes (1)): the agent wrote "merge"
// at the start of a message instead of calling change_request {ask:
// "merge"}. A word in text asks Fyr for nothing, so the PR waited for the
// owner's press. At a clean end of a run on a thread with an open PR and no
// merge ask, the engine reads the run's texts; one that starts with the bare
// word lands here, and the agent is told to make the call.
const (
	// KindMergeText is the engine's reading: a text of run Run starts with
	// "merge".
	KindMergeText = "mergeText.seen"
	// ObsMergeText is the observer that reports it.
	ObsMergeText = "mergeText"
	// EffectMergeText asks the engine to read the texts of run Effect.Run.
	EffectMergeText EffectKind = "mergeText"
	// MergeTextText is the agent's notice.
	MergeTextText = `Your message starts with "merge", but a word in text asks Fyr for nothing: no merge ask is open on this thread's PR. To ask, call mcp__fyr__change_request {change, ask: "merge"}.`
)

// MergeTextArgs is the reading of one run.
type MergeTextArgs struct {
	Run int `json:"run"`
}

func init() {
	RegisterKind(&InputKind{Name: KindMergeText, From: observed, Reduce: reduceMergeText})
}

func reduceMergeText(r Rec, in Input, _ time.Time) (Result, error) {
	a, err := argsOf[MergeTextArgs](in)
	if err != nil {
		return Result{}, err
	}
	if !mergeTextDue(r.T) || a.Run != r.T.RunEnds {
		return Result{}, nil
	}
	return Result{Events: []Event{{"mergeText.seen", map[string]any{"run": a.Run}}}}, nil
}

// mergeTextDue reports a thread whose texts are worth reading: idle and
// awake, with an open PR and no merge ask, open or held.
func mergeTextDue(t *Thread) bool {
	return !t.Running && t.Failure == nil && !t.Asleep() && OpenPR(t) && !mergeAsked(t)
}

// mergeAsked reports a merge ask of the thread that is open or held for CI.
func mergeAsked(t *Thread) bool {
	for _, ds := range [][]Decision{t.Decisions, t.HeldAsks} {
		for _, d := range ds {
			if d.Kind == DecisionEffect && d.Effect != nil && d.Effect.Action == FyrMerge {
				return true
			}
		}
	}
	return false
}

// mergeTextAfter asks the engine to read a run that ended cleanly.
func mergeTextAfter(before, t *Thread, in Input) Result {
	if in.Kind != KindRunEnded || !before.Running || !mergeTextDue(t) {
		return Result{}
	}
	if a, ok := in.Args.(RunEndedArgs); !ok || a.Failure != nil {
		return Result{}
	}
	return Result{Effects: []Effect{{Kind: EffectMergeText, Run: t.RunEnds}}}
}

// mergeTextNotices tells the agent to make the call; it wakes the agent, so
// the PR does not wait for the owner's next message.
func mergeTextNotices(r Reduced) []Notice {
	for _, ev := range r.Events {
		if ev.Type == "mergeText.seen" {
			return []Notice{{About: "thread", Key: "merge-text", What: MergeTextText}}
		}
	}
	return nil
}
