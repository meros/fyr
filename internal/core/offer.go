package core

import (
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"
)

// Offers are the one action model of SPEC 0.5 V0.60: a button the owner
// presses, declared by the agent or built in by Fyr, held by a change, by the
// thread, or by the thread's live view. Every holder runs them the same way.

// Ways an offer runs.
const (
	RunFyr    = "fyr"    // a built-in effect Fyr runs: pr.merge, pr.rerunFailed
	RunScript = "script" // an exact command the agent declared
	RunAgent  = "agent"  // a prompt for this thread's agent or a new thread
)

// Holders of offers besides a change.
const (
	OnThread = "thread"
	OnView   = "view"
)

const (
	maxOffers    = 12
	maxRuns      = 10
	maxScriptSec = 3600
)

// Offer is something the owner can do with one press.
type Offer struct {
	ID string `json:"id"`
	// On is the holder for thread and view offers; a change's offers leave
	// it empty.
	On     string   `json:"on,omitempty"`
	Label  string   `json:"label"`
	Run    string   `json:"run"`
	Effect string   `json:"effect,omitempty"` // fyr
	Argv   []string `json:"argv,omitempty"`   // script
	Cwd    string   `json:"cwd,omitempty"`
	// TimeoutSec bounds a script; 0 is 600.
	TimeoutSec int    `json:"timeoutSec,omitempty"`
	Prompt     string `json:"prompt,omitempty"` // agent
	In         string `json:"in,omitempty"`     // agent: "this" or "new"
	// When, Revision and OnOK exist only on a change: the phases it shows
	// in, the revision a script is bound to (like a saved plan), the phase a
	// successful script moves the change to.
	When     []Phase `json:"when,omitempty"`
	Revision string  `json:"revision,omitempty"`
	OnOK     Phase   `json:"onOk,omitempty"`
	Primary  bool    `json:"primary,omitempty"`
	By       string  `json:"by"` // agent or observed
	Digest   string  `json:"digest"`
}

// OfferRun is one press of an offer and its result.
type OfferRun struct {
	// On is "thread" or "view" for the thread's runs; "" on a change.
	On      string     `json:"on,omitempty"`
	Action  string     `json:"action"`
	Label   string     `json:"label"`
	By      string     `json:"by"`
	At      time.Time  `json:"at"`
	Running bool       `json:"running,omitempty"`
	OK      bool       `json:"ok"`
	Exit    int        `json:"exit,omitempty"`
	Output  string     `json:"output,omitempty"` // the last 4 KB
	EndedAt *time.Time `json:"endedAt,omitempty"`
}

func (a Offer) digest() string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%q\x00%s\x00%d\x00%s\x00%s\x00%q\x00%s\x00%s", a.On, a.Label, a.Run, a.Effect, a.Argv, a.Cwd, a.TimeoutSec, a.Prompt, a.In, a.When, a.Revision, a.OnOK)
	return fmt.Sprintf("%016x", h.Sum64())
}

// Holder is where offers live: a change, or the thread (for its own and its
// view's offers).
type Holder struct {
	Offers *[]Offer
	Runs   *[]OfferRun
	// Change is set when the holder is a change: phases, revisions and
	// conditions apply.
	Change *Change
	On     string
}

// HolderOf finds the holder for on: "thread", "view", or a change id or key.
func HolderOf(t *Thread, on string) (Holder, error) {
	switch on {
	case OnThread, OnView:
		return Holder{Offers: &t.Offers, Runs: &t.OfferRuns, On: on}, nil
	}
	if c := FindChange(t, on); c != nil {
		return Holder{Offers: &c.Actions, Runs: &c.Runs, Change: c}, nil
	}
	return Holder{}, fmt.Errorf("no change %q in this thread; on is \"thread\", \"view\" or a change's id or key", on)
}

