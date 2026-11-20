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
	s.Add(mcp.Tool{Name: "pr_link", Hidden: true, // V0.101: change_open {pr} does this
		Description: alias("change_open {pr}", "Link a GitHub pull request to this Fyr thread. Call it when you open a PR other than with `gh pr create`, or to switch the thread to another PR."),
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
		Description: "Ask Fyr to perform an effect that needs the user's approval. Returns at once; do not wait or poll. The user approves or declines in Fyr, Fyr runs it, and the result arrives as a later message starting with [Fyr]. Actions: \"pr.merge\" with args {pr: <url>, method: squash|merge|rebase, head?: <commit SHA, default the head Fyr reads now>} (use this instead of gh pr merge; Fyr merges only that head, and shows the ask to the user only when CI is green on it); \"fyr.deploy\": Fyr updates itself when CI publishes a green commit on main (the tag deployable), so do not ask after a merge. With args {} it installs the newest CI release now (approved by itself unless the owner turned auto-update off). With args {dir: <Fyr checkout>, tests?: true} it builds that checkout locally and switches to it to try it; that always waits for the owner's press and pauses auto-update until a release contains it.",
		Schema: obj(map[string]any{
			"action": map[string]any{"type": "string", "enum": requestable()},
			"args":   map[string]any{"type": "object"},
			"reason": str("One sentence for the user: why now."),
		}, "action", "args"),
		Call: func(ctx context.Context, th string, a map[string]any) (string, error) {
			action, _ := a["action"].(string)
			args, _ := a["args"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			if err := canonicalArgs(args); err != nil {
				return "", err
			}
			if action == core.FyrMerge {
				e.readPRForMergeAsk(ctx, th)
			}
			reason, _ := a["reason"].(string)
			id := NewID("d_")
			if _, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindEffectRequest, Thread: th,
				Args: core.EffectRequest{ID: id, Action: action, Args: args, Reason: reason}}); err != nil {
				return "", err
			}
			if s := e.heldReply(th, id); s != "" { // V0.136: a merge ask waits for CI
				return s, nil
			}
			return "Requested. The user decides in Fyr; the result arrives in a later message starting with [Fyr]. Continue with other work or end your turn; do not perform this action another way.", nil
		}})

	e.changeTools(s) // V0.59
	s.Add(e.showTool())
	e.viewTools(s)
	e.threadTools(s)
	e.setupTools(s)     // V0.116
	e.hostSetupTools(s) // V0.208

	s.Add(mcp.Tool{Name: "settle_when",
		Description: "Mark this Fyr thread to settle (leave the user's inbox) when a condition holds: \"done\" when the work is done (no run or task is live, nothing waits for the owner, and no PR is open), \"merged\" when the linked PR merges, \"change\" when the change (id or key) reaches phase (landed is also reached by live). Conditions add up: the first that holds settles the thread, never while a run is live.",
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
			return "The thread settles when " + cond + "." + e.pinNote(th), nil
		}})

	s.Add(e.settleRequestTool()) // V0.settle-request

	s.Add(e.watchTool())
	s.Add(e.stateTool())
	s.Add(e.askTool()) // V0.ask-tool
	e.slotTools(s)     // V0.86
	e.leftoverTools(s) // V0.176
}

// pinNote tells the agent that its settle waits: the owner pinned the
// thread, and only the owner settles a pinned thread (V0.106).
func (e *Engine) pinNote(th string) string {
	if t, ok := e.Thread(th); ok && t.PinnedAt != nil {
		return " The owner pinned this thread: it does not settle while pinned, and unpin drops this condition."
	}
	return ""
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
			note := ""
			if sArg(a, "then") == "settle" {
				note = e.pinNote(th)
			}
			return "Armed the watch " + r.ID + " (then " + sArg(a, "then") + "). Remove it with watch {remove: \"" + r.ID + "\"}." + note, nil
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

// SettleRequestDesc is settle_request's description (V0.settle-request).
const SettleRequestDesc = "Tell Fyr this thread's work is done, so it can leave the owner's inbox. Call it at the end of a turn when the task is clearly finished and nothing is left behind: every ask answered, no run_request or merge waiting, no PR open that still needs you, nothing the owner said they want next. Do not call it while you wait on CI, a review, the owner, or a job of your own, and not when you just asked the owner something. The project decides what happens: either Fyr settles the thread itself once the run ends and nothing waits, or the owner sees a Settle chip with your why. A new message or run clears it, so call it again when a later turn finishes the work."

// settleRequestTool is settle_request (V0.settle-request): the project's
// AgentSettle decides whether Fyr settles or the owner does.
func (e *Engine) settleRequestTool() mcp.Tool {
	return mcp.Tool{Name: "settle_request",
		Description: SettleRequestDesc,
		Schema:      obj(map[string]any{"why": str("A few words on why the work is done, not what the thread was about (the owner already sees its title): \"PR #12 merged\", \"question answered\", \"pushed to main\". The chip reads \"Settle: <why>\".")}, "why"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindSettleRequest, Thread: th,
				Args: core.Args{"why": sArg(a, "why"), "auto": e.settlesOnRequest(th)}})
			if err != nil {
				return "", err
			}
			if out == core.SettleAskAuto {
				return "The thread settles once this run ends, nothing waits for the owner and no PR is open." + e.pinNote(th), nil
			}
			return "The owner sees a Settle chip with your why; they settle the thread. Do not call it again in this turn.", nil
		}}
}

// settlesOnRequest reports that the thread's project settles on the agent's
// request (V0.settle-request).
func (e *Engine) settlesOnRequest(th string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.threads[th]
	return t != nil && e.projects[t.ProjectID].SettlesOnRequest()
}
