package engine

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

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
			e.Observe(th, func(t *core.Thread, now time.Time) []agent.Ev {
				core.PRObserved(t, core.PR{URL: url, Repo: repo, Number: n, State: "OPEN", Checks: core.ChecksPending}, now)
				return []agent.Ev{{Type: "pr.linked", Data: map[string]any{"url": url}}}
			})
			return "Linked " + url + ". Fyr watches its checks and reviews.", nil
		}})

	s.Add(mcp.Tool{Name: "effect_request",
		Description: "Ask Fyr to perform an effect that needs the user's approval. Returns at once; do not wait or poll. The user approves or declines in Fyr, Fyr runs it, and the result arrives as a later message starting with [Fyr]. Actions: \"pr.merge\" with args {pr: <url>, method: squash|merge|rebase} (use this instead of gh pr merge); \"fyr.deploy\" with args {dir: <Fyr checkout>} builds, tests and switches the running Fyr to that checkout.",
		Schema: obj(map[string]any{
			"action": map[string]any{"type": "string", "enum": requestable()},
			"args":   map[string]any{"type": "object"},
			"reason": str("One sentence for the user: why now."),
		}, "action", "args"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			action, _ := a["action"].(string)
			args, _ := a["args"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			fx := core.FyrEffects[action]
			if fx == nil || !fx.Request {
				return "", fmt.Errorf("unknown action %q", action)
			}
			if err := e.canRequest(th, fx, args); err != nil {
				return "", err
			}
			reason, _ := a["reason"].(string)
			d := core.Decision{ID: NewID("d_"), Kind: core.DecisionEffect, Assignee: core.Owner,
				Effect: &core.EffectReq{Action: action, Args: args, Reason: reason}}
			e.Observe(th, func(t *core.Thread, now time.Time) []agent.Ev {
				d.CreatedAt = now
				core.DecisionOpened(t, d)
				return []agent.Ev{{Type: "decision.opened", Data: map[string]any{"decision": d}}}
			})
			return "Requested. The user decides in Fyr; the result arrives in a later message starting with [Fyr]. Continue with other work or end your turn; do not perform this action another way.", nil
		}})

	e.changeTools(s) // V0.59
	s.Add(e.showTool())
	e.viewTools(s)
	e.threadTools(s)

	s.Add(mcp.Tool{Name: "settle_when",
		Description: "Mark this Fyr thread to settle (leave the user's inbox) when a condition holds: \"done\" when your current run ends, \"merged\" when the linked PR merges, \"change\" when the change (id or key) reaches phase (landed is also reached by live). Conditions add up: the first that holds settles the thread, never while a run is live.",
		Schema: obj(map[string]any{
			"condition": map[string]any{"type": "string", "enum": []string{string(core.SettleDone), "merged", string(core.SettleChange)}},
			"change":    str("For condition change: the change's id or key."),
			"phase":     map[string]any{"type": "string", "enum": core.PhasesAfter(core.PhaseWorking)},
		}, "condition"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			cond, _ := a["condition"].(string)
			var err error
			e.Observe(th, func(t *core.Thread, now time.Time) []agent.Ev {
				switch cond {
				case string(core.SettleDone):
					core.ArmSettle(t, core.SettleCond{Kind: core.SettleDone}, now)
				case "merged":
					if !core.SettleWhenMerged(t, now) {
						err = errors.New("this thread has no open PR; link one with pr_link first")
						return nil
					}
				case string(core.SettleChange):
					c := core.FindChange(t, sArg(a, "change"))
					phase := core.Phase(sArg(a, "phase"))
					if phase == "" {
						phase = core.PhaseLanded
					}
					if c == nil {
						err = noChange(a)
						return nil
					}
					core.ArmSettle(t, core.SettleCond{Kind: core.SettleChange, Change: c.ID, Phase: phase}, now)
					cond = fmt.Sprintf("%q is %s", c.Title, phase)
				default:
					err = fmt.Errorf("unknown condition %q", cond)
				}
				return nil
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

// requestable are the Fyr effects an agent may request, sorted.
func requestable() []string {
	var out []string
	for id, fx := range core.FyrEffects {
		if fx.Request {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// canRequest checks an agent's request of a Fyr effect: its args, and the
// effect's guard, the same one the owner's command and press use (#31).
func (e *Engine) canRequest(th string, fx *core.FyrEffect, args map[string]any) error {
	if err := effectArgs(fx, args); err != nil {
		return err
	}
	t, ok := e.Thread(th)
	if !ok {
		return errors.New("no such thread")
	}
	if why := fx.Guard(&t, args); why != "" {
		return fmt.Errorf("Fyr cannot %s now: %s", strings.ToLower(fx.Label), why)
	}
	return nil
}
