package core

// One action model (#31; SPEC 5.1, 0.5 V0.60, decision 0018).
//
// ActionSpec is one thing the owner does with one press: id, label, how it
// runs (fyr: one of FyrEffects; script: an exact argv; agent: a prompt), and
// the digest of every field the owner saw; a press with another digest is 409.
// Actions {offers, runs} is the one component that holds specs and their last
// runs. Change, Thread and LiveView embed it; Holder declares, guards, starts
// and ends them the same way for all three.
// FyrEffects are the effects Fyr runs itself. Each has one Guard, used by
// every way to start it: the owner's command (the registry's Available), the
// press of a built-in offer, and the owner's yes to an agent's effect_request
// or change_request. A PR effect asks the change that has the PR (#32).
// The registry stays the list of owner commands, each with Available (V0.5).
// The agent hears the result of every fyr and script action (V0.60 (4)).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"
)

const (
	maxOffers    = 12
	maxRuns      = 10
	maxScriptSec = 3600
)

// ActionSpec is something the owner can do with one press.
type ActionSpec struct {
	ID string `json:"id"`
	// On is the holder for thread and view actions; a change's leave it empty.
	On     string   `json:"on,omitempty"`
	Label  string   `json:"label"`
	Run    RunKind  `json:"run"`
	Effect string   `json:"effect,omitempty"` // fyr: a key of FyrEffects
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

// ActionRun is one press of an action and its result. On names the holder
// ("thread" or "view"; "" on a change).
type ActionRun struct {
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

// Actions are a holder's actions and their last runs. It has no methods, so
// embedding it adds only the two fields.
type Actions struct {
	Offers []ActionSpec `json:"offers,omitempty"`
	Runs   []ActionRun  `json:"runs,omitempty"`
}

func (a ActionSpec) digest() string {
	h := fnv.New64a()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%q\x00%s\x00%d\x00%s\x00%s\x00%q\x00%s\x00%s", a.On, a.Label, a.Run, a.Effect, a.Argv, a.Cwd, a.TimeoutSec, a.Prompt, a.In, a.When, a.Revision, a.OnOK)
	return fmt.Sprintf("%016x", h.Sum64())
}

// Holder is one Actions with what guards it: a change, the thread or its view.
type Holder struct {
	*Actions
	// Change is set when the holder is a change: phases, revisions and
	// conditions apply.
	Change *Change
	On     string
	t      *Thread
}

// HolderOf finds the holder for on: "thread", "view", or a change id or key.
func HolderOf(t *Thread, on string) (Holder, error) {
	switch on {
	case OnThread:
		return Holder{Actions: &t.Actions, On: on, t: t}, nil
	case OnView:
		if !t.ViewOn() {
			return Holder{}, errors.New("this thread has no live view; publish one with view_publish first, or use on: \"thread\"")
		}
		return Holder{Actions: &t.Live.Actions, On: on, t: t}, nil
	}
	if c := FindChange(t, on); c != nil {
		return Holder{Actions: &c.Actions, Change: c, t: t}, nil
	}
	return Holder{}, fmt.Errorf("no change %q in this thread; on is \"thread\", \"view\" or a change's id or key", on)
}

// Declare adds or replaces an action the agent declared (V0.60). A script
// needs argv; an agent action a prompt; ids starting with "fyr." are Fyr's.
// The digest covers every field, so an action changed after the owner looked
// does not run on the old press.
func (h Holder) Declare(a ActionSpec) error {
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
	for i := range h.Offers {
		if h.Offers[i].ID == a.ID {
			h.Offers[i] = a
			return nil
		}
	}
	if len(h.Offers) >= maxOffers {
		return fmt.Errorf("at most %d actions here; remove one first", maxOffers)
	}
	h.Offers = append(h.Offers, a)
	return nil
}

// Remove drops an action the agent declared, and on a change its
// action:<id> condition (#35); its runs stay as history.
func (h Holder) Remove(id string) bool {
	for i, a := range h.Offers {
		if a.ID == id && a.By == ByAgent {
			h.Offers = append(h.Offers[:i:i], h.Offers[i+1:]...)
			if h.Change != nil {
				dropConditions(h.Change, CondAction(id))
			}
			return true
		}
	}
	return false
}

// Find is the action with id, or nil.
func (h Holder) Find(id string) *ActionSpec {
	for i := range h.Offers {
		if h.Offers[i].ID == id {
			return &h.Offers[i]
		}
	}
	return nil
}

// Available reports why the owner cannot press a now, or "". A fyr action
// also needs its effect's guard.
func (h Holder) Available(a ActionSpec) string {
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
	for _, r := range h.Runs {
		if r.Action == a.ID && r.Running {
			return "It is running"
		}
	}
	if a.Run == RunFyr {
		fx := FyrEffects[a.Effect]
		if fx == nil {
			return "Fyr has no effect " + a.Effect
		}
		return fx.Blocked(h.t, h.EffectArgs(a))
	}
	return ""
}

// EffectArgs are the arguments a fyr action runs its effect with: the PR of
// the change that holds it.
func (h Holder) EffectArgs(a ActionSpec) map[string]any {
	args := map[string]any{}
	if h.Change != nil {
		args["change"] = h.Change.ID
		if pr := h.Change.PRURL(); pr != "" {
			args["pr"] = pr
		}
	}
	return args
}

// Started records a press; Ended its result. On a change, a script that ends
// ok sets its condition ok and moves the change to OnOK, and a failed one
// sets it to fail, which blocks the thread. A failed script on the thread or
// its view is a failure line only; it moves ActivityAt so the owner sees it.
func (h Holder) Started(a ActionSpec, now time.Time) {
	h.Runs = append(h.Runs, ActionRun{On: h.On, Action: a.ID, Label: a.Label, By: ByOwner, At: now, Running: true})
	h.Runs = keepLast(h.Runs, maxRuns)
	if h.Change != nil {
		h.Change.UpdatedAt = now
	}
}

func (h Holder) Ended(a ActionSpec, ok bool, exit int, output string, now time.Time) {
	output = TruncTail(output, 4096)
	for i := len(h.Runs) - 1; i >= 0; i-- {
		if r := &h.Runs[i]; r.Action == a.ID && r.Running {
			r.Running, r.OK, r.Exit, r.Output, r.EndedAt = false, ok, exit, output, &now
			break
		}
	}
	if !ok {
		h.t.ActivityAt = now
	}
	c := h.Change
	if c == nil || a.Run != RunScript {
		return
	}
	if ok {
		setCondition(c, CondAction(a.ID), CondOK, "", ByObserved, now)
		if a.OnOK != "" {
			setPhase(h.t, c, a.OnOK, ByOwner, now)
		}
	} else {
		setCondition(c, CondAction(a.ID), CondFail, fmt.Sprintf("%s failed (exit %d)", a.Label, exit), ByObserved, now)
	}
	c.UpdatedAt = now
}

// FyrEffect is an effect Fyr runs itself, whoever started it.
type FyrEffect struct {
	ID    string
	Label string
	// Ask is the change_request ask that requests it, or "".
	Ask ChangeAsk
	// Request: the agent may ask for it with effect_request (5.1 viaEffect).
	Request bool
	// OnPR: it acts on args.pr; the poller looks at the PR again after it.
	OnPR bool
	// Guard is why it cannot run now with these args, or "". It is checked
	// when it is offered, asked for, pressed and run.
	Guard func(t *Thread, args map[string]any) string
	// Gate is why the owner's yes or press, or Fyr's own approval, cannot
	// run it now, or "": what must also hold when it runs, but not when the
	// agent asks for it. A merge waits for CI (V0.112).
	Gate func(t *Thread, args map[string]any) string
	// Check validates and completes the args.
	Check   func(args map[string]any) error
	Timeout time.Duration
}

// Blocked is why the effect cannot run now with these args: its Guard, then
// its Gate (V0.112).
func (fx *FyrEffect) Blocked(t *Thread, args map[string]any) string {
	if why := fx.Guard(t, args); why != "" {
		return why
	}
	if fx.Gate != nil {
		return fx.Gate(t, args)
	}
	return ""
}

// FyrEffects are every effect Fyr runs, by id.
var FyrEffects = map[string]*FyrEffect{
	FyrMerge: {ID: FyrMerge, Label: "Merge PR", Ask: AskMerge, Request: true, OnPR: true, Guard: prGuard(true), Gate: checksWait, Timeout: 10 * time.Minute,
		Check: func(a map[string]any) error {
			if err := needPR(a); err != nil {
				return err
			}
			if err := needHead(a); err != nil { // V0.97: a merge is bound to a head
				return err
			}
			switch m, _ := a["method"].(string); m {
			case "":
				a["method"] = "squash"
			case "squash", "merge", "rebase":
			default:
				return fmt.Errorf("method %q must be squash, merge or rebase", m)
			}
			return nil
		}},
	FyrRerun: {ID: FyrRerun, Label: "Re-run failed checks", OnPR: true, Guard: prGuard(false), Check: needPR, Timeout: 10 * time.Minute},
	// A deploy builds, tests and then drains for up to 30 minutes (V0.13).
	FyrDeploy: {ID: FyrDeploy, Label: "Deploy Fyr", Ask: AskDeploy, Request: true, Timeout: 60 * time.Minute,
		Guard: func(*Thread, map[string]any) string { return "" },
		Check: func(a map[string]any) error {
			if d, _ := a["dir"].(string); d == "" {
				return errors.New("args.dir must be the Fyr checkout to build")
			}
			return nil
		}},
}

// FyrEffectFor is the effect a change_request ask runs, or nil.
func FyrEffectFor(ask ChangeAsk) *FyrEffect {
	for _, fx := range FyrEffects {
		if ask != "" && fx.Ask == ask {
			return fx
		}
	}
	return nil
}

func needPR(a map[string]any) error {
	if pr, _ := a["pr"].(string); pr == "" {
		return errors.New("args.pr must be a GitHub pull request URL")
	}
	return nil
}

// prGuard is why a PR effect cannot run on args.pr now, or "" (V0.43 (2)).
// The change that has the PR decides (#32); a PR Fyr has no record of is left
// to GitHub, which the runner reads again.
func prGuard(merge bool) func(*Thread, map[string]any) string {
	return func(t *Thread, a map[string]any) string {
		if t.ArchivedAt != nil {
			return "The thread is archived"
		}
		url, _ := a["pr"].(string)
		if url == "" {
			return "No PR"
		}
		open, conflict, changes := true, false, false
		if c := changeByPR(t, url); c != nil {
			fails := func(typ CondType) bool { k := condition(c, typ); return k != nil && k.Status == CondFail }
			open, conflict, changes = c.Phase.InFlight(), fails(CondConflict), fails(CondReview)
		}
		switch {
		case !open:
			return "The PR is not open"
		case merge && conflict:
			return "The PR has merge conflicts"
		case merge && changes:
			return "Changes requested"
		case merge:
			return headMoved(t, a) // V0.97
		}
		return ""
	}
}

// The owner's PR commands from the PR tab (V0.43 (2)) are FyrEffects on the
// thread's main PR (#32): their Available is the effect's guard.
func init() {
	for _, id := range []string{FyrMerge, FyrRerun} {
		fx := FyrEffects[id]
		args := func(t *Thread) map[string]any {
			if p := t.MainPR(); p != nil {
				return map[string]any{"pr": p.URL}
			}
			return map[string]any{}
		}
		register(&Action{Name: id, Label: fx.Label,
			Available: func(t *Thread, _ *View, _ time.Time) string { return fx.Blocked(t, args(t)) },
			Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
				req := args(t)
				if m, ok := a["method"].(string); ok && id == FyrMerge {
					req["method"] = m
				}
				if h, ok := a["head"].(string); ok && id == FyrMerge {
					req["head"] = h // V0.97: the head the PR tab showed
				}
				if err := fx.Check(req); err != nil {
					return nil, fmt.Errorf("%w: %v", ErrBadArgs, err)
				}
				if why := fx.Blocked(t, req); why != "" {
					return nil, &Rejection{Code: RejectUnavailable, Reason: why}
				}
				return []Effect{{Kind: EffectRun, Allow: true, ByOwner: true, Request: &EffectReq{Action: id, Args: req}}}, nil
			}})
	}
}

// DecodeThread reads a stored thread, also in the shape stored before #31:
// the thread's runs in offerRuns, the view's actions and runs mixed into the
// thread's, and a change's actions under "actions"; and before #32: the PR on
// the thread, dismissedPRs and settleWhenMerged; and before #33: the
// settle in settledAt, settleWhenDone and settleWhen, and the live view's
// flag in liveView; and before #46: settle.when and a change's deployWatch,
// which become Rules.
func DecodeThread(b []byte) (*Thread, error) {
	t := &Thread{}
	if err := json.Unmarshal(b, t); err != nil {
		return nil, err
	}
	if err := migrateComponents(t, b); err != nil {
		return nil, err
	}
	if err := migratePR(t, b); err != nil {
		return nil, err
	}
	if err := migrateRules(t, b); err != nil {
		return nil, err
	}
	for i := range t.Changes { // V0.70: stored before landedAt
		c := &t.Changes[i]
		if c.LandedAt == nil && c.Phase.Reached(PhaseLanded) {
			at := c.UpdatedAt
			c.LandedAt = &at
		}
	}
	if bytes.Contains(b, []byte(`"offerRuns"`)) || bytes.Contains(b, []byte(`"actions"`)) {
		var old struct {
			Runs    []ActionRun `json:"offerRuns"`
			Changes []struct {
				Offers []ActionSpec `json:"actions"`
			} `json:"changes"`
		}
		if err := json.Unmarshal(b, &old); err != nil {
			return nil, err
		}
		if len(t.Runs) == 0 {
			t.Runs = old.Runs
		}
		for i := range old.Changes {
			if i < len(t.Changes) && len(t.Changes[i].Offers) == 0 {
				t.Changes[i].Offers = old.Changes[i].Offers
			}
		}
	}
	isView := func(on string) bool { return on == OnView }
	if slices.ContainsFunc(t.Offers, func(a ActionSpec) bool { return isView(a.On) }) || slices.ContainsFunc(t.Runs, func(r ActionRun) bool { return isView(r.On) }) {
		if t.Live == nil {
			t.Live = &LiveView{}
		}
		for _, a := range t.Offers {
			if isView(a.On) {
				t.Live.Offers = append(t.Live.Offers, a)
			}
		}
		for _, r := range t.Runs {
			if isView(r.On) {
				t.Live.Runs = append(t.Live.Runs, r)
			}
		}
		t.Offers = slices.DeleteFunc(t.Offers, func(a ActionSpec) bool { return isView(a.On) })
		t.Runs = slices.DeleteFunc(t.Runs, func(r ActionRun) bool { return isView(r.On) })
	}
	return t, nil
}

// migrateComponents moves a thread stored before #33 into its Settle and
// Live components.
func migrateComponents(t *Thread, b []byte) error {
	keys := []string{`"settledAt"`, `"settleWhenDone"`, `"settleWhen"`, `"liveView"`}
	if !slices.ContainsFunc(keys, func(k string) bool { return bytes.Contains(b, []byte(k)) }) {
		return nil
	}
	var old struct {
		SettledAt      *time.Time `json:"settledAt"`
		SettleWhenDone bool       `json:"settleWhenDone"`
		SettleWhen     *struct {
			Change string `json:"change"`
			Phase  Phase  `json:"phase"`
		} `json:"settleWhen"`
		LiveView bool `json:"liveView"`
	}
	if err := json.Unmarshal(b, &old); err != nil {
		return err
	}
	if t.Settle.At == nil && !Settling(t) {
		t.Settle.At = old.SettledAt
		if old.SettleWhenDone {
			SettleWhenDone(t, ByOwner, t.UpdatedAt)
		}
		if w := old.SettleWhen; w != nil && w.Change != "" {
			SettleWhenReached(t, w.Change, w.Phase, ByOwner, t.UpdatedAt)
		}
	}
	if old.LiveView {
		if t.Live == nil {
			t.Live = &LiveView{}
		}
		t.Live.Enabled = true
	}
	return nil
}

// migrateRules moves what watched before #46 into Rules: settle.when (#33)
// into settle Rules, a change's deployWatch that had not ended into its
// deploy Rule, and a change with a PR gets the Rule of its built-in actions.
// A deploy Rule stored before #48 also ends when its change ends.
func migrateRules(t *Thread, b []byte) error {
	now := t.UpdatedAt
	if bytes.Contains(b, []byte(`"when"`)) || bytes.Contains(b, []byte(`"deployWatch"`)) {
		var old struct {
			Settle struct {
				When json.RawMessage `json:"when"`
			} `json:"settle"`
			Changes []struct {
				ID          string `json:"id"`
				DeployWatch *struct {
					Repo     string    `json:"repo"`
					SHA      string    `json:"sha"`
					MergedAt time.Time `json:"mergedAt"`
					Checked  bool      `json:"checked"`
					Seen     bool      `json:"seen"`
					Done     bool      `json:"done"`
				} `json:"deployWatch"`
			} `json:"changes"`
		}
		if err := json.Unmarshal(b, &old); err != nil {
			return err
		}
		// settle.when was a list of {kind, change, phase}; settle Rules have
		// their own "when" objects, which are not stored under settle.
		var conds []struct {
			Kind   string `json:"kind"`
			Change string `json:"change"`
			Phase  Phase  `json:"phase"`
		}
		json.Unmarshal(old.Settle.When, &conds)
		for _, w := range conds {
			switch w.Kind {
			case "done":
				SettleWhenDone(t, ByOwner, now)
			case "change":
				SettleWhenReached(t, w.Change, w.Phase, ByOwner, now)
			}
		}
		for _, oc := range old.Changes {
			w, c := oc.DeployWatch, FindChange(t, oc.ID)
			if w == nil || w.Done || c == nil || FindRule(t, deployRuleID(c.ID)) != nil {
				continue
			}
			ArmRule(t, Rule{ID: deployRuleID(c.ID), Purpose: PurposeDeploy, About: c.ID, When: deployWhen(c, w.MergedAt, w.Seen),
				Then: Then{Kind: KindRuleEnd}, After: AfterDisarm, CreatedBy: BySystem,
				Observe: &Observe{Observer: ObsDeploy, Repo: w.Repo, SHA: w.SHA, Since: w.MergedAt, Read: w.Checked}}, now)
		}
	}
	for i := range t.Changes {
		armBuiltins(t, &t.Changes[i], now)
	}
	for i := range t.Rules {
		endsWithChange(&t.Rules[i])
	}
	return nil
}

// migratePR moves a thread stored before #32 onto its changes: a change's
// "pr" evidence becomes its PR, thread.pr goes to its change (made when none
// has it) and that change is the main one, a dismissed PR's change is
// dismissed, and settleWhenMerged waits for the main change to land.
func migratePR(t *Thread, b []byte) error {
	for i := range t.Changes {
		c := &t.Changes[i]
		for _, ev := range c.Evidence {
			if ev.Kind == "pr" && c.PR == nil && ev.URL != "" {
				repo, n := parsePRURL(ev.URL)
				c.PR = &PR{URL: ev.URL, Repo: repo, Number: n, State: "OPEN", Checks: ChecksPending}
				switch {
				case c.Phase == PhaseAbandoned:
					c.PR.State = "CLOSED"
				case !c.Phase.InFlight():
					c.PR.State = "MERGED"
				}
			}
		}
	}
	if !bytes.Contains(b, []byte(`"pr"`)) && !bytes.Contains(b, []byte(`"dismissedPRs"`)) && !bytes.Contains(b, []byte(`"settleWhenMerged"`)) {
		return nil
	}
	var old struct {
		PR               *PR      `json:"pr"`
		DismissedPRs     []string `json:"dismissedPRs"`
		SettleWhenMerged bool     `json:"settleWhenMerged"`
	}
	if err := json.Unmarshal(b, &old); err != nil {
		return err
	}
	if p := old.PR; p != nil && p.URL != "" && t.MainChange == "" {
		at := t.UpdatedAt
		if p.CheckedAt != nil {
			at = *p.CheckedAt
		}
		activity := t.ActivityAt
		c := changeByPR(t, p.URL)
		if c == nil || c.PR == nil || c.PR.CheckedAt == nil {
			c = ChangePRObserved(t, *p, at)
		}
		t.ActivityAt = activity // moving the PR is not news
		if c != nil {
			t.MainChange = c.ID
			if old.SettleWhenMerged && p.State == "OPEN" {
				SettleWhenReached(t, c.ID, PhaseLanded, ByOwner, t.UpdatedAt)
			}
		}
	}
	for _, u := range old.DismissedPRs {
		if c := changeByPR(t, u); c != nil {
			c.Dismissed = true
			if t.MainChange == c.ID {
				t.MainChange = ""
			}
		}
	}
	return nil
}

// armBuiltins arms the Rule that keeps the built-in actions of a change with
// a PR (#46): each time the change moves, change.builtins sets them again.
func armBuiltins(t *Thread, c *Change, now time.Time) {
	id := "builtins:" + c.ID
	if c.PR == nil || FindRule(t, id) != nil {
		return
	}
	ArmRule(t, Rule{ID: id, Purpose: PurposeBuiltins, About: c.ID, When: When{Kind: WhenMoved, Change: c.ID},
		Then: Then{Kind: KindBuiltins, Args: map[string]any{"change": c.ID}}, After: AfterKeep, CreatedBy: BySystem}, now)
}

// syncBuiltins is change.builtins: it sets the built-in actions of a code
// change with a PR from what Fyr observed: Merge while open; Re-run failed
// and Fix CI while checks fail; Address review while changes are requested;
// Rebase on a conflict (V0.60); Investigate a failed production deploy
// (V0.66).
func syncBuiltins(c *Change) {
	c.Offers = slices.DeleteFunc(c.Offers, func(a ActionSpec) bool { return a.By == ByObserved })
	add := func(a ActionSpec) {
		a.By = ByObserved
		a.Digest = a.digest()
		c.Offers = append(c.Offers, a)
	}
	is := func(typ CondType, status CondStatus) bool {
		k := condition(c, typ)
		return k != nil && k.Status == status
	}
	// V0.66 (6): a failed production deploy of a landed change.
	if c.Phase == PhaseLanded && is(CondDeploy, CondFail) {
		add(ActionSpec{ID: "fyr.deployfail", Label: "Investigate the failed deploy", Run: RunAgent, In: "this", Primary: true, Prompt: deployPrompt(c)})
	}
	pr := c.PRURL()
	if pr == "" || c.Phase != PhaseReview {
		return
	}
	failing, changesAsked, conflict := is(CondChecks, CondFail), is(CondReview, CondFail), is(CondConflict, CondFail)
	if failing {
		add(ActionSpec{ID: "fyr.rerun", Label: "Re-run failed checks", Run: RunFyr, Effect: FyrRerun})
		add(ActionSpec{ID: "fyr.fixci", Label: "Fix CI", Run: RunAgent, In: "this", Primary: true,
			Prompt: "CI failed on " + pr + ". Read the failed jobs (gh run view --log-failed), fix the cause and push; Fyr reads the new checks itself."})
	}
	if changesAsked {
		add(ActionSpec{ID: "fyr.review", Label: "Address review", Run: RunAgent, In: "this", Primary: !failing,
			Prompt: "The review of " + pr + " asks for changes. Read the unresolved review threads, address each one, push, and reply on each thread."})
	}
	if conflict {
		add(ActionSpec{ID: "fyr.rebase", Label: "Rebase", Run: RunAgent, In: "this",
			Prompt: "The PR " + pr + " has a merge conflict with its base. Rebase on the base, resolve the conflicts, run the tests, and push."})
	}
	add(ActionSpec{ID: "fyr.merge", Label: "Merge", Run: RunFyr, Effect: FyrMerge, Primary: !failing && !changesAsked && !conflict})
}

func deployPrompt(c *Change) string {
	p := "The deploy of " + c.Title
	if pr := c.PRURL(); pr != "" {
		p += " (" + pr + ")"
	}
	if c.PR != nil && c.PR.MergeSHA != "" {
		p += ", merge commit " + c.PR.MergeSHA + ","
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
	return p + " Read the deployment's log, find the cause, and propose a fix with change_update before you change anything."
}
