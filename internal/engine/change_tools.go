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

const changeRef = "The change: its id (c_…), its key, its PR URL, owner/repo#N, or #N when one change of this thread has that PR."

// The tool descriptions of decision 0024 section 7 (SPEC V0.101).
const (
	ChangeOpenDesc    = "Give Fyr a card for work it cannot see: a cluster, cloud, DNS or bucket change, a migration or data fix, a NixOS rebuild, another machine, a document, a design. Call it before the first write. Fyr makes cards for your pushes and PRs itself. The same key, PR or branch returns the same change."
	ChangeUpdateDesc  = "Tell Fyr what only you know. Send only what changed; send nothing when nothing moved. Fyr reads PR state, checks, reviews, conflicts and the pushed head itself; if you send one of these, Fyr replies with the value it reads. abandoned takes the card off the board."
	ChangeRequestDesc = "Ask the owner. It returns at once; the answer comes later as a [Fyr] line. merge: Fyr merges the head the owner saw; never run gh pr merge. approve on a document or design finishes it."
	ChangeListDesc    = "Fyr's facts, one line per change, open changes only by default, at most 4,000 characters. id gives one change of this thread in full."
	// UnchangedText is the reply of change_unchanged, a no-op for one
	// release (V0.101).
	UnchangedText = "Nothing to do: Fyr reads your PRs and pushes itself. Report with change_update only when you learn what Fyr cannot read."
)

// alias marks a tool kept for one release (V0.101): hidden from the list,
// still callable.
func alias(of, desc string) string { return "Alias of " + of + " for one release. " + desc }

