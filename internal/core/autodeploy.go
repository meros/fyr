package core

import (
	"fmt"
	"maps"
	"time"
)

// Auto-deploy (V0.111): when the owner's setting is on (the default), Fyr
// approves an agent's fyr.deploy ask itself once the engine's gate passed
// the checkout: no uncommitted file, and HEAD on origin's default branch.
// The setting is the owner's alone (KindAutoDeploySet, a host command with
// a receipt); the approval, the hold and the wait are an observer's Input
// (KindAutoDeploy). No agent tool sends either.
const (
	KindAutoDeploySet = "host.autoDeploy"
	KindAutoDeploy    = "deploy.autoApproved"
	// EffectAutoDeploySet stores the owner's setting on the host.
	EffectAutoDeploySet EffectKind = "autoDeploy.set"
)

// AutoDeployArgs are what the engine found for one open deploy ask: the
// commit the gate passed (approve it), why the gate refused (Held: the ask
// waits for the owner), or that another deploy runs (Wait).
type AutoDeployArgs struct {
	Decision string `json:"decision"`
	Commit   string `json:"commit,omitempty"`
	Held     string `json:"held,omitempty"`
	Wait     bool   `json:"wait,omitempty"`
}

// AutoDeployAsk is an open deploy ask and the checkout it builds.
type AutoDeployAsk struct {
	Decision string
	Dir      string
}

func isDeployAsk(d Decision) bool {
	return d.Kind == DecisionEffect && d.Effect != nil && d.Effect.Action == FyrDeploy
}

// AutoDeployAsks are the thread's open deploy asks; the engine gates each
// one and sends KindAutoDeploy.
func AutoDeployAsks(t *Thread) []AutoDeployAsk {
	var out []AutoDeployAsk
	for _, d := range t.Decisions {
		if isDeployAsk(d) {
			dir, _ := d.Effect.Args["dir"].(string)
			out = append(out, AutoDeployAsk{Decision: d.ID, Dir: dir})
		}
	}
	return out
}

func init() {
	RegisterKind(&InputKind{Name: KindAutoDeploySet, On: RecHost, From: ownerOnly, Await: true, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
		on, ok := mapArgs(in)["on"].(bool)
		if !ok {
			return Result{}, reject(RejectBadArgs, "on must be true or false")
		}
		return Result{Effects: []Effect{{Kind: EffectAutoDeploySet, Request: &EffectReq{Action: KindAutoDeploySet, Args: map[string]any{"on": on}}}}}, nil
	}})
	RegisterKind(&InputKind{Name: KindAutoDeploy, From: observed, Reduce: reduceAutoDeploy})
}

// AutoDeployWait is the line a deploy ask gets while another deploy runs.
const AutoDeployWait = "Another Fyr deploy runs; Fyr approves this one when it ends."

// reduceAutoDeploy closes the deploy ask as approved by the setting and
// runs the deploy bound to the commit the gate read, as the owner's yes
// would; or it notes on the thread why the ask still waits.
func reduceAutoDeploy(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[AutoDeployArgs](in)
	if err != nil {
		return Result{}, err
	}
	t := r.T
	idx := -1
	for i, d := range t.Decisions {
		if d.ID == a.Decision && isDeployAsk(d) {
			idx = i
		}
	}
	if idx < 0 {
		return Result{}, reject(RejectNotFound, "no open deploy ask %q", a.Decision)
	}
	switch {
	case a.Held != "":
		return Result{Events: []Event{{"effect.error", map[string]any{"kind": "autoDeploy", "decision": a.Decision,
			"error": fmt.Sprintf("Not auto-approved: %s. The deploy waits for your press.", a.Held)}}}}, nil
	case a.Wait:
		return Result{Events: []Event{{"effect.error", map[string]any{"kind": "autoDeploy", "decision": a.Decision, "error": AutoDeployWait}}}}, nil
	case a.Commit == "":
		return Result{}, reject(RejectBadArgs, "an approval names the commit the gate read")
	}
	d := t.Decisions[idx]
	t.Decisions = append(t.Decisions[:idx:idx], t.Decisions[idx+1:]...)
	if len(t.Decisions) == 0 {
		t.Decisions = nil
	}
	req := *d.Effect
	req.Args = maps.Clone(req.Args)
	req.Args["autoDeploy"] = true
	req.Args["commit"] = a.Commit // the deployer refuses a HEAD that moved
	if id, _ := req.Args["change"].(string); id != "" {
		if c := FindChange(t, id); c != nil {
			c.AutoApproved = &AutoApproval{Deploy: true, Head: a.Commit, At: now}
			c.UpdatedAt = now
		}
	}
	answer := fmt.Sprintf("Auto-approved: Fyr deploys %s without a press.", shortSHA(a.Commit))
	return Result{
		Events: []Event{{"decision.closed", map[string]any{
			"id": d.ID, "answer": answer, "auto": true, "action": req.Action, "args": req.Args,
		}}},
		Effects: []Effect{{Kind: EffectRun, Allow: true, Request: &req}},
	}, nil
}
