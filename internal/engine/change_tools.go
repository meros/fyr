package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// The change tools of SPEC 0.5 V0.59 (5), decision 0018: one family for every
// change an agent makes or asks for. Git and GitHub are observed without
// them; the tools add intent, grouping and asks.

const changeRef = "The change: its id (c_…) or its key."

func (e *Engine) changeTools(s *mcp.Server) {
	e.offerTool(s) // V0.60
	s.Add(mcp.Tool{Name: "change_unchanged",
		Description: "Say that your open changes in Fyr are still current after this stretch of work, with a short reason. Call it instead of change_status when nothing about them moved.",
		Schema:      obj(map[string]any{"reason": str("Why they are still current, in a few words.")}, "reason"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			n := 0
			e.Observe(th, func(t *core.Thread, now time.Time) []agent.Ev {
				n = core.OpenAgentChanges(t)
				core.ChangesChecked(t, now)
				return nil
			})
			if n == 0 {
				return "You have no open changes; nothing to keep current.", nil
			}
			return fmt.Sprintf("Noted: %d open changes still current (%s).", n, sArg(a, "reason")), nil
		}})
	s.Add(mcp.Tool{Name: "change_open",
		Description: "Start a change record in Fyr, or get the one with this key (idempotent). Open one for every change you make or prepare outside this folder's git branch: a server or cluster change, a migration, a NixOS rebuild, another repo, a design, a document. Do it before the first edit. Git branches and GitHub PRs of this thread are recorded by Fyr itself; open one for them only to give a title, a group or a stack.",
		Schema: obj(map[string]any{
			"key":      str("A stable name, unique in this thread, for example \"k8s:prod/api replicas\" or \"doc:design-v2\". The same key returns the same change."),
			"kind":     map[string]any{"type": "string", "enum": core.ChangeKinds()},
			"title":    str("One line for the owner: what changes."),
			"targets":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Where it lands, as kind:name, for example \"k8s:gke-prod/api\", \"host:meros-server\", \"repo:github.com/o/r\", \"env:production\"."},
			"branch":   str("For code: the git branch."),
			"pr":       str("For code: a GitHub PR URL; Fyr watches its checks and reviews."),
			"group":    str("A name shared by changes that belong together, for example one feature across several repos."),
			"after":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Changes this one builds on (a stack), by id or key."},
			"revision": revisionSchema(),
		}, "key", "kind", "title"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			o := core.ChangeOpen{Key: sArg(a, "key"), Kind: core.ChangeKind(sArg(a, "kind")), Title: sArg(a, "title"), Branch: sArg(a, "branch"), Group: sArg(a, "group"),
				Targets: listArg(a, "targets"), After: listArg(a, "after"), Revision: revisionArg(a["revision"])}
			pr := sArg(a, "pr")
			var prRepo string
			var prN int
			if pr != "" {
				var ok bool
				if pr, ok = agent.CanonicalPR(pr); !ok {
					return "", errors.New("pr must be a GitHub pull request URL")
				}
				prRepo, prN, _ = agent.ParsePR(pr)
				o.Evidence = append(o.Evidence, core.Evidence{Kind: "pr", URL: pr, Title: fmt.Sprintf("#%d", prN)})
			}
			var c core.Change
			var made bool
			err := e.changeObserve(th, "open", func(t *core.Thread, now time.Time) (*core.Change, error) {
				x, m, err := core.OpenChange(t, o, core.ByAgent, now)
				if err != nil {
					return nil, err
				}
				made = m
				if pr != "" && t.MainPR() == nil {
					// The first PR is also the thread's main PR (the PR tab, the merge gate).
					core.PRObserved(t, core.PR{URL: pr, Repo: prRepo, Number: prN, State: "OPEN", Checks: core.ChecksPending, Branch: x.Branch}, now)
				}
				return x, nil
			}, &c)
			if err != nil {
				return "", err
			}
			if pr != "" && e.OnPR != nil {
				e.OnPR(th)
			}
			verb := "Opened"
			if !made {
				verb = "Found"
			}
			return fmt.Sprintf("%s change %s %q · %s. Fyr shows it in the thread's Changes tab. Report progress with change_status, new versions with change_revise, proof with change_attach, and ask the owner with change_request.", verb, c.ID, c.Title, phaseText(c)), nil
		}})

	s.Add(mcp.Tool{Name: "change_revise",
		Description: "Record a new version of a change: a new commit range, a new plan, a new document version, a new design export. Conditions about the old version go back to pending.",
		Schema:      obj(map[string]any{"change": str(changeRef), "revision": revisionSchema()}, "change", "revision"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			r := revisionArg(a["revision"])
			if r == nil {
				return "", errors.New("revision.id is required: a commit SHA, a plan digest or a version")
			}
			var c core.Change
			added := false
			err := e.changeObserve(th, "revise", func(t *core.Thread, now time.Time) (*core.Change, error) {
				x := core.FindChange(t, sArg(a, "change"))
				if x == nil {
					return nil, noChange(a)
				}
				added = core.ReviseChange(x, *r, now)
				return x, nil
			}, &c)
			if err != nil {
				return "", err
			}
			if !added {
				return fmt.Sprintf("Revision %s of %q was already recorded.", r.ID, c.Title), nil
			}
			return fmt.Sprintf("Recorded revision %s of %q (%d revisions).", r.ID, c.Title, len(c.Revisions)), nil
		}})

	s.Add(mcp.Tool{Name: "change_status",
		Description: "Report how far a change got, or a fact about it. Phases: " + core.PhaseGuide() + ". Conditions are independent facts with status " + core.OrList(core.CondStatuses()) + ", for example {type: \"apply\", status: \"fail\", reason: \"quota exceeded\"}; a failed one puts the thread in the owner's Needs you. needs: \"owner\" says the owner must look. You cannot change what Fyr observes (git, GitHub) and you cannot mark a review or an approval ok; use change_request.",
		Schema: obj(map[string]any{
			"change": str(changeRef),
			"phase":  map[string]any{"type": "string", "enum": core.Phases()},
			"label":  str("The kind's own word for where it is, for example \"planned\", \"applied to staging\"."),
			"conditions": map[string]any{"type": "array", "items": obj(map[string]any{
				"type":   str("What the fact is about: apply, health, migration, tests, …"),
				"status": map[string]any{"type": "string", "enum": core.CondStatuses()},
				"reason": str("One line, mainly for fail and pending."),
			}, "type", "status")},
			"needs": map[string]any{"type": "string", "enum": []string{"owner", ""}, "description": "\"owner\" when the owner must look; \"\" clears it."},
		}, "change"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			u := core.ChangeUpdate{Phase: core.Phase(sArg(a, "phase"))}
			if v, ok := a["label"].(string); ok {
				u.Label = &v
			}
			if v, ok := a["needs"].(string); ok {
				u.Needs = &v
			}
			if raw, ok := a["conditions"].([]any); ok {
				for _, x := range raw {
					m, _ := x.(map[string]any)
					u.Conditions = append(u.Conditions, core.Condition{Type: core.CondType(sArg(m, "type")), Status: core.CondStatus(sArg(m, "status")), Reason: sArg(m, "reason")})
				}
			}
			var c core.Change
			err := e.changeObserve(th, "status", func(t *core.Thread, now time.Time) (*core.Change, error) {
				x := core.FindChange(t, sArg(a, "change"))
				if x == nil {
					return nil, noChange(a)
				}
				return x, core.UpdateChange(t, x, u, core.ByAgent, now)
			}, &c)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%q · %s.", c.Title, phaseText(c)), nil
		}})

	s.Add(mcp.Tool{Name: "change_attach",
		Description: "Attach proof to a change: a plan output, a log, a deployment URL, a preview or a design export. For a file, call show first and pass its attachment id.",
		Schema: obj(map[string]any{
			"change": str(changeRef),
			"evidence": map[string]any{"type": "array", "items": obj(map[string]any{
				"kind":       str("plan, log, deployment, preview, doc, design, pr, other"),
				"url":        str("A link."),
				"attachment": str("A Fyr attachment id (from show)."),
				"title":      str("A short label."),
			}, "kind")},
		}, "change", "evidence"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			var evs []core.Evidence
			raw, _ := a["evidence"].([]any)
			for _, x := range raw {
				m, _ := x.(map[string]any)
				evs = append(evs, core.Evidence{Kind: sArg(m, "kind"), URL: sArg(m, "url"), Attachment: sArg(m, "attachment"), Title: sArg(m, "title")})
			}
			var c core.Change
			err := e.changeObserve(th, "attach", func(t *core.Thread, now time.Time) (*core.Change, error) {
				x := core.FindChange(t, sArg(a, "change"))
				if x == nil {
					return nil, noChange(a)
				}
				return x, core.AttachEvidence(x, evs, now)
			}, &c)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Attached %d to %q (%d in all).", len(evs), c.Title, len(c.Evidence)), nil
		}})

	s.Add(mcp.Tool{Name: "change_request",
		Description: "Ask the owner about a change. Returns at once; do not wait or poll. The owner answers in Fyr and the answer arrives as a later message starting with [Fyr]. ask: review (look at it), approve (accept it before you go on), merge (Fyr merges the change's PR; args {method: squash|merge|rebase}; use this instead of gh pr merge), apply (you apply it after the yes), deploy (Fyr deploys a Fyr checkout; args {dir}), revert. An approve or review that the owner accepts marks the change's approval ok.",
		Schema: obj(map[string]any{
			"change": str(changeRef),
			"ask":    map[string]any{"type": "string", "enum": core.Names(core.ChangeAsks())},
			"args":   map[string]any{"type": "object"},
			"reason": str("One sentence for the owner: why now."),
		}, "change", "ask"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			ask := core.ChangeAsk(sArg(a, "ask"))
			args, _ := a["args"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			if !slices.Contains(core.ChangeAsks(), ask) {
				return "", fmt.Errorf("ask must be one of %s", strings.Join(core.Names(core.ChangeAsks()), ", "))
			}
			t, ok := e.Thread(th)
			if !ok {
				return "", errors.New("no such thread")
			}
			c := core.FindChange(&t, sArg(a, "change"))
			if c == nil {
				return "", noChange(a)
			}
			if c.Phase.Ended() {
				return "", fmt.Errorf("%q is %s", c.Title, c.Phase)
			}
			action := "change." + string(ask)
			if fx := core.FyrEffectFor(ask); fx != nil { // merge, deploy: Fyr runs them
				action = fx.ID
				if _, ok := args["pr"].(string); !ok && fx.OnPR && c.PRURL() != "" {
					args["pr"] = c.PRURL()
				}
				if err := e.canRequest(th, fx, args); err != nil {
					return "", err
				}
			}
			args["change"] = c.ID
			if _, ok := args["title"]; !ok {
				args["title"] = c.Title
			}
			d := core.Decision{ID: NewID("d_"), Kind: core.DecisionEffect, Assignee: core.Owner,
				Effect: &core.EffectReq{Action: action, Args: args, Reason: sArg(a, "reason")}}
			e.Observe(th, func(t *core.Thread, now time.Time) []agent.Ev {
				d.CreatedAt = now
				core.DecisionOpened(t, d)
				core.ChangesChecked(t, now) // V0.62
				return []agent.Ev{{Type: "decision.opened", Data: map[string]any{"decision": d}}}
			})
			return fmt.Sprintf("Asked the owner to %s %q. The answer arrives in a later message starting with [Fyr]. Continue with other work or end your turn; do not do it another way meanwhile.", ask, c.Title), nil
		}})

	s.Add(mcp.Tool{Name: "change_list",
		Description: "List changes with what Fyr observed about them (phase, CI, review, conflicts, pushed, evidence). scope \"thread\" (default) is this thread's; \"project\" is every thread of this thread's project, so you see another agent's open change on the same target before you touch it. Read this instead of polling gh.",
		Schema: obj(map[string]any{
			"scope": map[string]any{"type": "string", "enum": []string{"thread", "project"}},
			"id":    str("One change, by id or key (this thread only)."),
		}),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			t, ok := e.Thread(th)
			if !ok {
				return "", errors.New("no such thread")
			}
			type row struct {
				Thread string `json:"thread,omitempty"`
				core.Change
			}
			var rows []row
			if id := sArg(a, "id"); id != "" {
				c := core.FindChange(&t, id)
				if c == nil {
					return "", noChange(map[string]any{"change": id})
				}
				rows = append(rows, row{Change: *c})
			} else if sArg(a, "scope") == "project" && t.ProjectID != "" {
				for _, d := range e.Snapshot() {
					if d.ProjectID != t.ProjectID || d.ArchivedAt != nil {
						continue
					}
					for _, c := range d.Changes {
						r := row{Change: c}
						if d.ID != th {
							r.Thread = d.ID + " " + d.Title
						}
						rows = append(rows, r)
					}
				}
			} else {
				for _, c := range t.Changes {
					rows = append(rows, row{Change: c})
				}
			}
			if len(rows) == 0 {
				return "No changes yet.", nil
			}
			b, _ := json.MarshalIndent(rows, "", " ")
			return string(b), nil
		}})
}