// Declare adds or replaces an offer the agent declared (V0.60). A script
// needs argv; an agent offer a prompt; ids starting with "fyr." are Fyr's.
// The digest covers every field, so an offer changed after the owner looked
// does not run on the old press.
func (h Holder) Declare(a Offer) error {
	a.ID, a.Label = strings.TrimSpace(a.ID), strings.TrimSpace(a.Label)
	if a.ID == "" || a.Label == "" {
		return errors.New("an action needs an id and a label")
	}
	if strings.HasPrefix(a.ID, "fyr.") {
		return errors.New(`action ids starting with "fyr." are Fyr's own`)
	}
	switch a.Run {
	case RunScript:
		if len(a.Argv) == 0 || strings.TrimSpace(a.Argv[0]) == "" {
			return errors.New("a script action needs argv: the program and its arguments, as a list")
		}
		if a.TimeoutSec < 0 || a.TimeoutSec > maxScriptSec {
			return fmt.Errorf("timeoutSec must be 0 to %d", maxScriptSec)
		}
		a.Prompt, a.In = "", ""
	case RunAgent:
		if strings.TrimSpace(a.Prompt) == "" {
			return errors.New("an agent action needs a prompt")
		}
		if a.In == "" {
			a.In = "this"
		}
		if a.In != "this" && a.In != "new" {
			return errors.New(`in must be "this" or "new"`)
		}
		a.Argv, a.Cwd, a.TimeoutSec, a.OnOK = nil, "", 0, ""
	default:
		return errors.New(`run must be "script" or "agent"`)
	}
	if h.Change == nil {
		if len(a.When) > 0 || a.Revision != "" || a.OnOK != "" {
			return errors.New("when, bind and onOk are for actions on a change")
		}
	} else {
		for _, p := range a.When {
			if !p.Valid() {
				return fmt.Errorf("when: unknown phase %q", p)
			}
		}
		if a.OnOK != "" && !a.OnOK.Valid() {
			return fmt.Errorf("onOk: unknown phase %q", a.OnOK)
		}
		if a.Revision == "newest" {
			if len(h.Change.Revisions) == 0 {
				return errors.New("the change has no revision to bind to; add one with change_revise")
			}
			a.Revision = h.Change.Revisions[len(h.Change.Revisions)-1].ID
		}
	}
	a.On, a.By, a.Effect = h.On, ByAgent, ""
	a.Digest = a.digest()
	list := *h.Offers
	for i := range list {
		if list[i].ID == a.ID && list[i].On == a.On {
			list[i] = a
			return nil
		}
	}
	if n := h.count(); n >= maxOffers {
		return fmt.Errorf("at most %d actions here; remove one first", maxOffers)
	}
	*h.Offers = append(list, a)
	return nil
}

func (h Holder) count() int {
	n := 0
	for _, a := range *h.Offers {
		if a.On == h.On {
			n++
		}
	}
	return n
}

// owns reports a run of this holder. Runs stored before runs had On are the
// thread's, as they were then.
func (h Holder) owns(r OfferRun) bool { return r.On == h.On || (r.On == "" && h.Change == nil) }

// Remove drops an offer the agent declared.
func (h Holder) Remove(id string) bool {
	list := *h.Offers
	for i, a := range list {
		if a.ID == id && a.On == h.On && a.By == ByAgent {
			*h.Offers = append(list[:i:i], list[i+1:]...)
			return true
		}
	}
	return false
}

// Find is the offer with id, or nil.
func (h Holder) Find(id string) *Offer {
	for i := range *h.Offers {
		if (*h.Offers)[i].ID == id && (*h.Offers)[i].On == h.On {
			return &(*h.Offers)[i]
		}
	}
	return nil
}

// Available reports why the owner cannot press a now, or "".
func (h Holder) Available(a Offer) string {
	if c := h.Change; c != nil {
		if len(a.When) == 0 {
			if c.Phase.Ended() {
				return "The change is " + string(c.Phase)
			}
		} else if !slices.Contains(a.When, c.Phase) {
			return "Not while the change is " + string(c.Phase)
		}
		if a.Revision != "" && (len(c.Revisions) == 0 || c.Revisions[len(c.Revisions)-1].ID != a.Revision) {
			return "The change has a newer revision than " + a.Revision
		}
	}
	for _, r := range *h.Runs {
		if r.Action == a.ID && h.owns(r) && r.Running {
			return "It is running"
		}
	}
	return ""
}

// Started records a press; Ended its result. On a change, a script that ends
// ok sets its condition ok and moves the change to OnOK, and a failed one
// sets it to fail, which blocks the thread. A failed script on the thread or
// its view is a failure line only; it moves ActivityAt so the owner sees it.
func (h Holder) Started(a Offer, now time.Time) {
	*h.Runs = append(*h.Runs, OfferRun{On: h.On, Action: a.ID, Label: a.Label, By: ByOwner, At: now, Running: a.Run == RunScript || a.Run == RunFyr})
	*h.Runs = keepLast(*h.Runs, maxRuns)
	if h.Change != nil {
		h.Change.UpdatedAt = now
	}
}