func (e *Engine) changeTools(s *mcp.Server) {
	e.offerTool(s) // V0.60
	s.Add(mcp.Tool{Name: "change_unchanged", Hidden: true,
		Description: alias("nothing", "Fyr keeps your changes current itself; this does nothing."),
		Schema:      obj(map[string]any{"reason": str("Ignored.")}),
		Call: func(context.Context, string, map[string]any) (string, error) {
			return UnchangedText, nil
		}})
	s.Add(mcp.Tool{Name: "change_update",
		Description: ChangeUpdateDesc,
		Schema: obj(map[string]any{
			"change": str(changeRef),
			"phase":  map[string]any{"type": "string", "enum": core.Phases()},
			"label":  str("The kind's own word for where it is, for example \"planned\", \"applied to staging\"."),
			"conditions": map[string]any{"type": "array", "items": obj(map[string]any{
				"type":   str("What the fact is about: apply, health, migration, tests, …"),
				"status": map[string]any{"type": "string", "enum": core.CondStatuses()},
				"reason": str("One line, mainly for fail and pending."),
			}, "type", "status")},
			"revision": revisionSchema(),
			"evidence": evidenceSchema(),
		}, "change"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			out, err := e.changeInput(th, core.KindChangeUpdate, core.ChangeUpdateArgs{Change: sArg(a, "change"), Update: updateArg(a), Revision: revisionArg(a["revision"]), Evidence: evidenceArg(a)})
			if err != nil {
				return "", err
			}
			return updateReply(out), nil
		}})
	s.Add(mcp.Tool{Name: "change_open",
		Description: ChangeOpenDesc,
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
		}, "kind", "title"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			o := core.ChangeOpen{Key: sArg(a, "key"), Kind: core.ChangeKind(sArg(a, "kind")), Title: sArg(a, "title"), Branch: sArg(a, "branch"), Group: sArg(a, "group"),
				Targets: listArg(a, "targets"), After: listArg(a, "after"), Revision: revisionArg(a["revision"])}
			pr := sArg(a, "pr")
			if pr != "" {
				var ok bool
				if pr, ok = core.CanonicalPR(pr); !ok {
					return "", errors.New("pr must be a GitHub pull request URL")
				}
				prRepo, prN, _ := core.ParsePR(pr)
				o.Evidence = append(o.Evidence, core.Evidence{Kind: "pr", URL: pr, Title: fmt.Sprintf("#%d", prN)})
				if o.Key == "" { // V0.101: the PR names the change
					o.Key = core.PRKey(prRepo, prN)
				}
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
			return fmt.Sprintf("%s change %s %q · %s. Fyr shows it in the thread's Changes tab. Report what only you know with change_update, and ask the owner with change_request.", verb, c.ID, c.Title, phaseText(c)), nil
		}})

	s.Add(mcp.Tool{Name: "change_revise", Hidden: true,
		Description: alias("change_update", "Record a new version of a change: a new commit range, a new plan, a new document version, a new design export. Conditions about the old version go back to pending."),
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

	s.Add(mcp.Tool{Name: "change_status", Hidden: true,
		Description: alias("change_update", "Report how far a change got, or a fact about it. Phases: "+core.PhaseGuide()+"."),
		Schema: obj(map[string]any{
			"change": str(changeRef),
			"phase":  map[string]any{"type": "string", "enum": core.Phases()},
			"label":  str("The kind's own word for where it is."),
			"conditions": map[string]any{"type": "array", "items": obj(map[string]any{
				"type":   str("What the fact is about: apply, health, migration, tests, …"),
				"status": map[string]any{"type": "string", "enum": core.CondStatuses()},
				"reason": str("One line, mainly for fail and pending."),
			}, "type", "status")},
		}, "change"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			out, err := e.changeInput(th, core.KindChangeStatus, core.ChangeStatusArgs{Change: sArg(a, "change"), Update: updateArg(a)})
			if err != nil {
				return "", err
			}
			return updateReply(out), nil
		}})

	s.Add(mcp.Tool{Name: "change_attach", Hidden: true,
		Description: alias("change_update", "Attach proof to a change: a plan output, a log, a deployment URL, a preview or a design export. For a file, call show first and pass its attachment id."),
		Schema: obj(map[string]any{
			"change":   str(changeRef),
			"evidence": evidenceSchema(),
		}, "change", "evidence"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			evs := evidenceArg(a)
			out, err := e.changeInput(th, core.KindChangeAttach, core.ChangeAttachArgs{Change: sArg(a, "change"), Evidence: evs})
			if err != nil {
				return "", err
			}
			c := out.Change
			return fmt.Sprintf("Attached %d to %q (%d in all).", len(evs), c.Title, len(c.Evidence)), nil
		}})

	s.Add(mcp.Tool{Name: "change_request",
		Description: ChangeRequestDesc,
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
		Description: ChangeListDesc,
		Schema: obj(map[string]any{
			"scope": map[string]any{"type": "string", "enum": []string{"thread", "project"}},
			"id":    str("One change, by id or key (this thread only), in full."),
			"done":  map[string]any{"type": "boolean", "description": "Also list done changes: landed, live, abandoned or reverted."},
		}),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			t, ok := e.Shown(th)
			if !ok {
				return "", errors.New("no such thread")
			}
			type row struct {
				Thread string `json:"thread,omitempty"`
				core.Change
				// V0.84: the owner parked it (Not now).
				Note string `json:"note,omitempty"`
			}
			if id := sArg(a, "id"); id != "" {
				c := core.FindChange(&t, id)
				if c == nil {
					return "", noChange(map[string]any{"change": id})
				}
				rows := []row{{Change: *c, Note: core.ParkedText(c)}}
				b, _ := json.MarshalIndent(rows, "", " ")
				return string(b), nil
			}
			var rows []row
			if sArg(a, "scope") == "project" && t.ProjectID != "" {
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
			// SPEC V0.96 (2): open changes, one line each, within 4,000
			// characters.
			all, _ := a["done"].(bool)
			var lines []string
			hidden := 0
			for i := range rows {
				c := &rows[i].Change
				if !all && !core.ListedOpen(c) {
					hidden++
					continue
				}
				l := core.ChangeLine(c)
				if rows[i].Thread != "" {
					l += " · thread " + core.TruncHead(rows[i].Thread, 60)
				}
				lines = append(lines, l)
			}
			head := fmt.Sprintf("%d open", len(lines))
			if all {
				head = fmt.Sprintf("%d changes", len(lines))
			} else if hidden > 0 {
				head += fmt.Sprintf("; %d done not shown (done: true lists them)", hidden)
			}
			return core.CapLines(head+".", lines), nil
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

func evidenceSchema() map[string]any {
	return map[string]any{"type": "array", "items": obj(map[string]any{
		"kind":       str("plan, log, deployment, preview, doc, design, pr, other"),
		"url":        str("A link."),
		"attachment": str("A Fyr attachment id (from show)."),
		"title":      str("A short label."),
	}, "kind")}
}

func evidenceArg(a map[string]any) []core.Evidence {
	var evs []core.Evidence
	raw, _ := a["evidence"].([]any)
	for _, x := range raw {
		m, _ := x.(map[string]any)
		evs = append(evs, core.Evidence{Kind: sArg(m, "kind"), URL: sArg(m, "url"), Attachment: sArg(m, "attachment"), Title: sArg(m, "title")})
	}
	return evs
}

// updateArg reads the report of change_update and change_status.
func updateArg(a map[string]any) core.ChangeUpdate {
	u := core.ChangeUpdate{Phase: core.Phase(sArg(a, "phase"))}
	if v, ok := a["label"].(string); ok {
		u.Label = &v
	}
	if raw, ok := a["conditions"].([]any); ok {
		for _, x := range raw {
			m, _ := x.(map[string]any)
			u.Conditions = append(u.Conditions, core.Condition{Type: core.CondType(sArg(m, "type")), Status: core.CondStatus(sArg(m, "status")), Reason: sArg(m, "reason")})
		}
	}
	return u
}

// updateReply is the change and what Fyr reads instead of an observed
// value the agent sent (V0.101).
func updateReply(r core.ChangeReply) string {
	out := fmt.Sprintf("%q · %s.", r.Change.Title, phaseText(r.Change))
	if len(r.Notes) > 0 {
		out += " " + strings.Join(r.Notes, " ")
	}
	return out
}

func revisionArg(v any) *core.Revision {
	m, _ := v.(map[string]any)
	if m == nil || sArg(m, "id") == "" {
		return nil
	}
	return &core.Revision{ID: sArg(m, "id"), Summary: sArg(m, "summary"), URL: sArg(m, "url"), By: core.ByAgent}
}
