package core

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

// V0.wakeups-via-watch: an agent's wake-up is its watch on a time
// ({kind: "at"}, then notify). V0.quota-reset-wake: a watch on a provider's
// quota window ({kind: "quota"}, armed by the agent or by the owner with
// thread.wakeOnQuota) is a wake-up too. The owner sees both on the thread
// and the inbox row, and cancels either with thread.cancelWake.

// KindCancelWake is the owner's cancel of a thread's wake-up.
const KindCancelWake = "thread.cancelWake"

// KindWakeOnQuota is the owner's action to continue a thread when a
// provider's quota window resets (V0.quota-reset-wake).
const KindWakeOnQuota = "thread.wakeOnQuota"

// WakeUp is one armed wake-up as clients and notices read it.
type WakeUp struct {
	Rule     string    `json:"rule"`
	At       time.Time `json:"at"`
	Note     string    `json:"note,omitempty"`
	Provider string    `json:"provider,omitempty"`
	Window   string    `json:"window,omitempty"`
	By       string    `json:"by,omitempty"`
}

// WakeOf is the wake-up a Rule is, or nil.
func WakeOf(r Rule) *WakeUp {
	if r.Purpose != PurposeWatch || (r.CreatedBy != ByAgent && r.CreatedBy != ByOwner) || r.Status == RuleSuspended || r.When.At == nil {
		return nil
	}
	if r.When.Kind != WhenAt && r.When.Kind != WhenQuota {
		return nil
	}
	note, _ := r.Then.Args["note"].(string)
	return &WakeUp{Rule: r.ID, At: *r.When.At, Note: note, Provider: r.When.Provider, Window: r.When.Window, By: r.CreatedBy}
}

// Wakes are the thread's armed wake-ups, soonest first.
func Wakes(t *Thread) []WakeUp {
	var out []WakeUp
	for _, r := range t.Rules {
		if w := WakeOf(r); w != nil {
			out = append(out, *w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out
}

func init() {
	register(&Action{Name: KindWakeOnQuota, Label: "Wake when quota resets",
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.Settle.At != nil {
				return "Settled threads do not wake"
			}
			return notArchived(t, v, now)
		},
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			provider := strings.TrimSpace(a.Str("provider"))
			if provider == "" {
				provider = t.Profile
			}
			if provider == "" {
				return nil, fmt.Errorf("%w: provider is required", ErrBadArgs)
			}
			at, err := time.Parse(time.RFC3339, a.Str("at"))
			if err != nil || !at.After(now) {
				return nil, fmt.Errorf("%w: at must be a future RFC 3339 time", ErrBadArgs)
			}
			at = at.UTC()
			window := strings.TrimSpace(a.Str("window"))
			note := strings.TrimSpace(a.Str("note"))
			if note == "" {
				what := "The quota window"
				if window != "" {
					what = "The " + window + " quota window"
				}
				if t.Failure != nil {
					note = what + " has reset (the last run stopped with: " + t.Failure.Message + "). Continue from where you stopped."
				} else {
					note = what + " has reset. Continue from where you stopped."
				}
			}
			id := strings.TrimSpace(a.Str("id"))
			if id == "" {
				id = "w_quota_" + provider
				if window != "" {
					id += "_" + strings.ReplaceAll(window, " ", "")
				}
			}
			// Replace an existing armed quota wake for the same (provider, window).
			DropRules(t, func(r Rule) bool {
				return r.Purpose == PurposeWatch && r.When.Kind == WhenQuota && r.When.Provider == provider && r.When.Window == window
			})
			t.Failure = nil
			t.QuotaResumes = 0
			t.QuotaResumedAt = nil
			Unsnooze(v)
			v.SeenAt = now
			ArmRule(t, Rule{
				ID:        id,
				Purpose:   PurposeWatch,
				When:      When{Kind: WhenQuota, Provider: provider, Window: window, At: &at},
				Then:      Then{Kind: KindRuleNotify, Args: map[string]any{"note": note}},
				After:     AfterDisarm,
				CreatedBy: ByOwner,
			}, now)
			return nil, nil
		}})

	register(&Action{Name: KindCancelWake, Label: "Cancel wake-up",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if len(Wakes(t)) == 0 {
				return "No wake-up is set"
			}
			return ""
		},
		// rule: the watch to cancel; "" cancels the soonest.
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			id := a.trimmed("rule")
			ws := Wakes(t)
			if id == "" && len(ws) > 0 {
				id = ws[0].Rule
			}
			x := FindRule(t, id)
			if x == nil || WakeOf(*x) == nil {
				return nil, reject(RejectNotFound, "no wake-up %q on this thread", id)
			}
			DropRules(t, func(y Rule) bool { return y.ID == id })
			return nil, nil
		}})
}

