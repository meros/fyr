package core

import (
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
		res.Events = append(res.Events, Event{"action", pressData(p.On, p.Title, p.Action, map[string]any{"ok": x.OK, "exit": x.Exit, "output": short, "ended": true})})
	case ef.Kind == EffectRun && ef.Request != nil && ef.ByOwner:
		req := *ef.Request
		res.Events = append(res.Events, Event{"effect.result", map[string]any{"action": req.Action, "args": req.Args, "ok": x.OK, "text": ownerLine(req, x.OK, x.Text), "output": TruncTail(strings.TrimSpace(x.Text), 2000), "by": "owner"}})
	case ef.Kind == EffectRun && ef.Request != nil:
		report(&res, *ef.Request, ef.Allow, x.OK, x.Text)
	case !x.OK:
		res.Events = append(res.Events, Event{"effect.error", map[string]any{"kind": ef.Kind, "error": x.Text}})
	}
	// V0.136: a merge Fyr ran that failed, whoever started it, marks the
	// change and tells the agent GitHub's words and the next step.
	if req := ef.Request; req != nil && req.Action == FyrMerge && !x.OK && (ef.Allow || ef.ByOwner || ef.Press != nil) {
		url, _ := req.Args["pr"].(string)
		res.Events = append(res.Events, mergeRefused(r.T, url, x.Text, now)...)
	}
	return res, nil
}

// report records the result of a request the agent made; the agent feed
// tells the agent (feed.go, the result rule).
func report(res *Result, req EffectReq, allowed, ok bool, text string) {
	if line := autoLine(req); line != "" { // V0.102
		text = strings.TrimSpace(line + " " + text)
	}
	data := map[string]any{"action": req.Action, "args": req.Args, "ok": ok, "text": text}
	if !allowed {
		data["declined"] = true
	}
	res.Events = append(res.Events, Event{"effect.result", data})
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
// a decline, and an old change ask Fyr does not run (review, approve,
// apply, revert: gone since V0.changes-code-only, but a stored one may still
// be open), are reported at once; a yes to a Fyr effect runs it.
func answerEffect(t *Thread, res *Result, req EffectReq, allow bool, now time.Time) {
	if ask, ok := strings.CutPrefix(req.Action, "change."); ok {
		title, _ := req.Args["title"].(string)
		if !allow {
			report(res, req, false, false, fmt.Sprintf("The owner said no to %s %q.", ask, title))
			return
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
