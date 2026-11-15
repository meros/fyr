package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindChangeUnchanged, Thread: th})
			if err != nil {
				return "", err
			}
			if n, _ := out.(int); n == 0 {
				return "You have no open changes; nothing to keep current.", nil
			}
			return fmt.Sprintf("Noted: %d open changes still current (%s).", out, sArg(a, "reason")), nil
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
			if pr != "" {
				var ok bool
				if pr, ok = core.CanonicalPR(pr); !ok {
					return "", errors.New("pr must be a GitHub pull request URL")
				}
				_, prN, _ := core.ParsePR(pr)
				o.Evidence = append(o.Evidence, core.Evidence{Kind: "pr", URL: pr, Title: fmt.Sprintf("#%d", prN)})
			}
			r, err := e.changeInput(th, core.KindChangeOpen, core.ChangeOpenArgs{Open: o, PR: pr})
			if err != nil {
				return "", err
			}
			if pr != "" && e.OnPR != nil {
				e.OnPR(th)
			}
			c, made := r.Change, r.New
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
			out, err := e.changeInput(th, core.KindChangeRevise, core.ChangeReviseArgs{Change: sArg(a, "change"), Revision: *r})
			if err != nil {
				return "", err
			}
			c, added := out.Change, out.New
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
			out, err := e.changeInput(th, core.KindChangeStatus, core.ChangeStatusArgs{Change: sArg(a, "change"), Update: u})
			if err != nil {
				return "", err
			}
			c := out.Change
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
			out, err := e.changeInput(th, core.KindChangeAttach, core.ChangeAttachArgs{Change: sArg(a, "change"), Evidence: evs})
			if err != nil {
				return "", err
			}
			c := out.Change
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
			if err := canonicalArgs(args); err != nil {
				return "", err
			}
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindChangeRequest, Thread: th,
				Args: core.ChangeRequest{ID: NewID("d_"), Change: sArg(a, "change"), Ask: ask, Args: args, Reason: sArg(a, "reason")}})
			if err != nil {
				return "", err
			}
			c := out.(core.Change)
			return fmt.Sprintf("Asked the owner to %s %q. The answer arrives in a later message starting with [Fyr]. Continue with other work or end your turn; do not do it another way meanwhile.", ask, c.Title), nil
		}})

	s.Add(mcp.Tool{Name: "change_list",
		Description: "List changes with what Fyr observed about them (phase, CI, review, conflicts, pushed, evidence). scope \"thread\" (default) is this thread's; \"project\" is every thread of this thread's project, so you see another agent's open change on the same target before you touch it. Read this instead of polling gh.",
		Schema: obj(map[string]any{
			"scope": map[string]any{"type": "string", "enum": []string{"thread", "project"}},
			"id":    str("One change, by id or key (this thread only)."),
		}),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			t, ok := e.Shown(th)
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
				seen := map[string]bool{}
				for _, d := range e.Snapshot() {
					if d.ProjectID != t.ProjectID || d.ArchivedAt != nil {
						continue
					}
					for _, c := range d.Changes {
						if c.Threads != nil { // a host record: once (#35)
							if seen[c.ID] {
								continue
							}
							seen[c.ID] = true
						}
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

// changeInput sends a change tool's Input and returns the change it
// touched.
func (e *Engine) changeInput(th, kind string, args any) (core.ChangeReply, error) {
	out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: kind, Thread: th, Args: args})
	if err != nil {
		return core.ChangeReply{}, err
	}
	return out.(core.ChangeReply), nil
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

func noChange(a map[string]any) error { return core.NoChange(sArg(a, "change")) }

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