// changeObserve runs fn on the thread under the engine's lock, keeps a copy
// of the change it returns in out, and records it with a `change` event
// (V0.59 (5)) that the conversation shows as one line: one observation.
func (e *Engine) changeObserve(th, op string, fn func(t *core.Thread, now time.Time) (*core.Change, error), out *core.Change) error {
	if _, ok := e.Thread(th); !ok {
		return errors.New("no such thread")
	}
	var err error
	e.Observe(th, func(t *core.Thread, now time.Time) []agent.Ev {
		var c *core.Change
		if c, err = fn(t, now); err != nil {
			return nil
		}
		core.ChangesChecked(t, now) // V0.62
		*out = *c
		return []agent.Ev{{Type: "change", Data: map[string]any{"op": op, "change": map[string]any{"id": out.ID, "title": out.Title, "kind": out.Kind, "phase": out.Phase, "phaseBy": out.PhaseBy, "label": out.Label}}}}
	})
	return err
}

// changeEffect finishes a change_request ask that Fyr does not run itself
// (review, approve, apply, revert): the owner's answer is the result, and a
// yes to review or approve marks the change's approval ok (V0.59 (5)).
func (e *Engine) changeEffect(threadID string, req core.EffectReq, allowed bool) (bool, string) {
	ask := core.ChangeAsk(strings.TrimPrefix(req.Action, "change."))
	title, _ := req.Args["title"].(string)
	if !allowed {
		return false, fmt.Sprintf("The owner said no to %s %q.", ask, title)
	}
	if ask == core.AskReview || ask == core.AskApprove {
		id, _ := req.Args["change"].(string)
		e.Observe(threadID, func(t *core.Thread, now time.Time) []agent.Ev {
			if c := core.FindChange(t, id); c != nil {
				no := ""
				core.UpdateChange(t, c, core.ChangeUpdate{Conditions: []core.Condition{{Type: core.CondApproval, Status: core.CondOK, Reason: "the owner said yes"}}, Needs: &no}, core.ByOwner, now)
			}
			return nil
		})
	}
	return true, fmt.Sprintf("The owner said yes to %s %q.", ask, title)
}

