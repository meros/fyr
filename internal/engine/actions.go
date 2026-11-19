package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
)

// Actions (SPEC 0.5 V0.60, #31; the model is in core/actionspec.go): the agent
// declares them on a change, the thread or its live view; the owner presses
// them (the Input action.run, core/kinds.go); Fyr runs them as effects
// (effects.go). Three ways: a Fyr effect, an exact script, or a prompt for an
// agent. Every Fyr effect runs through runFyr, whoever started it.

// ScriptRunner runs a script action; tests replace it.
type ScriptRunner func(ctx context.Context, dir string, argv []string) (out string, exit int, err error)

func (e *Engine) offerTool(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "offer_action",
		Description: OfferActionDesc,
		Schema: obj(map[string]any{
			"on":         map[string]any{"type": "string", "enum": []string{core.OnThread, core.OnView}, "description": "\"thread\": a chip under your newest message, gone when the next turn starts. \"view\": your live view runs it from an element with data-action=\"<id>\" or fyr.run(\"<id>\")."},
			"id":         str("A short id, unique where it shows, for example \"fix-test\"."),
			"label":      str("The chip's text, a verb first: \"Fix the failing test\"."),
			"run":        map[string]any{"type": "string", "enum": core.Names([]core.RunKind{core.RunAgent, core.RunScript}), "description": "agent (default): the press sends prompt. script: only on \"view\"; on the thread a command is run_request {optional: true}."},
			"prompt":     str("agent: what the agent does when the owner presses it."),
			"in":         map[string]any{"type": "string", "enum": []string{"this", "new"}, "description": "agent: this thread's agent (default) or a new thread in the same folder."},
			"argv":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "script, on the view only: the program and its arguments."},
			"cwd":        str("script, on the view only: the folder to run in."),
			"timeoutSec": map[string]any{"type": "integer", "description": "script, on the view only: the time limit, default 600, at most 3600."},
			"remove":     map[string]any{"type": "boolean"},
		}, "on", "id"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			remove, _ := a["remove"].(bool)
			if err := offerAllowed(a, remove); err != nil {
				return "", err
			}
			o := core.ActionSpec{ID: sArg(a, "id"), Label: sArg(a, "label"), Run: core.RunKind(sArg(a, "run")), Argv: listArg(a, "argv"), Cwd: sArg(a, "cwd"),
				Prompt: sArg(a, "prompt"), In: sArg(a, "in")}
			if o.Run == "" {
				o.Run = core.RunAgent
			}
			if n, ok := a["timeoutSec"].(float64); ok {
				o.TimeoutSec = int(n)
			}
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindActionOffer, Thread: th, Args: core.OfferArgs{On: sArg(a, "on"), Spec: o, Remove: remove}})
			if err != nil {
				return "", err
			}
			r := out.(core.OfferReply)
			if r.Removed {
				return fmt.Sprintf("Removed %q from %s.", o.ID, r.Where), nil
			}
			if r.Where == core.OnThread {
				return fmt.Sprintf("The owner sees %q as a chip under your newest message, until the next turn starts. When they press it, its prompt arrives as a message.", r.Label), nil
			}
			return fmt.Sprintf("The owner sees %q on %s in Fyr. When they press it, the result arrives as a message starting with [Fyr].", r.Label, r.Where), nil
		}})
}

// OfferActionDesc is offer_action's description (V0.next-step-chips).
const OfferActionDesc = "Offer the owner an optional next step: a chip under your newest message that sends a prompt when pressed, to you (in: \"this\") or to a new thread in the same folder (in: \"new\"). Use it for an obvious next move the owner may want (fix the failing test, write the release notes); it never puts the thread in the owner's Your move, and it goes when the next turn starts. For a command use run_request (with optional: true when nothing waits on it); when you are blocked on the owner use ask or run_request. on: \"view\" declares an action your live view runs (data-action=\"<id>\" or fyr.run(\"<id>\")), a prompt or a script. The same id again replaces it; remove: true removes it. Fyr adds Merge, Re-run failed checks, Fix CI, Address review and Rebase to PRs itself."

// offerAllowed refuses what offer_action no longer takes (V0.next-step-chips
// (5)), each with the line that names the way now.
func offerAllowed(a map[string]any, remove bool) error {
	on := sArg(a, "on")
	if on != core.OnThread && on != core.OnView {
		return errors.New(`on is "thread" or "view": an action on a change is Fyr's own (Merge, Fix CI, …); for a command use run_request`)
	}
	for _, k := range []string{"bind", "onOk", "when", "primary"} {
		if _, ok := a[k]; ok && !remove {
			return fmt.Errorf("offer_action no longer takes %s: a chip is a prompt; a command is run_request", k)
		}
	}
	if on == core.OnThread && sArg(a, "run") == string(core.RunScript) && !remove {
		return errors.New("a command on the thread is a Run card: call run_request {command, why, optional: true} instead of a script action")
	}
	return nil
}

// canonicalArgs puts a PR URL in the one form gh is given; the reducer
// checks the rest of the args.
func canonicalArgs(args map[string]any) error {
	if pr, ok := args["pr"].(string); ok && pr != "" {
		canon, ok := core.CanonicalPR(pr)
		if !ok {
			return errors.New("args.pr must be a GitHub pull request URL")
		}
		args["pr"] = canon
	}
	return nil
}
