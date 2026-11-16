package engine

import (
	"context"
	"encoding/json"
	"errors"
	"slices"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// Tools registers the agent tools of SPEC 0.5 V0.12 on an MCP server.
func (e *Engine) Tools(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "pr_link",
		Description: "Link a GitHub pull request to this Fyr thread. Call it when you open a PR other than with `gh pr create`, or to switch the thread to another PR.",
		Schema:      obj(map[string]any{"url": str("The PR URL, https://github.com/<owner>/<repo>/pull/<n>")}, "url"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			url, ok := core.CanonicalPR(sArg(a, "url"))
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
			"condition": map[string]any{"type": "string", "enum": []string{"done", "merged", "change"}},
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

	s.Add(e.watchTool())
	s.Add(e.stateTool())
	e.slotTools(s) // V0.86
}

// stateTool reads the current state of the thread's records (#47): the
// notices that start with [Fyr] are one sentence each and point here.
func (e *Engine) stateTool() mcp.Tool {
	return mcp.Tool{Name: "fyr_state",
		Description: "Read the current state of this thread's records in Fyr in one compact form: the thread and its settle state, the changes with their PR, git and deploy conditions and actions, the live view, and the rules (your watches among them). Fyr's [Fyr] notices are one sentence each; read the details here instead of polling gh.",
		Schema:      obj(map[string]any{"about": str("Optional: thread, changes, view, rules, settle, or one change's id or key. Empty: everything.")}),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			t, ok := e.Shown(th)
			if !ok {
				return "", errors.New("no such thread")
			}
			return core.StateText(&t, sArg(a, "about"), e.Now())
		}}
}

// watchTool is the agent's own Rules (SPEC 7, #46): arm a watch over Fyr's
// records that notifies the agent or settles the thread, or remove one.
func (e *Engine) watchTool() mcp.Tool {
	cond := map[string]any{"type": "object", "description": `A condition over Fyr's records (never the network). One of: {"kind":"runEnded"} (your current run ends); {"kind":"changeReached","change":<id or key>,"phase":<phase>}; {"kind":"condition","change":<id or key>,"type":"checks"|"review"|"conflict"|"deploy"|...,"status":"ok"|"fail"|"pending"}; {"kind":"at","at":<RFC 3339 time>}; {"kind":"idleFor","ms":<n>} (nothing happened on the thread for n ms); or {"any":[...]}, {"all":[...]}, {"not":{...}}.`}
	return mcp.Tool{Name: "watch",
		Description: "Watch something in Fyr and act once when it becomes true: then \"notify\" tells you (for example when CI on a change passes), then \"settle\" settles the thread (never while a run is live). It fires once, when the condition goes from false to true, and is then removed. The owner sees your watches on the thread. Remove one with {remove: <id>}.",
		Schema: obj(map[string]any{
			"when":   cond,
			"then":   map[string]any{"type": "string", "enum": []string{"notify", "settle"}},
			"note":   str("For notify: one sentence for yourself, what to do when it fires."),
			"remove": str("The id of a watch of yours to remove; nothing else is needed."),
		}),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			if id := sArg(a, "remove"); id != "" {
				if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindRuleRemove, Thread: th, Args: id}); err != nil {
					return "", err
				}
				return "Removed the watch " + id + ".", nil
			}
			var w core.When
			b, _ := json.Marshal(a["when"])
			if err := json.Unmarshal(b, &w); err != nil || a["when"] == nil {
				return "", errors.New("when must be a condition object")
			}
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindRuleArm, Thread: th,
				Args: core.RuleArmArgs{ID: NewID("w_"), When: w, Then: sArg(a, "then"), Note: sArg(a, "note")}})
			if err != nil {
				return "", err
			}
			r, _ := out.(core.Rule)
			return "Armed the watch " + r.ID + " (then " + sArg(a, "then") + "). Remove it with watch {remove: \"" + r.ID + "\"}.", nil
		}}
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