func (h Holder) Ended(t *Thread, a Offer, ok bool, exit int, output string, now time.Time) {
	output = TruncTail(output, 4096)
	runs := *h.Runs
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Action == a.ID && h.owns(runs[i]) && runs[i].Running {
			runs[i].Running, runs[i].OK, runs[i].Exit, runs[i].Output, runs[i].EndedAt = false, ok, exit, output, &now
			break
		}
	}
	if !ok {
		t.ActivityAt = now
	}
	c := h.Change
	if c == nil || a.Run != RunScript {
		return
	}
	if ok {
		setCondition(c, CondAction(a.ID), CondOK, "", ByObserved, now)
		if a.OnOK != "" {
			setPhase(t, c, a.OnOK, ByOwner, now)
		}
	} else {
		setCondition(c, CondAction(a.ID), CondFail, fmt.Sprintf("%s failed (exit %d)", a.Label, exit), ByObserved, now)
	}
	c.UpdatedAt = now
}

// ClearView drops the view's offers and their runs when the view goes away.
func ClearView(t *Thread) {
	t.Offers = slices.DeleteFunc(t.Offers, func(a Offer) bool { return a.On == OnView })
	t.OfferRuns = slices.DeleteFunc(t.OfferRuns, func(r OfferRun) bool { return r.On == OnView })
}

// syncBuiltins sets the built-in offers of a code change with a PR from what
// Fyr observed: Merge while open; Re-run failed and Fix CI while checks fail;
// Address review while changes are requested; Rebase on a conflict (V0.60).
func syncBuiltins(c *Change) {
	keep := c.Actions[:0]
	for _, a := range c.Actions {
		if a.By != ByObserved {
			keep = append(keep, a)
		}
	}
	c.Actions = keep
	add := func(a Offer) {
		a.By = ByObserved
		a.Digest = a.digest()
		c.Actions = append(c.Actions, a)
	}
	is := func(typ CondType, status CondStatus) bool {
		k := condition(c, typ)
		return k != nil && k.Status == status
	}
	// V0.66 (6): a failed production deploy of a landed change.
	if c.Phase == PhaseLanded && is(CondDeploy, CondFail) {
		add(Offer{ID: "fyr.deployfail", Label: "Investigate the failed deploy", Run: RunAgent, In: "this", Primary: true, Prompt: deployPrompt(c)})
	}
	pr := c.PR()
	if pr == "" || c.Phase != PhaseReview {
		return
	}
	failing, changesAsked, conflict := is(CondChecks, CondFail), is(CondReview, CondFail), is(CondConflict, CondFail)
	if failing {
		add(Offer{ID: "fyr.rerun", Label: "Re-run failed checks", Run: RunFyr, Effect: "pr.rerunFailed"})
		add(Offer{ID: "fyr.fixci", Label: "Fix CI", Run: RunAgent, In: "this", Primary: true,
			Prompt: "CI failed on " + pr + ". Read the failed jobs (gh run view --log-failed), fix the cause, push, and report with change_status."})
	}
	if changesAsked {
		add(Offer{ID: "fyr.review", Label: "Address review", Run: RunAgent, In: "this", Primary: !failing,
			Prompt: "The review of " + pr + " asks for changes. Read the unresolved review threads, address each one, push, and reply on each thread."})
	}
	if conflict {
		add(Offer{ID: "fyr.rebase", Label: "Rebase", Run: RunAgent, In: "this",
			Prompt: "The PR " + pr + " has a merge conflict with its base. Rebase on the base, resolve the conflicts, run the tests, and push."})
	}
	add(Offer{ID: "fyr.merge", Label: "Merge", Run: RunFyr, Effect: "pr.merge", Primary: !failing && !changesAsked && !conflict})
}

func deployPrompt(c *Change) string {
	p := "The deploy of " + c.Title
	if pr := c.PR(); pr != "" {
		p += " (" + pr + ")"
	}
	if c.DeployWatch != nil {
		p += ", merge commit " + c.DeployWatch.SHA + ","
	}
	if k := condition(c, CondDeploy); k != nil && k.Reason != "" {
		p += " failed: " + k.Reason + "."
	} else {
		p += " failed."
	}
	for _, ev := range c.Evidence {
		if ev.Kind == "deploy-log" {
			p += " The log: " + ev.URL + "."
		}
	}
	return p + " Read the deployment's log, find the cause, and propose a fix with change_status before you change anything."
}
