package engine

import (
	"context"
	"errors"
	"slices"

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
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindPRLink, Thread: th, Args: url}); err != nil {
				return "", err
			}
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
			if err := canonicalArgs(args); err != nil {
				return "", err
			}
			reason, _ := a["reason"].(string)
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindEffectRequest, Thread: th,
				Args: core.EffectRequest{ID: NewID("d_"), Action: action, Args: args, Reason: reason}}); err != nil {
				return "", err
			}
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
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindSettleWhen, Thread: th, Args: core.Args(a)})
			if err != nil {
				return "", err
			}
			cond, _ := out.(string)
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

