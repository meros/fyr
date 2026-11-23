package core

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// The change tools' reducers (change_open, change_update and the change
// lookups they share), out of the kind table (V0.structure-cost (3)).

// changeKind is a change tool's reducer: fn finds or makes the change, and
// the reduce records one `change` event (V0.59 (5)).
func changeKind(op string, fn func(t *Thread, in Input, now time.Time) (*Change, bool, error)) func(Rec, Input, time.Time) (Result, error) {
	return changeKindNotes(op, func(t *Thread, in Input, now time.Time) (*Change, bool, []string, error) {
		c, isNew, err := fn(t, in, now)
		return c, isNew, nil, err
	})
}

// changeKindNotes is changeKind for a report whose reply carries notes
// (V0.101: what Fyr reads instead of an observed value).
func changeKindNotes(op string, fn func(t *Thread, in Input, now time.Time) (*Change, bool, []string, error)) func(Rec, Input, time.Time) (Result, error) {
	return func(r Rec, in Input, now time.Time) (Result, error) {
		c, isNew, notes, err := fn(r.T, in, now)
		if err != nil {
			return Result{}, err
		}
		ev := Event{"change", map[string]any{"op": op, "change": map[string]any{"id": c.ID, "title": c.Title, "kind": c.Kind, "phase": c.Phase, "phaseBy": c.PhaseBy, "label": c.Label}}}
		return Result{Events: []Event{ev}, Reply: ChangeReply{Change: *c, New: isNew, Notes: notes}}, nil
	}
}

// reportChange is change_update: the report, then a revision and proof
// when given (V0.101).
func reportChange(t *Thread, in Input, now time.Time) (*Change, bool, []string, error) {
	a, ok := in.Args.(ChangeUpdateArgs)
	if !ok {
		return nil, false, nil, fmt.Errorf("%w: %s got %T", ErrBadArgs, in.Kind, in.Args)
	}
	c, err := findChange(t, a.Change)
	if err != nil {
		return nil, false, nil, err
	}
	if len(a.Evidence) > 0 {
		for _, ev := range a.Evidence {
			if ev.Kind == "" || (ev.URL == "" && ev.Attachment == "") {
				return nil, false, nil, errors.New("evidence needs a kind and a url or an attachment")
			}
		}
	}
	notes, err := ReportChange(t, c, a.Update, in.Source.Principal(), now)
	if err != nil {
		return nil, false, nil, err
	}
	added := false
	if a.Revision != nil {
		added = ReviseChange(c, *a.Revision, now)
	}
	if len(a.Evidence) > 0 {
		id, group := c.ID, c.Group
		AttachEvidence(c, a.Evidence, now)
		c = joinPRWork(t, c, now) // V0.87 (2)
		if c == nil {
			c = FindChange(t, id)
		}
		for _, ev := range a.Evidence {
			if ev.Kind != "pr" || ev.URL == "" {
				continue
			}
			u, ok := CanonicalPR(ev.URL)
			if !ok {
				continue
			}
			repo, n := parsePRURL(u)
			if cur := changeByPR(t, u); cur == nil {
				if pc := ChangePRObserved(t, PR{URL: u, Repo: repo, Number: n, State: "OPEN", Checks: ChecksPending}, now); pc != nil {
					if pc.Group == "" && group != "" {
						pc.Group = group
					}
					if t.MainPR() == nil {
						t.MainChange = pc.ID
					}
				}
			} else {
				armBuiltins(t, cur, now)
				if t.MainPR() == nil && !cur.Dismissed {
					t.MainChange = cur.ID
				}
			}
		}
		if updated := FindChange(t, id); updated != nil {
			c = updated
		}
	}
	return c, added, notes, nil
}

// codeOnly refuses a change_open that is not code (V0.changes-code-only
// (3)) with the line that names its replacement; no kind is code. A code
// change names its branch or its PR: Fyr reads the rest itself.
func codeOnly(a *ChangeOpenArgs) error {
	if a.Open.Kind == "" {
		a.Open.Kind = KindCode
	}
	if line, gone := goneKinds[a.Open.Kind]; gone {
		return errors.New(line)
	}
	if a.Open.Kind != KindCode {
		return fmt.Errorf("kind must be %s", strings.Join(Names(OpenKinds()), ", "))
	}
	if strings.TrimSpace(a.Open.Branch) == "" && a.PR == "" {
		return errors.New("a code change needs its branch or its pr; Fyr finds pushes and PRs of gh pr create itself")
	}
	return nil
}

// NoChange is the reason for a change ref the thread does not have.
func NoChange(ref string) error {
	return &Rejection{Code: RejectNotFound, Reason: fmt.Sprintf("no change %q in this thread; open it with change_open, or list them with change_list", ref)}
}

func findChange(t *Thread, ref string) (*Change, error) {
	if c := FindChange(t, ref); c != nil {
		return c, nil
	}
	return nil, NoChange(ref)
}
