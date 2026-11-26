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

// Tools registers the agent tools of SPEC 0.5 V0.12 on an MCP server.
func (e *Engine) Tools(s *mcp.Server) {
	e.changeTools(s) // V0.59
	s.Add(e.showTool())
	e.viewTools(s)
	e.threadTools(s)
	e.setupTools(s)     // V0.116
	e.playbookTools(s)  // V0.project-playbook
	e.hostSetupTools(s) // V0.208

	s.Add(e.settleRequestTool()) // V0.settle-request

	s.Add(e.watchTool())
	s.Add(e.stateTool())
	s.Add(e.askTool()) // V0.ask-tool
	e.leftoverTools(s) // V0.176
	for name, instead := range Retired {
		s.Retire(name, instead)
	}
}

// Retired are the tools V0.one-tool-vocabulary removed, each with the line
// that names its replacement: an agent that read the old list hears it when
// it calls one.
var Retired = map[string]string{
	"effect_request": `ask with change_request instead: {change: <the PR URL or the change>, ask: "merge", args?: {method, head}} to merge, or {ask: "fyr.deploy", args: {dir?, tests?}} to deploy Fyr from a thread on Fyr's own repo.`,
	"settle_when":    `arm watch with then "settle" instead: {when: {kind: "workDone"}} for done, {when: {kind: "changeReached", change, phase: "landed"}} for merged or a change's phase. When your work is done, call settle_request.`,
	"slot_acquire":   "nothing to call: Fyr admits a heavy command by itself when you run it.",
	"slot_release":   "nothing to call: an admission ends by itself when the work stops.",
	"pr_link":        "call change_open {pr} instead.",
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
	cond := map[string]any{"type": "object", "description": `A condition over Fyr's records (never the network). One of: {"kind":"runEnded"} (your current run ends); {"kind":"changeReached","change":<id, key or PR URL>,"phase":<phase>} (landed is a merge; live is also landed); {"kind":"condition","change":<id or key>,"type":"checks"|"review"|"conflict"|"deploy"|...,"status":"ok"|"fail"|"pending"} (review: fail on a requested change or on any unresolved review thread, whoever left it and also when the review only commented; ok when approved with no thread open; pending while a required review waits; otherwise no review condition, so an ok watch never fires on a PR nobody must approve); {"kind":"at","at":<RFC 3339 time>}; {"kind":"quota","provider":<profile id, optional (default this thread's)>,"window":<"5 h"|"7 d"|"7 d Opus"|..., a window the provider reports, optional>} (when that provider's quota window resets; refused when no fresh reset time is known); {"kind":"idleFor","ms":<n>} (nothing happened on the thread for n ms); {"kind":"workDone"} (only alone, with then settle: no run or task is live, nothing waits for the owner, no PR is open); or {"any":[...]}, {"all":[...]}, {"not":{...}}.`}
	return mcp.Tool{Name: "watch",
		Description: "Watch something in Fyr and act once when it becomes true: then \"notify\" tells you (for example when CI on a change passes or a provider's quota window resets), then \"settle\" settles the thread (never while a run is live), for example when the owner says the thread is finished once a PR merges. It fires once, when the condition goes from false to true, and is then removed. The same watch armed again returns the one already armed. The owner sees your watches on the thread. Remove one with {remove: <id>}.",
		Schema: obj(map[string]any{
			"when":   cond,
			"then":   map[string]any{"type": "string", "enum": []string{"notify", "settle"}},
			"note":   str("For notify: one sentence for yourself, what to do when it fires."),
			"remove": str("The id of a watch of yours to remove; nothing else is needed."),
		}),
		Call: func(ctx context.Context, th string, a map[string]any) (string, error) {
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
			if err := e.resolveQuotaWhen(ctx, th, &w); err != nil {
				return "", err
			}
			id := NewID("w_")
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindRuleArm, Thread: th,
				Args: core.RuleArmArgs{ID: id, When: w, Then: sArg(a, "then"), Note: sArg(a, "note")}})
			if err != nil {
				return "", err
			}
			r, _ := out.(core.Rule)
			note := ""
			if sArg(a, "then") == "settle" {
				note = e.pinNote(th)
			}
			verb := "Armed"
			if r.ID != id && strings.HasPrefix(r.ID, "w_") { // V0.one-tool-vocabulary: a retry
				verb = "Already armed:"
			}
			return verb + " the watch " + r.ID + " (then " + sArg(a, "then") + "). Remove it with watch {remove: \"" + r.ID + "\"}." + note, nil
		}}
}

// resolveQuotaWhen resolves every WhenQuota node in w against the host's quota
// service (without e.mu), filling Provider, Window and At or refusing with the
// reason (V0.quota-reset-wake).
func (e *Engine) resolveQuotaWhen(ctx context.Context, th string, w *core.When) error {
	for i := range w.All {
		if err := e.resolveQuotaWhen(ctx, th, &w.All[i]); err != nil {
			return err
		}
	}
	for i := range w.Any {
		if err := e.resolveQuotaWhen(ctx, th, &w.Any[i]); err != nil {
			return err
		}
	}
	if w.Not != nil {
		if err := e.resolveQuotaWhen(ctx, th, w.Not); err != nil {
			return err
		}
	}
	if w.Kind != core.WhenQuota {
		return nil
	}
	prov, win, at, err := e.resolveQuotaReset(ctx, th, w.Provider, w.Window)
	if err != nil {
		if errors.Is(err, core.ErrBadArgs) {
			return errors.New("quota needs a provider")
		}
		return fmt.Errorf("cannot arm quota watch: %w", err)
	}
	w.Provider, w.Window, w.At = prov, win, &at
	return nil
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
