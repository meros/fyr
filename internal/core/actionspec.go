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
// press of a built-in offer, and the owner's yes to an agent's
// change_request. A PR effect asks the change that has the PR (#32).
// The registry stays the list of owner commands, each with Available (V0.5).
// The agent hears the result of every fyr and script action (V0.60 (4)).

import (
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
	// Request, Command, Why and Root are a run request's (V0.149): the
	// shell text the owner sees and runs (argv is bash -c it), one line
	// why, and whether it runs as root through pkexec.
	Request bool   `json:"request,omitempty"`
	Command string `json:"command,omitempty"`
	Why     string `json:"why,omitempty"`
	Root    bool   `json:"root,omitempty"`
	// Optional is a run request nothing waits on (V0.next-step-chips): a
	// chip under the newest message, never in Your move or the dock.
	Optional bool   `json:"optional,omitempty"`
	By       string `json:"by"` // agent or observed
	Digest   string `json:"digest"`
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
	if a.Request { // V0.149: what the card shows; older digests stay as they were
		fmt.Fprintf(h, "\x00%q\x00%q\x00%t", a.Command, a.Why, a.Root)
		if a.Optional { // V0.next-step-chips: older digests stay as they were
			fmt.Fprint(h, "\x00optional")
		}
	}
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
func (h Holder) Declare(a ActionSpec) error { return h.declare(a, false) }

// declare is Declare; request: run_request declares it (V0.149), and only
// it may.
func (h Holder) declare(a ActionSpec, request bool) error {
	if a.Request != request {
		return errors.New("a run request is declared with run_request")
	}
	if !request {
		a.Command, a.Why, a.Root, a.Optional = "", "", false, false
	}
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
				return errors.New("the change has no revision to bind to; add one with change_update {revision}")
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
	if c := h.Change; c != nil && a.Run == RunFyr && a.Effect == FyrMerge && c.InMerge() {
		return "It is merging" // V0.merge-in-flight: however the merge was started
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
	FyrMerge: {ID: FyrMerge, Label: "Merge PR", Ask: AskMerge, OnPR: true, Guard: prGuard(true), Gate: checksWait, Timeout: 10 * time.Minute,
		Check: func(a map[string]any) error {
			if err := needPR(a); err != nil {
				return err
			}
			if err := needHead(a); err != nil { // V0.97: a merge is bound to a head
				return err
			}
			if err := checkSensitive(a); err != nil { // V0.sensitive-pr
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
	// A deploy installs a release, or builds a checkout, then drains for up
	// to 30 minutes (V0.13, decision 0027).
	// Only a thread on Fyr's own repo may ask for it (V0.one-tool-vocabulary,
	// reduceChangeRequest); the owner's own deploy is host.deploy.
	FyrDeploy: {ID: FyrDeploy, Label: "Deploy Fyr", Ask: AskFyrDeploy, Timeout: 60 * time.Minute,
		Guard: func(*Thread, map[string]any) string { return "" },
		Check: func(a map[string]any) error {
			if d, ok := a["dir"]; ok {
				if s, _ := d.(string); s == "" {
					return errors.New("args.dir, when given, must be the Fyr checkout to build")
				}
			}
			return nil
		}},
}

// fyrEffectByAsk is FyrEffects by their change_request ask; two effects
// with one ask stop the build's first run (V0.one-predicate (3)).
var fyrEffectByAsk = func() map[ChangeAsk]*FyrEffect {
	m := map[ChangeAsk]*FyrEffect{}
	for _, fx := range FyrEffects {
		if fx.Ask == "" {
			continue
		}
		if m[fx.Ask] != nil {
			panic("two Fyr effects for the ask " + string(fx.Ask))
		}
		m[fx.Ask] = fx
	}
	return m
}()

// FyrEffectFor is the effect a change_request ask runs, or nil.
func FyrEffectFor(ask ChangeAsk) *FyrEffect { return fyrEffectByAsk[ask] }

// noPRText is why a merge ask on c cannot be made, and what to do instead
// (V0.prompt-fits-setup): its repository has no GitHub remote, or it has no
// pull request yet.
func noPRText(c *Change) string {
	if repo := RecordRepo(c); repo != "" && !strings.HasPrefix(repo, "github.com/") {
		return fmt.Sprintf("%q has no GitHub pull request to merge: its repository (%s) has no GitHub remote, so Fyr cannot merge it. Do not merge it yourself: say the work is done and the owner lands the branch; for a command they must run, call mcp__fyr__run_request.", c.Title, repo)
	}
	return fmt.Sprintf("%q has no GitHub pull request yet. Push the branch and open one (gh pr create), then ask again; Fyr links it by itself. If this project has no GitHub remote, Fyr cannot merge here: say the work is done and the owner lands the branch.", c.Title)
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

// The owner's PR commands from the PR tab (V0.43 (2)) are FyrEffects on a PR
// of the thread (#32): optional args.pr or args.change picks the PR, defaulting
// to the main PR. Their Available is the effect's guard across the thread's PRs.
func init() {
	for _, id := range []string{FyrMerge, FyrRerun} {
		fx := FyrEffects[id]
		args := func(t *Thread, a Args) (map[string]any, error) {
			ref := strings.TrimSpace(a.Str("pr"))
			if ref == "" {
				ref = strings.TrimSpace(a.Str("change"))
			}
			c := t.PRChange(ref)
			if ref != "" && (c == nil || c.PR == nil) {
				return nil, fmt.Errorf("%w: no such PR on this thread", ErrBadArgs)
			}
			if c != nil && c.PR != nil {
				return map[string]any{"pr": c.PR.URL}, nil
			}
			return map[string]any{}, nil
		}
		register(&Action{Name: id, Label: fx.Label,
			Available: func(t *Thread, _ *View, _ time.Time) string {
				first := ""
				if c := t.PRChange(""); c != nil && c.PR != nil {
					if why := fx.Blocked(t, map[string]any{"pr": c.PR.URL}); why == "" {
						return ""
					} else {
						first = why
					}
				}
				for i := range t.Changes {
					c := &t.Changes[i]
					if c.PR == nil || c.Dismissed {
						continue
					}
					if why := fx.Blocked(t, map[string]any{"pr": c.PR.URL}); why == "" {
						return ""
					} else if first == "" {
						first = why
					}
				}
				if first != "" {
					return first
				}
				return "No PR"
			},
			Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
				req, err := args(t, a)
				if err != nil {
					return nil, err
				}
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
	if is(CondChecks, CondPending) {
		return // V0.136: no Merge on the card while CI runs; the PR tab says why it waits
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

func reduceOffer(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[OfferArgs](in)
	if err != nil {
		return Result{}, err
	}
	h, err := HolderOf(r.T, a.On)
	if err != nil {
		return Result{}, err
	}
	where := a.On
	if h.Change != nil {
		where = fmt.Sprintf("the change %q", h.Change.Title)
		h.Change.UpdatedAt = now
	}
	if a.Remove {
		if !h.Remove(a.Spec.ID) {
			return Result{}, fmt.Errorf("no action %q of yours on %s", a.Spec.ID, where)
		}
		return Result{Reply: OfferReply{Where: where, Removed: true}}, nil
	}
	if err := h.Declare(a.Spec); err != nil {
		return Result{}, err
	}
	return Result{Reply: OfferReply{Label: h.Find(a.Spec.ID).Label, Where: where}}, nil
}

// reducePress is the owner's press (command action.run {on, action,
// digest}, V0.60): the digest must be the one the owner saw, and the
// action must be available. The press starts the run; its effect does the
// work and its result ends it.
func reducePress(r Rec, in Input, now time.Time) (Result, error) {
	args := mapArgs(in)
	t := r.T
	on := args.Str("on")
	if on == "" {
		on = args.Str("change") // the first form
	}
	h, err := HolderOf(t, on)
	if err != nil {
		return Result{}, &Rejection{Code: RejectNotFound, Reason: err.Error()}
	}
	act := h.Find(args.Str("action"))
	if act == nil {
		return Result{}, reject(RejectNotFound, "no action %s", args.Str("action"))
	}
	if d := args.Str("digest"); d == "" || d != act.Digest {
		return Result{}, reject(RejectUnavailable, "The action changed since you saw it; look at it again.")
	}
	if why := h.Available(*act); why != "" {
		return Result{}, &Rejection{Code: RejectUnavailable, Reason: why}
	}
	if last := lastRunOf(h.Runs, act.ID, h.On); last != nil && now.Sub(last.At) < PressTwice {
		// V0.71: a page or a double tap that sends one press twice
		return Result{}, &Rejection{Code: RejectUnavailable, Reason: "It was just pressed"}
	}
	a, title := *act, "this thread"
	if h.Change != nil {
		on, title = h.Change.ID, fmt.Sprintf("the change %q", h.Change.Title)
	} else if on == OnView {
		title = "the live view"
	}
	req := h.EffectArgs(a)
	if a.Run == RunFyr && a.Effect == FyrMerge { // V0.97: bound to the head the owner saw
		req["head"] = args.Str("head")
		if err := needHead(req); err != nil {
			return Result{}, fmt.Errorf("%w: %v", ErrBadArgs, err)
		}
		if why := headMoved(t, req); why != "" {
			return Result{}, &Rejection{Code: RejectUnavailable, Reason: why}
		}
	}
	h.Started(a, now)
	res := Result{Reply: map[string]any{"on": on, "action": a.ID},
		Events: []Event{{"action", withActor(pressData(on, title, a, map[string]any{"by": "owner"}), in)}}}
	press := &Press{On: on, Title: title, Action: a}
	switch a.Run {
	case RunFyr:
		res.Effects = []Effect{{Kind: EffectRun, Allow: true, Press: press, Request: &EffectReq{Action: a.Effect, Args: req}}}
	case RunAgent:
		msg := fmt.Sprintf("[Fyr] The owner pressed %q on %s.\n%s", a.Label, title, a.Prompt)
		// V0.71: every press runs until its result lands, so a second
		// press is "It is running"; a new thread follows this one.
		if a.In == "new" {
			res.Effects = []Effect{{Kind: EffectThread, Press: press, Text: msg, Cwd: t.Cwd, Project: t.ProjectID}}
		} else {
			res.Effects = []Effect{{Kind: EffectSend, Press: press, Text: msg}}
			markStarting(t, now) // V0.thread-claims: the press starts a turn
		}
	case RunScript:
		dir := a.Cwd
		if dir == "" {
			dir = t.Cwd
		}
		res.Effects = []Effect{{Kind: EffectScript, Press: press, Script: &Script{Argv: a.Argv, Dir: dir, TimeoutSec: a.TimeoutSec, Root: a.Root}}}
	}
	return res, nil
}

// PressTwice is how close a second press of one action counts as the same
// press (V0.71).
const PressTwice = 2 * time.Second

func lastRunOf(runs []ActionRun, action, on string) *ActionRun {
	for i := len(runs) - 1; i >= 0; i-- {
		if runs[i].Action == action && runs[i].On == on {
			return &runs[i]
		}
	}
	return nil
}