func phaseText(c core.Change) string {
	s := string(c.Phase)
	if c.PhaseBy == core.ByAgent && c.Phase != core.PhaseWorking {
		s += " (reported)"
	}
	if c.Label != "" {
		s += ", " + c.Label
	}
	for _, k := range c.Conditions {
		if k.Status == core.CondFail {
			s += " · " + string(k.Type) + " failed"
			if k.Reason != "" {
				s += ": " + k.Reason
			}
		}
	}
	return s
}

func noChange(a map[string]any) error {
	return fmt.Errorf("no change %q in this thread; open it with change_open, or list them with change_list", sArg(a, "change"))
}

func sArg(a map[string]any, k string) string {
	v, _ := a[k].(string)
	return strings.TrimSpace(v)
}

func listArg(a map[string]any, k string) []string { return listOf[string](a, k) }

// listOf reads a list of strings into a typed list.
func listOf[T ~string](a map[string]any, k string) []T {
	raw, _ := a[k].([]any)
	var out []T
	for _, x := range raw {
		if s, ok := x.(string); ok {
			out = append(out, T(s))
		}
	}
	return out
}

func revisionSchema() map[string]any {
	return obj(map[string]any{
		"id":      str("A commit SHA, a plan digest (sha256:…), a document version."),
		"summary": str("One line: what this version changes."),
		"url":     str("A link to this version."),
	}, "id")
}

func revisionArg(v any) *core.Revision {
	m, _ := v.(map[string]any)
	if m == nil || sArg(m, "id") == "" {
		return nil
	}
	return &core.Revision{ID: sArg(m, "id"), Summary: sArg(m, "summary"), URL: sArg(m, "url"), By: core.ByAgent}
}
