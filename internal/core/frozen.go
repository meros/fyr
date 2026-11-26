package core

import (
	"slices"
	"time"
)

// V0.freeze-visible: the memory guard's freeze of a thread's work is a
// record on the thread, so the row says the work is frozen, not running.

// FreezeKey names a freeze by the slot event's "work" field: undeclared
// heavy work ("work": its command, or true) or the slotted job.
func FreezeKey(work any) string {
	if s, _ := work.(string); s != "" || work == true {
		return "work"
	}
	return "job"
}

func reduceFrozen(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[FrozenArgs](in)
	if err != nil {
		return Result{}, err
	}
	if a.Key == "" {
		a.Key = "job"
	}
	if a.Why == "" {
		if !unfreeze(r.T, a.Key) {
			return Result{}, nil
		}
		return Result{Events: []Event{{"agent.frozen", map[string]any{"done": a.Key}}}}, nil
	}
	t := r.T
	if t.Asleep() {
		return Result{}, nil
	}
	if t.Frozen == nil {
		t.Frozen = &Frozen{Since: now}
	} else if t.Frozen.Why == a.Why && slices.Contains(t.Frozen.Keys, a.Key) {
		return Result{}, nil
	}
	t.Frozen.Why = a.Why
	if !slices.Contains(t.Frozen.Keys, a.Key) {
		t.Frozen.Keys = append(t.Frozen.Keys, a.Key)
	}
	return Result{Events: []Event{{"agent.frozen", map[string]any{"key": a.Key, "why": a.Why}}}}, nil
}

// unfreeze ends the freeze key; the record goes with the last one. It
// reports whether anything changed.
func unfreeze(t *Thread, key string) bool {
	if t.Frozen == nil || !slices.Contains(t.Frozen.Keys, key) {
		return false
	}
	t.Frozen.Keys = slices.DeleteFunc(t.Frozen.Keys, func(k string) bool { return k == key })
	if len(t.Frozen.Keys) == 0 {
		t.Frozen = nil
	}
	return true
}
