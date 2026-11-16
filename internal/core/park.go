package core

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Not now (SPEC 0.5 V0.84, #93; V0.101): the owner parks a change that
// waits on them. It leaves Needs you until its head changes (the PR's head,
// else its newest revision) or a new failure type appears (decision 0024
// section 5). Not now on an ask declines the ask instead.

// fingerprint is what a park is keyed to: the change's head and the sorted
// set of its failing condition types.
func (c *Change) fingerprint() string {
	var types []string
	for _, k := range c.Conditions {
		if k.Status == CondFail && !slices.Contains(types, string(k.Type)) {
			types = append(types, string(k.Type))
		}
	}
	slices.Sort(types)
	return changeHead(c) + "|" + strings.Join(types, ",")
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

// BackText is when a parked change comes back, for the card and the agent.
const BackText = "back when the head changes"

// ParkedText says that the owner parked a change, for the agent's view of
// it (fyr_state, change_list), or "".
func ParkedText(c *Change) string {
	if !c.ParkHolds() {
		return ""
	}
	return fmt.Sprintf("the owner parked it (Not now) at %s: it is not in Needs you now; it comes back by itself when its head changes or a new failure appears, so do not ask about it again until then",
		c.Parked.At.UTC().Format("2006-01-02 15:04 UTC"))
}

// parkable reports a change with a Ready or failure cause, before any Not
// now: what the owner can park (an ask is declined instead).
func parkable(t *Thread, c *Change) bool {
	x := *c
	x.Parked = nil
	n := ChangeNeed(t, &x)
	return n != nil && (n.Cause == CauseReady || n.Cause == CauseFailure)
}

// parkChange is the owner's change.park {change}: Not now on a change that
// waits on them.
func parkChange(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
	c := FindChange(t, strings.TrimSpace(a.Str("change")))
	if c == nil {
		return nil, NoChange(a.Str("change"))
	}
	if c.ParkHolds() {
		return nil, reject(RejectUnavailable, "%q is already parked", c.Title)
	}
	if !parkable(t, c) {
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
				if parkable(t, &t.Changes[i]) && !t.Changes[i].ParkHolds() {
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