// cancelledWakes are the owner's notices for agent wake-ups an owner's input
// dropped (V0.wakeups-via-watch): the agent must not count on them. An
// owner-armed wake-up cancels quietly.
func cancelledWakes(b, a *Thread) []Notice {
	var out []Notice
	for _, w := range Wakes(b) {
		if w.By == ByOwner {
			continue
		}
		if x := FindRule(a, w.Rule); x != nil && WakeOf(*x) != nil {
			continue
		}
		what := fmt.Sprintf("The owner cancelled your wake-up %s at %s", w.Rule, w.At.UTC().Format(time.RFC3339))
		if w.Window != "" {
			what += fmt.Sprintf(" (when the %s window resets)", w.Window)
		}
		if w.Note != "" {
			what += fmt.Sprintf(" (%q)", w.Note)
		}
		out = append(out, Notice{About: "thread", Key: "owner:wake:" + w.Rule, What: what + "; it will not fire. Do not set it again unless the owner asks."})
	}
	return out
}

// V0.waits-strip: every armed watch of the agent's (a time, a quota reset, a
// change reaching a phase, a check, the run's end, idleness) is a wait the
// owner sees above the composer and cancels one by one.

// KindCancelWait is the owner's cancel of one of the agent's waits.
const KindCancelWait = "thread.cancelWait"

// Wait is one armed watch as clients read it: its id and its condition in a few words.
// At is when it wakes, for a wait on a time (a wake-up, a quota reset): the
// owner can snooze the thread until then (V0.snooze-until-wake).
type Wait struct {
	Rule string     `json:"rule"`
	Text string     `json:"text"`
	By   string     `json:"by,omitempty"`
	At   *time.Time `json:"at,omitempty"`
}

// Waits are the thread's armed notify watches, soonest wake-up first, the
// words of WatchWaitText.
func Waits(t *Thread, now time.Time) []Wait {
	var out []Wait
	for _, r := range agentWatches(t) {
		w := Wait{Rule: r.ID, Text: watchText(t, r.When, now), By: r.CreatedBy}
		if (r.When.Kind == WhenAt || r.When.Kind == WhenQuota) && r.When.At != nil {
			at := *r.When.At
			w.At = &at
		}
		out = append(out, w)
	}
	return out
}

func init() {
	register(&Action{Name: KindCancelWait, Label: "Cancel wait",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if len(agentWatches(t)) == 0 {
				return "Nothing is waited for"
			}
			return ""
		},
		// rule: the watch to cancel; "" cancels the first.
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			id := a.trimmed("rule")
			ws := agentWatches(t)
			if id == "" && len(ws) > 0 {
				id = ws[0].ID
			}
			if !slices.ContainsFunc(ws, func(r Rule) bool { return r.ID == id }) {
				return nil, reject(RejectNotFound, "no wait %q on this thread", id)
			}
			DropRules(t, func(y Rule) bool { return y.ID == id })
			return nil, nil
		}})
}

// cancelledWaits are the agent's notices for its watches an owner's cancel
// dropped: it must not count on them. An owner-armed watch cancels quietly.
func cancelledWaits(b, a *Thread, now time.Time) []Notice {
	var out []Notice
	for _, r := range agentWatches(b) {
		if r.CreatedBy == ByOwner || FindRule(a, r.ID) != nil {
			continue
		}
		out = append(out, Notice{About: "thread", Key: "owner:wait:" + r.ID, What: fmt.Sprintf("The owner cancelled your watch %s (%s); it will not fire. Do not set it again unless the owner asks.", r.ID, watchText(b, r.When, now))})
	}
	return out
}
