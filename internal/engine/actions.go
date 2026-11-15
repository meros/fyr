package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/proc"
)

// Actions (SPEC 0.5 V0.60, #31; the model is in core/actionspec.go): the agent
// declares them on a change, the thread or its live view; the owner presses
// them (the Input action.run, core/kinds.go); Fyr runs them as effects
// (effects.go). Three ways: a Fyr effect, an exact script, or a prompt for an
// agent. Every Fyr effect runs through runFyr, whoever started it.

// ScriptRunner runs a script action; tests replace it.
type ScriptRunner func(ctx context.Context, dir string, argv []string) (out string, exit int, err error)

func runScript(ctx context.Context, dir string, argv []string) (string, int, error) {
	cmd := proc.Command(ctx, argv[0], argv[1:]...) // #39: the limit stops what the script started too
	cmd.Dir = dir
	var b bytes.Buffer
	cmd.Stdout, cmd.Stderr = &b, &b
	err := cmd.Run()
	exit := 0
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		exit = ee.ExitCode()
	} else if err != nil {
		exit = -1
		fmt.Fprintf(&b, "\n%v", err)
	}
	if ctx.Err() != nil {
		fmt.Fprintf(&b, "\nstopped after the time limit")
	}
	return b.String(), exit, err
}

func (e *Engine) offerTool(s *mcp.Server) {
	s.Add(mcp.Tool{Name: "offer_action",
		Description: "Offer the owner an action: one button they press in Fyr. Fyr never runs it without the press. on: a change (its id or key; the button shows on the change), \"thread\" (in the thread header) or \"view\" (your live view runs it from an element with data-action=\"<id>\" or fyr.run(\"<id>\"); it goes away with the view). run \"script\": Fyr runs argv exactly (no shell) in cwd (default this thread's folder) and records the output; on a change, bind: true ties it to the newest revision like a saved plan, and onOk moves the change to that phase when it succeeds; a failed script on a change puts the thread in Needs you. run \"agent\": the press sends prompt to this thread's agent (in: \"this\") or starts a new thread with it in the same folder (in: \"new\"). Use script for exact, repeatable steps (apply a plan, run a migration, roll back, run the e2e suite) and agent for steps that need judgement (fix the failing test, write the release notes). The same id again replaces it; remove: true removes it. Fyr adds Merge, Re-run failed checks, Fix CI, Address review and Rebase to PRs itself.",
		Schema: obj(map[string]any{
			"on":         str("Where the button shows: a change's id or key, \"thread\" or \"view\"."),
			"id":         str("A short id, unique where it shows, for example \"apply\" or \"rollback\"."),
			"label":      str("The button text, a verb first: \"Apply plan to prod\"."),
			"run":        map[string]any{"type": "string", "enum": core.Names([]core.RunKind{core.RunScript, core.RunAgent})},
			"argv":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "script: the program and its arguments, for example [\"terraform\", \"apply\", \"plan.out\"]. For a pipeline use [\"bash\", \"-c\", \"…\"]."},
			"cwd":        str("script: the folder to run in."),
			"timeoutSec": map[string]any{"type": "integer", "description": "script: the time limit, default 600, at most 3600."},
			"bind":       map[string]any{"type": "boolean", "description": "On a change, script: run only while the newest revision is the one it was declared on."},
			"onOk":       map[string]any{"type": "string", "enum": append(core.PhasesAfter(core.PhaseWorking), core.PhaseReverted), "description": "On a change, script: the phase after success."},
			"prompt":     str("agent: what the agent does when the owner presses it."),
			"in":         map[string]any{"type": "string", "enum": []string{"this", "new"}},
			"when":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "On a change: the phases it shows in; default every phase not ended."},
			"primary":    map[string]any{"type": "boolean", "description": "The main next step: shown first and larger."},
			"remove":     map[string]any{"type": "boolean"},
		}, "on", "id"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			o := core.ActionSpec{ID: sArg(a, "id"), Label: sArg(a, "label"), Run: core.RunKind(sArg(a, "run")), Argv: listArg(a, "argv"), Cwd: sArg(a, "cwd"),
				Prompt: sArg(a, "prompt"), In: sArg(a, "in"), When: listOf[core.Phase](a, "when"), OnOK: core.Phase(sArg(a, "onOk"))}
			if n, ok := a["timeoutSec"].(float64); ok {
				o.TimeoutSec = int(n)
			}
			if b, _ := a["bind"].(bool); b {
				o.Revision = "newest"
			}
			o.Primary, _ = a["primary"].(bool)
			remove, _ := a["remove"].(bool)
			out, err := e.Apply(core.Input{Source: core.FromAgent, Kind: core.KindActionOffer, Thread: th, Args: core.OfferArgs{On: sArg(a, "on"), Spec: o, Remove: remove}})
			if err != nil {
				return "", err
			}
			r := out.(core.OfferReply)
			if r.Removed {
				return fmt.Sprintf("Removed %q from %s.", o.ID, r.Where), nil
			}
			return fmt.Sprintf("The owner sees %q on %s in Fyr. When they press it, the result arrives as a message starting with [Fyr].", r.Label, r.Where), nil
		}})
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
