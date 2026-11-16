package core

import (
	"fmt"
	"hash/fnv"
	"strings"
	"time"
)

// Not now (SPEC 0.5 V0.84, #93): the owner parks a change that needs them.
// It stops blocking its thread and leaves Needs you until something the
// owner saw on it changes: its needs, label, phase, failed or pending
// conditions with their reasons, or its newest revision.

// fingerprint is what the owner sees of a change when they park it.
func (c *Change) fingerprint() string {
	h := fnv.New64a()
	w := func(s string) { h.Write([]byte(s)); h.Write([]byte{0}) }
	w(c.Needs)
	w(c.Label)
	w(string(c.Phase))
	for _, k := range c.Conditions {
		if k.Status == CondFail || k.Status == CondPending {
			w(string(k.Type))
			w(string(k.Status))
			w(k.Reason)
		}
	}
	if n := len(c.Revisions); n > 0 {
		w(c.Revisions[n-1].ID)
	}
	return fmt.Sprintf("%016x", h.Sum64())
}

// ParkHolds reports a change the owner parked that is still as they saw it.
func (c *Change) ParkHolds() bool {
	return c.Parked != nil && c.Parked.Seen == c.fingerprint()
}

// unparkStale clears the park of every change that moved since the owner
// parked it, so the agent and the clients see it is back (V0.84 (3)).
func unparkStale(t *Thread) {
	for i := range t.Changes {
		if c := &t.Changes[i]; c.Parked != nil && !c.ParkHolds() {
			c.Parked = nil
		}
	}
}

// ParkedText says that the owner parked a change, for the agent's view of
// it (fyr_state, change_list), or "".
func ParkedText(c *Change) string {
	if !c.ParkHolds() {
		return ""
	}
	return fmt.Sprintf("the owner parked it (Not now) at %s: it does not block the thread now; it comes back by itself when its needs, label, phase, conditions or revision change, so do not ask about it again until then",
		c.Parked.At.UTC().Format("2006-01-02 15:04 UTC"))
}

// parkChange is the owner's change.park {change}: Not now on a change that
// blocks.
func parkChange(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
	c := FindChange(t, strings.TrimSpace(a.Str("change")))
	if c == nil {
		return nil, NoChange(a.Str("change"))
	}
	if c.ParkHolds() {
		return nil, reject(RejectUnavailable, "%q is already parked", c.Title)
	}
	if c.blocks() == "" {
		return nil, reject(RejectUnavailable, "nothing about %q needs you", c.Title)
	}
	c.Parked = &Park{At: now, Seen: c.fingerprint()}
	v.SeenAt = now
	return nil, nil
}

// unparkChange is change.unpark {change}: the Undo of Not now.
func unparkChange(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
	c := FindChange(t, strings.TrimSpace(a.Str("change")))
	if c == nil {
		return nil, NoChange(a.Str("change"))
	}
	if c.Parked == nil {
		return nil, reject(RejectUnavailable, "%q is not parked", c.Title)
	}
	c.Parked = nil
	return nil, nil
}

func init() {
	register(&Action{Name: KindChangePark, Label: "Not now",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			for i := range t.Changes {
				if t.Changes[i].Blocking() != "" {
					return ""
				}
			}
			return "No change needs you"
		},
		Apply: parkChange})
	register(&Action{Name: KindChangeUnpark, Label: "Undo Not now",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			for i := range t.Changes {
				if t.Changes[i].Parked != nil {
					return ""
				}
			}
			return "No change is parked"
		},
		Apply: unparkChange})
}
