package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/mcp"
	"github.com/meros/fyr/internal/store"
)

// Actions (offers, SPEC 0.5 V0.60): the agent declares them on a change, the
// thread or its live view; the owner presses them; Fyr runs them. Three ways:
// a built-in Fyr effect, an exact script, or a prompt for an agent.

// ScriptRunner runs a script action; tests replace it.
type ScriptRunner func(ctx context.Context, dir string, argv []string) (out string, exit int, err error)

func runScript(ctx context.Context, dir string, argv []string) (string, int, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
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
			"run":        map[string]any{"type": "string", "enum": []string{core.RunScript, core.RunAgent}},
			"argv":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "script: the program and its arguments, for example [\"terraform\", \"apply\", \"plan.out\"]. For a pipeline use [\"bash\", \"-c\", \"…\"]."},
			"cwd":        str("script: the folder to run in."),
			"timeoutSec": map[string]any{"type": "integer", "description": "script: the time limit, default 600, at most 3600."},
			"bind":       map[string]any{"type": "boolean", "description": "On a change, script: run only while the newest revision is the one it was declared on."},
			"onOk":       map[string]any{"type": "string", "enum": []string{core.PhaseReview, core.PhaseLanded, core.PhaseLive, core.PhaseReverted}, "description": "On a change, script: the phase after success."},
			"prompt":     str("agent: what the agent does when the owner presses it."),
			"in":         map[string]any{"type": "string", "enum": []string{"this", "new"}},
			"when":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "On a change: the phases it shows in; default every phase not ended."},
			"primary":    map[string]any{"type": "boolean", "description": "The main next step: shown first and larger."},
			"remove":     map[string]any{"type": "boolean"},
		}, "on", "id"),
		Call: func(_ context.Context, th string, a map[string]any) (string, error) {
			id, on := sArg(a, "id"), sArg(a, "on")
			var label, where string
			removed := false
			var err error
			e.Observe(th, func(t *core.Thread, now time.Time) {
				var h core.Holder
				if h, err = core.HolderOf(t, on); err != nil {
					return
				}
				where = on
				if h.Change != nil {
					where = fmt.Sprintf("the change %q", h.Change.Title)
					h.Change.UpdatedAt = now
					core.ChangesChecked(t, now) // V0.62
				} else if on == core.OnView && !t.LiveView {
					err = errors.New("this thread has no live view; publish one with view_publish first, or use on: \"thread\"")
					return
				}
				if b, _ := a["remove"].(bool); b {
					if !h.Remove(id) {
						err = fmt.Errorf("no action %q of yours on %s", id, where)
					}
					removed = true
					return
				}
				o := core.Offer{ID: id, Label: sArg(a, "label"), Run: sArg(a, "run"), Argv: listArg(a, "argv"), Cwd: sArg(a, "cwd"),
					Prompt: sArg(a, "prompt"), In: sArg(a, "in"), When: listArg(a, "when"), OnOK: sArg(a, "onOk")}
				if n, ok := a["timeoutSec"].(float64); ok {
					o.TimeoutSec = int(n)
				}
				if b, _ := a["bind"].(bool); b {
					o.Revision = "newest"
				}
				o.Primary, _ = a["primary"].(bool)
				if err = h.Declare(o); err == nil {
					label = h.Find(id).Label
				}
			})
			if err != nil {
				return "", err
			}
			if removed {
				return fmt.Sprintf("Removed %q from %s.", id, where), nil
			}
			return fmt.Sprintf("The owner sees %q on %s in Fyr. When they press it, the result arrives as a message starting with [Fyr].", label, where), nil
		}})
}

