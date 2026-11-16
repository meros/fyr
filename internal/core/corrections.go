package core

// The owner's corrections (SPEC V0.101, decision 0024 section 6, slice 5):
// Refresh now, Done (V0.98) and Abandon on every card, a thread change or a
// host record, each an owner Input with a receipt. An override shows on the
// card ("abandoned by you", "marked done by you") and blocks later agent
// writes to that field (rights.go, ReportChange).

import (
	"slices"
	"strings"
	"time"
)

const (
	// KindChangeRefresh is the owner's Refresh now: Fyr reads GitHub and
	// git now.
	KindChangeRefresh = "change.refresh"
	// KindRecordEnd is the owner's Abandon on a host record: the host sends
	// change.end there when the card is one.
	KindRecordEnd = "record.end"
	// EffectChangeRefresh reads the thread's PRs and git now.
	EffectChangeRefresh EffectKind = "change.refresh"
)

// changeRefresh is change.refresh {change}: a change of the thread or a
// host record it shows.
func changeRefresh(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
	id := strings.TrimSpace(a.Str("change"))
	if FindChange(t, id) == nil && !slices.ContainsFunc(t.Refs, func(r Ref) bool { return r.ID == id }) {
		return nil, NoChange(id)
	}
	return []Effect{{Kind: EffectChangeRefresh}}, nil
}

func init() {
	register(&Action{Name: KindChangeRefresh, Label: "Refresh now",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if len(t.Changes) == 0 && len(t.Refs) == 0 {
				return "No change to read"
			}
			return ""
		},
		Apply: changeRefresh})
}

// recordEnd is the owner's Abandon on a host record (V0.101): the record
// ends as abandoned by the owner and is done.
func recordEnd(rs []*Change, in Input, now time.Time) (before, after *Change, err error) {
	id := strings.TrimSpace(mapArgs(in).Str("change"))
	i := slices.IndexFunc(rs, func(c *Change) bool { return c.ID == id })
	if i < 0 {
		return nil, nil, NoChange(id)
	}
	if rs[i].Phase.Ended() {
		return nil, nil, reject(RejectUnavailable, "%q is already %s", rs[i].Title, rs[i].Phase)
	}
	before = rs[i]
	c := cloneChange(before)
	setPhase(nil, c, PhaseAbandoned, ByOwner, now)
	c.Label = strings.Join(strings.Fields(mapArgs(in).Str("reason")), " ")
	c.Done = &DoneMark{At: now, By: ByOwner, Reason: string(PhaseAbandoned)}
	c.UpdatedAt, c.TouchedAt = now, &now
	return before, c, nil
}
