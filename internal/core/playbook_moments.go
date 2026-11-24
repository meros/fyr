package core

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Stage moments (V0.playbook-moments): what Fyr does by itself when a merged
// change moves among its project's stages. A stage the owner checks opens a
// question card when the change gets there; a stage with a deploy command
// gets a Run card when the stage before it holds the change. Both put the
// thread in Your move without a model being asked; the agent hears the
// owner's answer, or the command's result, as it hears its own.

// KindStageMoment is the host's stage moment for a change: args
// StageMomentArgs.
const KindStageMoment = "stage.moment"

// MomentFresh is how long after a stage was reached (or the change landed)
// its moment may still open: an entry written later asks nothing about
// changes long since deployed.
const MomentFresh = 24 * time.Hour

// StageMomentArgs is one moment: the owner's check of Stage (Ask is the
// decision's id), or the Run card of Stage's deploy command (Command, in
// Cwd).
type StageMomentArgs struct {
	Change  string
	Stage   string
	Ask     string
	URL     string
	How     string
	Command string
	Cwd     string
}

func init() {
	RegisterKind(&InputKind{Name: KindStageMoment, From: observed, Reduce: reduceStageMoment})
}

// MomentAsk and MomentRun are a moment's keys in Change.Staged.
func MomentAsk(stage string) string { return "ask:" + stage }
func MomentRun(stage string) string { return "run:" + stage }

// MomentOK marks the owner's "Looks good" for a stage.
func MomentOK(stage string) string { return "ok:" + stage }

const verifyPrefix = "verify:"

// stageAnswered records the owner's "Looks good" to a stage check on its
// change, so the next stage's deploy command may be offered.
func stageAnswered(t *Thread, d Decision, answers map[string]string) {
	rest, ok := strings.CutPrefix(d.ID, verifyPrefix)
	if !ok || len(d.Questions) != 1 {
		return
	}
	id, stage, _ := strings.Cut(rest, ":")
	c := FindChange(t, id)
	if c == nil || answers[d.Questions[0].Question] != StageLooksGood || slices.Contains(c.Staged, MomentOK(stage)) {
		return
	}
	c.Staged = append(c.Staged, MomentOK(stage))
}

// StageMoments are the moments due for change c among stages (in the order
// a change reaches them) at now: each stage the owner checks that c reached
// since its entry was written, and each stage with a deploy command that c
// has not reached while the stage before it holds c (the merge, for the
// first stage) and its owner's check, if it has one, is no longer open.
func StageMoments(t *Thread, c *Change, stages []PlaybookEntry, now time.Time) []StageMomentArgs {
	if c == nil || c.Kind != KindCode || c.Phase.Ended() || !c.Phase.Reached(PhaseLanded) || t.ArchivedAt != nil {
		return nil
	}
	sha, repo := "", ""
	if c.PR != nil {
		sha, repo = c.PR.MergeSHA, c.PR.Repo
	}
	fresh := func(at time.Time, e PlaybookEntry) bool { return !at.Before(e.At) && now.Sub(at) < MomentFresh }
	reachedAt := func(e PlaybookEntry) (time.Time, bool) {
		typ := CondDeployTo(e.Name)
		if e.IsLive() {
			typ = CondDeploy
		}
		if k := condition(c, typ); k != nil && k.Status == CondOK {
			return k.At, true
		}
		return time.Time{}, false
	}
	var out []StageMomentArgs
	prevAt, prevOK, prevAsk := time.Time{}, c.LandedAt != nil, ""
	if c.LandedAt != nil {
		prevAt = *c.LandedAt
	}
	for _, e := range stages {
		at, ok := reachedAt(e)
		if ok && e.OwnerChecks && fresh(at, e) && !slices.Contains(c.Staged, MomentAsk(e.Name)) {
			out = append(out, StageMomentArgs{Change: c.ID, Stage: e.Name, Ask: verifyPrefix + c.ID + ":" + e.Name, URL: stageURL(c, e), How: e.How})
		}
		// A stage the owner checks holds the next deploy until they said it
		// looks good there.
		held := prevAsk != "" && !slices.Contains(c.Staged, MomentOK(prevAsk))
		if !ok && e.Deploy != "" && prevOK && !held && fresh(prevAt, e) && !slices.Contains(c.Staged, MomentRun(e.Name)) {
			if cmd := ProbeScript(e.Deploy, sha, repo); cmd != "" {
				out = append(out, StageMomentArgs{Change: c.ID, Stage: e.Name, Command: cmd})
			}
		}
		prevAt, prevOK, prevAsk = at, ok, ""
		if e.OwnerChecks {
			prevAsk = e.Name
		}
	}
	return out
}

// stageURL is where stage e is looked at for c: the address its deployment
// gave, else the playbook's.
func stageURL(c *Change, e PlaybookEntry) string {
	for _, ev := range c.Evidence {
		if ev.Kind == "deployment" && ev.Title == e.Name && ev.URL != "" {
			return ev.URL
		}
	}
	return e.URL
}

// The owner's two answers to a stage check.
const (
	StageLooksGood = "Looks good"
	StageIsWrong   = "Something is wrong"
)

func reduceStageMoment(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[StageMomentArgs](in)
	if err != nil {
		return Result{}, err
	}
	t := r.T
	c := FindChange(t, a.Change)
	if c == nil || t.ArchivedAt != nil {
		return Result{}, nil
	}
	name := changeName(c)
	if a.Command != "" {
		if slices.Contains(c.Staged, MomentRun(a.Stage)) {
			return Result{}, nil
		}
		res, err := openRunRequest(t, RunRequestArgs{ID: "deploy-" + a.Stage, Command: a.Command, Cwd: a.Cwd,
			Why: clipWhy(fmt.Sprintf("Deploy %s to %s (the project's playbook)", name, a.Stage))}, now)
		if err != nil {
			return Result{}, err
		}
		c.Staged = append(c.Staged, MomentRun(a.Stage))
		res.Reply = nil
		return res, nil
	}
	if slices.Contains(c.Staged, MomentAsk(a.Stage)) {
		return Result{}, nil
	}
	q := fmt.Sprintf("%s (%s) is on %s", name, c.Title, a.Stage)
	if c.PR == nil || c.PR.Number == 0 {
		q = fmt.Sprintf("%s is on %s", name, a.Stage)
	}
	if a.URL != "" {
		q += ": " + a.URL
	}
	q += ". Does it look right there?"
	d := Decision{ID: a.Ask, Kind: DecisionAsk, Assignee: Owner, Async: true, CreatedAt: now, Questions: []Question{{
		Question: q, Header: "Check " + a.Stage,
		Options: []Option{
			{Label: StageLooksGood, Description: "The agent hears it and goes on with what follows."},
			{Label: StageIsWrong, Description: "Say what in your own words; the agent picks it up."},
		}}}}
	c.Staged = append(c.Staged, MomentAsk(a.Stage))
	DecisionOpened(t, d)
	return Result{Events: []Event{{"decision.opened", map[string]any{"decision": d}}}}, nil
}

func clipWhy(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxWhy {
		s = s[:maxWhy-1] + "…"
	}
	return s
}