// runAction is the owner's press (command action.run {on, action, digest}):
// the digest must be the one the owner saw (V0.60). on is a change's id,
// "thread" or "view".
func (e *Engine) runAction(c Cmd) Reply {
	if prev, _ := e.st.Receipt(c.CommandID); prev != nil {
		var r Reply
		json.Unmarshal(prev, &r)
		r.Status = 200
		return r
	}
	th, ok := e.Thread(c.ThreadID)
	if !ok {
		return Reply{Error: "no thread " + c.ThreadID, Status: 404}
	}
	on := c.Args.Str("on")
	if on == "" {
		on = c.Args.Str("change") // the first form
	}
	h, err := core.HolderOf(&th, on)
	if err != nil {
		return Reply{Error: err.Error(), Status: 404}
	}
	act := h.Find(c.Args.Str("action"))
	if act == nil {
		return Reply{Error: "no action " + c.Args.Str("action"), Status: 404}
	}
	if d := c.Args.Str("digest"); d == "" || d != act.Digest {
		return Reply{Error: "The action changed since you saw it; look at it again.", Status: 409}
	}
	if why := h.Available(*act); why != "" {
		return Reply{Error: why, Status: 409}
	}
	a, title, cwd := *act, "this thread", th.Cwd
	if h.Change != nil {
		on, title = h.Change.ID, fmt.Sprintf("the change %q", h.Change.Title)
	} else if on == core.OnView {
		title = "the live view"
	}
	pr := ""
	if h.Change != nil {
		pr = prOfChange(h.Change)
	}
	e.Observe(c.ThreadID, func(t *core.Thread, now time.Time) {
		if x, err := core.HolderOf(t, on); err == nil {
			x.Started(a, now)
		}
	}, agent.Ev{Type: "action", Data: map[string]any{"on": on, "title": title, "action": a.ID, "label": a.Label, "run": a.Run, "argv": a.Argv, "by": "owner"}})
	reply := Reply{OK: true, Status: 200, Result: map[string]any{"on": on, "action": a.ID}}
	e.st.Write(func(tx *store.Tx) error { return tx.PutReceipt(c.CommandID, reply, e.Now()) })

	switch a.Run {
	case core.RunFyr:
		go e.runBuiltin(c.ThreadID, on, title, a, pr)
	case core.RunAgent:
		msg := fmt.Sprintf("[Fyr] The owner pressed %q on %s.\n%s", a.Label, title, a.Prompt)
		if a.In == "new" {
			r := e.Command(Cmd{CommandID: c.CommandID + ":new", Action: "thread.create", Args: core.Args{"cwd": cwd, "text": msg, "projectId": th.ProjectID}})
			if !r.OK {
				return Reply{Error: r.Error, Status: r.Status}
			}
			reply.Result.(map[string]any)["threadId"] = r.ThreadID
		} else if e.Agents != nil {
			if err := e.Agents.Do(c.ThreadID, core.Effect{Kind: "send", Text: msg}); err != nil {
				return Reply{Error: err.Error(), Status: 500}
			}
		}
	case core.RunScript:
		dir := a.Cwd
		if dir == "" {
			dir = cwd
		}
		go e.runScriptAction(c.ThreadID, on, title, dir, a)
	}
	return reply
}

func prOfChange(c *core.Change) string {
	for _, ev := range c.Evidence {
		if ev.Kind == "pr" {
			return ev.URL
		}
	}
	return ""
}

func (e *Engine) runScriptAction(threadID, on, title, dir string, a core.Offer) {
	limit := time.Duration(a.TimeoutSec) * time.Second
	if limit == 0 {
		limit = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	run := e.Scripts
	if run == nil {
		run = runScript
	}
	out, exit, err := run(ctx, dir, a.Argv)
	cancel()
	e.finishAction(threadID, on, title, a, err == nil, exit, out)
}

func (e *Engine) runBuiltin(threadID, on, title string, a core.Offer, pr string) {
	ok, text := false, "No effect runner is configured."
	if e.Effects != nil {
		ctx, cancel := context.WithTimeout(context.Background(), EffectTimeout(a.Effect))
		out, err := e.Effects.Run(ctx, threadID, core.EffectReq{Action: a.Effect, Args: map[string]any{"pr": pr}})
		cancel()
		ok, text = err == nil, out
		if err != nil {
			text = err.Error()
		}
	}
	e.finishAction(threadID, on, title, a, ok, 0, text)
	if e.OnPR != nil {
		e.OnPR(threadID)
	}
}

// finishAction records the result and tells the agent, which did not run it.
func (e *Engine) finishAction(threadID, on, title string, a core.Offer, ok bool, exit int, out string) {
	e.Observe(threadID, func(t *core.Thread, now time.Time) {
		if h, err := core.HolderOf(t, on); err == nil {
			h.Ended(t, a, ok, exit, out, now)
		}
	}, agent.Ev{Type: "action", Data: map[string]any{"on": on, "title": title, "action": a.ID, "label": a.Label, "run": a.Run, "ok": ok, "exit": exit, "output": tail(out, 2000), "ended": true}})
	t, found := e.Thread(threadID)
	if !found || t.ArchivedAt != nil || e.Agents == nil {
		return
	}
	status := "succeeded"
	if !ok {
		status = fmt.Sprintf("failed (exit %d)", exit)
	}
	msg := fmt.Sprintf("[Fyr] The owner ran %q on %s: it %s.\n%s", a.Label, title, status, tail(out, 2000))
	e.Agents.Do(threadID, core.Effect{Kind: "send", Text: msg})
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "…" + s[len(s)-n:]
	}
	return s
}
