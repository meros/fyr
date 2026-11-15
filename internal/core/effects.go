package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Effects run by Fyr (decision 0020 (3), SPEC 5.10). A reduce returns them;
// the engine's runner executes each once and reports its result as an Input
// of kind effect.result with source effect:<id>, which this file reduces.

// KindEffectResult is the Input kind of an effect's result.
const KindEffectResult = "effect.result"

// Durable is true for an effect that does work outside Fyr (a merge, a
// script, a deploy): the runner stores it and runs it at most once, also
// across a restart. The agent's effects die with its process, as before.
func (e Effect) Durable() bool {
	return (e.Kind == EffectRun && e.Request != nil) || e.Kind == EffectScript
}

// EffectResult is what the runner reports for one effect.
type EffectResult struct {
	Effect Effect `json:"effect"`
	OK     bool   `json:"ok"`
	Exit   int    `json:"exit,omitempty"`
	Text   string `json:"text,omitempty"`
}

// LostText is the result of an effect that was running when Fyr stopped:
// it is not run again (at most once).
const LostText = "Fyr restarted while this ran, so Fyr does not know whether it finished; it was not run again. Check its outcome before you retry."

func init() {
	RegisterKind(&InputKind{Name: KindEffectResult, From: observed, Reduce: reduceEffectResult})
}

func reduceEffectResult(r Rec, in Input, now time.Time) (Result, error) {
	x, err := argsOf[EffectResult](in)
	if err != nil {
		return Result{}, err
	}
	var res Result
	ef := x.Effect
	switch {
	case ef.Press != nil:
		p := ef.Press
		if h, err := HolderOf(r.T, p.On); err == nil {
			h.Ended(p.Action, x.OK, x.Exit, x.Text, now)
		}
		short := TruncTail(strings.TrimSpace(x.Text), 2000)
		res.Events = append(res.Events, Event{"action", map[string]any{"on": p.On, "title": p.Title, "action": p.Action.ID, "label": p.Action.Label, "run": p.Action.Run, "ok": x.OK, "exit": x.Exit, "output": short, "ended": true}})
		res.Effects = append(res.Effects, tell(ownerRan(p.Action.Label, p.Title, x.OK, x.Exit, x.Text)))
	case ef.Kind == EffectRun && ef.Request != nil && ef.ByOwner:
		req := *ef.Request
		res.Events = append(res.Events, Event{"effect.result", map[string]any{"action": req.Action, "args": req.Args, "ok": x.OK, "text": ownerLine(req, x.OK, x.Text), "by": "owner"}})
		label := req.Action
		if fx := FyrEffects[req.Action]; fx != nil {
			label = fx.Label
		}
		res.Effects = append(res.Effects, tell(ownerRan(label, prTitle(req), x.OK, 0, x.Text)))
	case ef.Kind == EffectRun && ef.Request != nil:
		report(&res, *ef.Request, ef.Allow, x.OK, x.Text)
	case !x.OK:
		res.Events = append(res.Events, Event{"effect.error", map[string]any{"kind": ef.Kind, "error": x.Text}})
	}
	return res, nil
}

// tell is the effect that tells the thread's agent: the one place a reduce
// speaks to the agent until the agent feed (#47) projects notices instead.
func tell(text string) Effect { return Effect{Kind: EffectTell, Text: text} }

// report records the result of a request the agent made and tells the
// agent.
func report(res *Result, req EffectReq, allowed, ok bool, text string) {
	res.Events = append(res.Events, Event{"effect.result", map[string]any{"action": req.Action, "args": req.Args, "ok": ok, "text": text}})
	status := "failed"
	switch {
	case ok:
		status = "succeeded"
	case !allowed:
		status = "was declined by the user"
	}
	b, _ := json.Marshal(req.Args)
	tool := "effect_request"
	if _, ok := req.Args["change"]; ok {
		tool = "change_request" // V0.59
	}
	res.Effects = append(res.Effects, tell(fmt.Sprintf("[Fyr] Your %s %s %s %s.\n%s", tool, req.Action, string(b), status, text)))
}

// ownerRan is the agent's message about something the owner ran.
func ownerRan(label, title string, ok bool, exit int, out string) string {
	status := "succeeded"
	if !ok {
		status = fmt.Sprintf("failed (exit %d)", exit)
	}
	return fmt.Sprintf("[Fyr] The owner ran %q on %s: it %s.\n%s", label, title, status, TruncTail(strings.TrimSpace(out), 2000))
}

// prTitle names the PR of an effect for the agent.
func prTitle(req EffectReq) string {
	url, _ := req.Args["pr"].(string)
	if _, n := parsePRURL(url); n != 0 {
		return fmt.Sprintf("the PR #%d", n)
	}
	return "this thread"
}

// ownerLine starts an owner effect's result with one plain sentence.
func ownerLine(req EffectReq, ok bool, text string) string {
	url, _ := req.Args["pr"].(string)
	pr := "the PR"
	if _, n := parsePRURL(url); n != 0 {
		pr = fmt.Sprintf("#%d", n)
	}
	var first string
	switch {
	case req.Action == FyrMerge && ok:
		method, _ := req.Args["method"].(string)
		first = fmt.Sprintf("You merged %s (%s).", pr, method)
	case req.Action == FyrMerge:
		first = fmt.Sprintf("Merging %s failed.", pr)
	case req.Action == FyrRerun && ok:
		first = fmt.Sprintf("You re-ran the failed checks of %s.", pr)
	case req.Action == FyrRerun:
		first = fmt.Sprintf("Re-running the failed checks of %s failed.", pr)
	default:
		return text
	}
	if strings.TrimSpace(text) == "" {
		return first
	}
	return first + "\n\n" + strings.TrimSpace(text)
}

// answerEffect is the owner's answer to an agent's effect request (5.10):
// a decline and a change ask Fyr does not run (review, approve, apply,
// revert) are reported at once, and a yes to review or approve marks the
// change's approval ok (V0.59 (5)); a yes to a Fyr effect runs it.
func answerEffect(t *Thread, res *Result, req EffectReq, allow bool, now time.Time) {
	if ask, ok := strings.CutPrefix(req.Action, "change."); ok {
		title, _ := req.Args["title"].(string)
		if !allow {
			report(res, req, false, false, fmt.Sprintf("The owner said no to %s %q.", ask, title))
			return
		}
		if a := ChangeAsk(ask); a == AskReview || a == AskApprove {
			id, _ := req.Args["change"].(string)
			if c := FindChange(t, id); c != nil {
				no := ""
				UpdateChange(t, c, ChangeUpdate{Conditions: []Condition{{Type: CondApproval, Status: CondOK, Reason: "the owner said yes"}}, Needs: &no}, ByOwner, now)
			}
		}
		report(res, req, true, true, fmt.Sprintf("The owner said yes to %s %q.", ask, title))
		return
	}
	if !allow {
		report(res, req, false, false, "The user declined it.")
		return
	}
	res.Effects = append(res.Effects, Effect{Kind: EffectRun, Allow: true, Request: &req})
}
