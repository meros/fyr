package core

import (
	"fmt"
	"time"
)

// The owner's PR actions from the PR tab (SPEC 0.5 V0.43). They run as
// effects the owner started (Effect.ByOwner): no Decision, and the agent is
// not told.

func openPR(t *Thread, v *View, now time.Time) string {
	if r := notArchived(t, v, now); r != "" {
		return r
	}
	if t.PR == nil {
		return "No PR"
	}
	if t.PR.State != "" && t.PR.State != "OPEN" {
		return "The PR is not open"
	}
	return ""
}

func init() {
	register(&Action{Name: "pr.merge", Label: "Merge PR",
		Available: func(t *Thread, v *View, now time.Time) string {
			if r := openPR(t, v, now); r != "" {
				return r
			}
			if t.PR.Conflict {
				return "The PR has merge conflicts"
			}
			if t.PR.Review == "CHANGES_REQUESTED" {
				return "Changes requested"
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			m := a.Str("method")
			if m == "" {
				m = "squash"
			}
			if m != "squash" && m != "merge" && m != "rebase" {
				return nil, fmt.Errorf("%w: method must be squash, merge or rebase", ErrBadArgs)
			}
			return []Effect{{Kind: "effect", Allow: true, ByOwner: true,
				Request: &EffectReq{Action: "pr.merge", Args: map[string]any{"pr": t.PR.URL, "method": m}}}}, nil
		}})

	register(&Action{Name: "pr.rerunFailed", Label: "Re-run failed checks",
		Available: openPR,
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			return []Effect{{Kind: "effect", Allow: true, ByOwner: true,
				Request: &EffectReq{Action: "pr.rerunFailed", Args: map[string]any{"pr": t.PR.URL}}}}, nil
		}})
}
