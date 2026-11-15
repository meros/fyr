package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// EffectActions are the actions an agent may request with effect_request.
var EffectActions = map[string]func(args map[string]any) error{
	"pr.merge": func(a map[string]any) error {
		pr, _ := a["pr"].(string)
		canon, ok := agent.CanonicalPR(pr)
		if !ok {
			return errors.New("args.pr must be a GitHub pull request URL")
		}
		a["pr"] = canon
		switch m, _ := a["method"].(string); m {
		case "", "squash", "merge", "rebase":
			return nil
		default:
			return fmt.Errorf("args.method %q must be squash, merge or rebase", m)
		}
	},
	"fyr.deploy": func(a map[string]any) error {
		if d, _ := a["dir"].(string); d == "" {
			return errors.New("args.dir must be the Fyr checkout to build")
		}
		return nil
	},
}

// Tools registers the agent tools of SPEC 0.5 V0.12 on an MCP server.
func (e *Engine) Tools(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "pr_link",
		Description: "Link a GitHub pull request to this Fyr thread. Call it when you open a PR other than with `gh pr create`, or to switch the thread to another PR.",
		Schema:      obj(map[string]any{"url": str("The PR URL, https://github.com/<owner>/<repo>/pull/<n>")}, "url"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			url, ok := agent.CanonicalPR(sArg(a, "url"))
			if !ok {
				return "", errors.New("not a GitHub pull request URL")
			}
			repo, n, _ := agent.ParsePR(url)
			e.Observe(th, func(t *core.Thread, now time.Time) {
				core.PRObserved(t, core.PR{URL: url, Repo: repo, Number: n, State: "OPEN", Checks: "pending"}, now)
			}, agent.Ev{Type: "pr.linked", Data: map[string]any{"url": url}})
			return "Linked " + url + ". Fyr watches its checks and reviews.", nil
		}})

	s.Add(mcp.Tool{Name: "effect_request",
		Description: "Ask Fyr to perform an effect that needs the user's approval. Returns at once; do not wait or poll. The user approves or declines in Fyr, Fyr runs it, and the result arrives as a later message starting with [Fyr]. Actions: \"pr.merge\" with args {pr: <url>, method: squash|merge|rebase} (use this instead of gh pr merge); \"fyr.deploy\" with args {dir: <Fyr checkout>} builds, tests and switches the running Fyr to that checkout.",
		Schema: obj(map[string]any{
			"action": map[string]any{"type": "string", "enum": []string{"pr.merge", "fyr.deploy"}},
			"args":   map[string]any{"type": "object"},
			"reason": str("One sentence for the user: why now."),
		}, "action", "args"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			action, _ := a["action"].(string)
			args, _ := a["args"].(map[string]any)
			check := EffectActions[action]
			if check == nil {
				return "", fmt.Errorf("unknown action %q", action)
			}
			if err := check(args); err != nil {
				return "", err
			}
			reason, _ := a["reason"].(string)
			d := core.Decision{ID: NewID("d_"), Kind: "effect", Assignee: core.Owner,
				Effect: &core.EffectReq{Action: action, Args: args, Reason: reason}}
			e.Observe(th, func(t *core.Thread, now time.Time) {
				d.CreatedAt = now
				core.DecisionOpened(t, d)
			}, agent.Ev{Type: "decision.opened", Data: map[string]any{"decision": d}})
			return "Requested. The user decides in Fyr; the result arrives in a later message starting with [Fyr]. Continue with other work or end your turn; do not perform this action another way.", nil
		}})

	e.changeTools(s) // V0.59
	s.Add(e.showTool())
	e.viewTools(s)
	e.threadTools(s)

	s.Add(mcp.Tool{Name: "settle_when",
		Description: "Mark this Fyr thread to settle (leave the user's inbox) when a condition holds: \"done\" when your current run ends, \"merged\" when the linked PR merges, \"change\" when the change (id or key) reaches phase (landed is also reached by live).",
		Schema: obj(map[string]any{
			"condition": map[string]any{"type": "string", "enum": []string{"done", "merged", "change"}},
			"change":    str("For condition change: the change's id or key."),
			"phase":     map[string]any{"type": "string", "enum": []string{core.PhaseReview, core.PhaseLanded, core.PhaseLive}},
		}, "condition"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			cond, _ := a["condition"].(string)
			var err error
			e.Observe(th, func(t *core.Thread, now time.Time) {
				switch cond {
				case "done":
					t.SettleWhenDone = true
				case "merged":
					if t.PR == nil || t.PR.State != "OPEN" {
						err = errors.New("this thread has no open PR; link one with pr_link first")
						return
					}
					t.SettleWhenMerged = true
				case "change":
					c := core.FindChange(t, sArg(a, "change"))
					phase := sArg(a, "phase")
					if phase == "" {
						phase = core.PhaseLanded
					}
					if c == nil {
						err = noChange(a)
						return
					}
					t.SettleWhen = &core.SettleWhen{Change: c.ID, Phase: phase}
					cond = fmt.Sprintf("%q is %s", c.Title, phase)
					core.CheckSettleChange(t, now)
				default:
					err = fmt.Errorf("unknown condition %q", cond)
				}
			})
			if err != nil {
				return "", err
			}
			return "The thread settles when " + cond + ".", nil
		}})
}

// obj builds an object schema. "required" is left out when empty: null is
// not valid JSON Schema, and the CLI drops every tool of a server when one
// schema is invalid.
func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
