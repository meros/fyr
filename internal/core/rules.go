package core

// Rules (SPEC 7, decision 0020 part 4, #46). Everything that watches
// something and then acts is a Rule {when, then, after}: settle when done or
// when a change reaches a phase, snooze, the deploy watch and its time
// limits, the built-in actions that follow a PR, and the agent's own
// watches. A Rule is a part of its owner's record: the thread's Rules, or
// the owner's View for a snooze.
//
// EvalRules is the scheduler. Reduce runs it after every Input, so a Rule is
// evaluated after every event that can change what its Condition reads, on
// host start (host.restarted is an Input) and at its NextCheck (the engine's
// clock sends an Input then). A Rule fires when its Condition goes from false
// to true (7.3): its `then` Input is reduced on the same records, in the same
// transaction, through the same kinds, at most maxFirings times per Input.
// Conditions read records only, never the network: observers bring facts in
// as Inputs.

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"
)

// ByRule is the principal of a Rule's `then` Input (source rule:<id>).
const ByRule = "rule"

// FromRule is the source of the Input a firing of rule id applies.
func FromRule(id string) Source { return Source("rule:" + id) }

// The purposes of Rules: what they are for, which decides what suspends
// them (7.4) and how the owner reads them.
const (
	PurposeSettle   = "settle"      // then: rule.settle
	PurposeSnooze   = "snooze"      // on the View; then: rule.wake
	PurposeDeploy   = "deployWatch" // the deploy watch: the deployments observer reads while it is armed
	PurposeBuiltins = "builtins"    // the built-in actions of a change with a PR
	PurposeWatch    = "watch"       // the agent's own: then rule.notify
)

// The kinds of a Rule's `then`.
const (
	KindRuleSettle = "rule.settle"
	KindRuleWake   = "rule.wake"
	KindRuleNotify = "rule.notify"
	KindRuleEnd    = "rule.end"
	KindBuiltins   = "change.builtins"
	KindClock      = "clock"
	KindRuleArm    = "rule.arm"
	KindRuleRemove = "rule.remove"
	AfterDisarm    = "disarm"
	AfterKeep      = "keep"
	RuleArmed      = "armed"
	RuleSuspended  = "suspended"
	SuspendSettle  = "settle"
	SuspendArchive = "archive"
	SuspendPin     = "pinned" // V0.106: a settle Rule the owner did not arm waits while the thread is pinned
	maxFirings     = 8
	maxRejections  = 10
	maxAgentRules  = 20
	ObsClock       = "clock"
	BySystem       = "system" // the creator of the Rules Fyr arms itself
)

// Rule is one armed watch (SPEC 7.1).
type Rule struct {
	ID      string `json:"id"`
	Purpose string `json:"purpose"`
	// About is the change the Rule watches or acts on, "" for the thread. A
	// Rule about a change that is gone is dropped.
	About string `json:"about,omitempty"`
	When  When   `json:"when"`
	Then  Then   `json:"then"`
	After string `json:"after"` // disarm or keep
	// CreatedBy is owner, agent or system.
	CreatedBy   string   `json:"createdBy"`
	Status      string   `json:"status"` // armed or suspended
	SuspendedBy []string `json:"suspendedBy,omitempty"`
	// Armed is the anchored Conditions' snapshot (7.2 Order): the run count
	// for runEnded, a change's digest for changeMoved. A firing of a kept
	// Rule takes it again.
	Armed      map[string]string `json:"armed,omitempty"`
	ArmedAt    time.Time         `json:"armedAt"`
	LastValue  bool              `json:"lastValue,omitempty"`
	Firings    int               `json:"firings,omitempty"`
	Rejections int               `json:"rejections,omitempty"`
	RetryAt    *time.Time        `json:"retryAt,omitempty"`
	// Observe is what an observer reads while the Rule is armed (the deploy
	// watch: a merge commit's deployments). Read is set after the first read.
	Observe *Observe `json:"observe,omitempty"`
}

// Outcome is how a firing's `then` went (7.3).
type Outcome string

const (
	OutcomeOK       Outcome = "ok"
	OutcomeRejected Outcome = "rejected"
)

// Then is the Input a firing applies.
type Then struct {
	Kind string         `json:"kind"`
	Args map[string]any `json:"args,omitempty"`
}

// Observe is what an observer reads for a Rule.
type Observe struct {
	Observer string    `json:"observer"`
	Repo     string    `json:"repo,omitempty"`
	SHA      string    `json:"sha,omitempty"`
	Since    time.Time `json:"since"`
	Read     bool      `json:"read,omitempty"`
}

// When is a Condition (SPEC 7.2): one kind with its params, or any, all or
// not of others.
type When struct {
	Kind   string     `json:"kind,omitempty"`
	Change string     `json:"change,omitempty"`
	Phase  Phase      `json:"phase,omitempty"`
	Type   CondType   `json:"type,omitempty"`
	Status CondStatus `json:"status,omitempty"`
	At     *time.Time `json:"at,omitempty"`
	Ms     int64      `json:"ms,omitempty"`
	Any    []When     `json:"any,omitempty"`
	All    []When     `json:"all,omitempty"`
	Not    *When      `json:"not,omitempty"`
}

// The Condition kinds. Each reads the thread record only.
const (
	WhenRunEnded    = "runEnded"      // anchored: a run ended after the Rule was armed
	WhenReached     = "changeReached" // {change, phase}: the change got at least that far
	WhenCondition   = "condition"     // {change, type, status}: a condition of the change has that status
	WhenAt          = "at"            // anchored: {at}: now ≥ at
	WhenIdleFor     = "idleFor"       // {ms}: nothing happened on the thread for ms
	WhenIdle        = "idle"          // no run is live and the last one did not fail
	WhenNoDecisions = "noDecisions"   // no question or approval waits
	WhenMoved       = "changeMoved"   // anchored: {change}: the change's phase, conditions or evidence moved since the last firing
	WhenEnded       = "changeEnded"   // {change}: the change ended (abandoned or reverted)
	WhenWorkOver    = "workOver"      // V0.185: no work goes on, or the thread needs the owner
	WhenWorkDone    = "workDone"      // V0.203: no work goes on, nothing waits for the owner, no PR is open
)

// AgentWhens are the kinds the agent may use in its own Rules.
var AgentWhens = []string{WhenRunEnded, WhenReached, WhenCondition, WhenAt, WhenIdleFor}

// All is the Condition that holds when all of ws hold.
func All(ws ...When) When { return When{All: ws} }

// Any is the Condition that holds when one of ws holds.
func Any(ws ...When) When { return When{Any: ws} }

// tri is a three-valued result (7.2 Logic).
type tri int8

const (
	no tri = iota
	yes
	unknown
)

func truth(b bool) tri {
	if b {
		return yes
	}
	return no
}

// eval is the Condition's value on the thread at now, with the Rule's
// anchors.
func (w When) eval(t *Thread, armed map[string]string, now time.Time) tri {
	switch {
	case w.Not != nil:
		switch w.Not.eval(t, armed, now) {
		case yes:
			return no
		case no:
			return yes
		}
		return unknown
	case len(w.All) > 0:
		out := yes
		for _, x := range w.All {
			switch x.eval(t, armed, now) {
			case no:
				return no
			case unknown:
				out = unknown
			}
		}
		return out
	case len(w.Any) > 0:
		out := no
		for _, x := range w.Any {
			switch x.eval(t, armed, now) {
			case yes:
				return yes
			case unknown:
				out = unknown
			}
		}
		return out
	}
	switch w.Kind {
	case WhenRunEnded:
		n, _ := strconv.Atoi(armed["run"])
		return truth(t.RunEnds > n)
	case WhenAt:
		return truth(w.At != nil && !now.Before(*w.At))
	case WhenIdleFor:
		return truth(now.Sub(t.ActivityAt) >= time.Duration(w.Ms)*time.Millisecond)
	case WhenIdle:
		return truth(!t.Running && t.Failure == nil)
	case WhenNoDecisions:
		return truth(len(t.Decisions) == 0)
	case WhenWorkOver:
		return truth(WorkOver(t))
	case WhenWorkDone:
		return truth(WorkDone(t))
	}
	c := FindChange(t, w.Change)
	if c == nil {
		return unknown
	}
	switch w.Kind {
	case WhenReached:
		return truth(c.Phase.Reached(w.Phase))
	case WhenEnded:
		return truth(c.Phase.Ended())
	case WhenCondition:
		if at, ok := armed["pr:"+c.ID]; ok && prFact(w.Type) && c.PR != nil && c.PR.State == "OPEN" && !readAfter(c.PR, at) {
			// V0.checks-follow-head: not known until GitHub is read again.
			return unknown
		}
		k := condition(c, w.Type)
		return truth(k != nil && k.Status == w.Status)
	case WhenMoved:
		return truth(armed["change:"+c.ID] != changeDigest(c))
	}
	return unknown
}

// nextCheck is the earliest time the Condition may change with time alone,
// or nil.
func (w When) nextCheck(t *Thread) *time.Time {
	var out *time.Time
	take := func(x *time.Time) {
		if x != nil && (out == nil || x.Before(*out)) {
			out = x
		}
	}
	for _, x := range slices.Concat(w.All, w.Any) {
		take(x.nextCheck(t))
	}
	if w.Not != nil {
		take(w.Not.nextCheck(t))
	}
	switch w.Kind {
	case WhenAt:
		take(w.At)
	case WhenIdleFor:
		at := t.ActivityAt.Add(time.Duration(w.Ms) * time.Millisecond)
		take(&at)
	}
	return out
}

// anchored reports a Condition made true by an event after the Rule was
// armed (7.2), not by the current state.
func (w When) anchored() bool {
	for _, x := range slices.Concat(w.All, w.Any) {
		if x.anchored() {
			return true
		}
	}
	return w.Kind == WhenRunEnded || w.Kind == WhenAt || w.Kind == WhenMoved
}

// anchor takes the snapshot of the anchored kinds in w.
func (w When) anchor(t *Thread, armed map[string]string) {
	for _, x := range slices.Concat(w.All, w.Any) {
		x.anchor(t, armed)
	}
	if w.Not != nil {
		w.Not.anchor(t, armed)
	}
	switch w.Kind {
	case WhenRunEnded:
		armed["run"] = strconv.Itoa(t.RunEnds)
	case WhenMoved:
		if c := FindChange(t, w.Change); c != nil {
			armed["change:"+c.ID] = changeDigest(c)
		}
	}
}

// prFact: a condition the PR poller reads from GitHub, about the PR's head.
func prFact(typ CondType) bool {
	return typ == CondChecks || typ == CondReview || typ == CondConflict
}

// anchorPR anchors each PR fact the agent's watch reads to the PR read the
// change holds now (V0.checks-follow-head, #120): the watch holds only on a
// later read. A watch armed right after a push would otherwise fire on the
// previous head's checks before the poller has read the new head. It
// reports whether the watch reads one, so Fyr reads the PR at once.
func (w When) anchorPR(t *Thread, armed map[string]string) bool {
	read := false
	for _, x := range slices.Concat(w.All, w.Any) {
		read = x.anchorPR(t, armed) || read
	}
	if w.Not != nil {
		read = w.Not.anchorPR(t, armed) || read
	}
	if w.Kind != WhenCondition || !prFact(w.Type) {
		return read
	}
	c := FindChange(t, w.Change)
	if c == nil || c.PR == nil || c.PR.State != "OPEN" || c.PR.CheckedAt == nil {
		return read
	}
	armed["pr:"+c.ID] = c.PR.CheckedAt.UTC().Format(time.RFC3339Nano)
	return true
}

// readAfter: the PR was read from GitHub after the anchored read at.
func readAfter(p *PR, at string) bool {
	t0, err := time.Parse(time.RFC3339Nano, at)
	return err != nil || (p.CheckedAt != nil && p.CheckedAt.After(t0))
}

// changeDigest is what changeMoved compares: the change's phase, PR,
// conditions and evidence.
func changeDigest(c *Change) string {
	b, _ := json.Marshal([]any{c.Title, c.Phase, c.PRURL(), c.Conditions, c.Evidence})
	h := fnv.New64a()
	h.Write(b)
	return fmt.Sprintf("%016x", h.Sum64())
}

// changeOf is the change a Condition names first, or "".
func (w When) changeOf() string {
	if w.Change != "" {
		return w.Change
	}
	for _, x := range slices.Concat(w.All, w.Any) {
		if c := x.changeOf(); c != "" {
			return c
		}
	}
	return ""
}

// --- the Rule store ---

// ArmRule adds r to the thread, replacing the Rule with its id. The anchored
// kinds take their snapshot now; the scheduler evaluates it after the
// Input that armed it.
func ArmRule(t *Thread, r Rule, now time.Time) {
	t.Rules = armIn(t.Rules, t, r, now)
}

func armIn(rules []Rule, t *Thread, r Rule, now time.Time) []Rule {
	r.Status, r.ArmedAt, r.Armed = RuleArmed, now, map[string]string{}
	if r.After == "" {
		r.After = AfterDisarm
	}
	r.When.anchor(t, r.Armed)
	// changeMoved fires once at arming: the first sync is due.
	for k := range r.Armed {
		if strings.HasPrefix(k, "change:") {
			delete(r.Armed, k)
		}
	}
	if i := slices.IndexFunc(rules, func(x Rule) bool { return x.ID == r.ID }); i >= 0 {
		rules[i] = r
		return rules
	}
	return append(rules, r)
}

// FindRule is the thread's Rule with this id, or nil.
func FindRule(t *Thread, id string) *Rule { return findIn(t.Rules, id) }

func findIn(rules []Rule, id string) *Rule {
	for i := range rules {
		if rules[i].ID == id {
			return &rules[i]
		}
	}
	return nil
}

// DropRules disarms the thread's Rules that match.
func DropRules(t *Thread, match func(Rule) bool) {
	t.Rules = slices.DeleteFunc(t.Rules, match)
	if len(t.Rules) == 0 {
		t.Rules = nil
	}
}

// RulesOf are the thread's armed or suspended Rules of a purpose.
func RulesOf(t *Thread, purpose string) []Rule {
	var out []Rule
	for _, r := range t.Rules {
		if r.Purpose == purpose {
			out = append(out, r)
		}
	}
	return out
}

// suspendOn are the reasons that suspend a Rule of a purpose (7.4): the
// agent's watches wait while the thread is settled or archived; the deploy
// watch and the built-in actions only while it is archived. A settle Rule
// is cleared by settling, a snooze by settling and archiving.
func suspendOn(purpose string) []string {
	switch purpose {
	case PurposeWatch:
		return []string{SuspendSettle, SuspendArchive}
	case PurposeDeploy, PurposeBuiltins:
		return []string{SuspendArchive}
	}
	return nil
}

// suspend sets the reasons from the thread's state; a Rule whose last reason
// goes resumes (7.4): a state Condition first takes its current value, so a
// state that became true during the pause does not fire; an anchored one
// keeps its last value, so an event during the pause fires once.
func suspend(r *Rule, t *Thread, now time.Time) {
	var why []string
	for _, s := range suspendOn(r.Purpose) {
		if (s == SuspendSettle && t.Settle.At != nil) || (s == SuspendArchive && t.ArchivedAt != nil) {
			why = append(why, s)
		}
	}
	if autoSettle(*r) && t.PinnedAt != nil {
		why = append(why, SuspendPin)
	}
	was := len(r.SuspendedBy) > 0
	r.SuspendedBy = why
	if len(why) > 0 {
		r.Status = RuleSuspended
		return
	}
	r.Status = RuleArmed
	if was && !r.When.anchored() {
		r.LastValue = r.When.eval(t, r.Armed, now) == yes
	}
}

// NextCheck is the earliest time a Rule of the thread or the view may fire
// with time alone, or when the wake budget frees for a held wake, or nil.
// The engine's clock sends an Input then.
func NextCheck(t *Thread, v *View, now time.Time) *time.Time {
	// V0.95 (3): a wake the budget holds back is delivered when it frees.
	out := t.Feed.FreeAt(now)
	for _, rs := range [][]Rule{t.Rules, viewRules(v)} {
		for i := range rs {
			r := &rs[i]
			if r.Status != RuleArmed || r.LastValue {
				continue
			}
			at := r.When.nextCheck(t)
			if r.RetryAt != nil && r.RetryAt.After(now) {
				at = r.RetryAt
			}
			if at != nil && (out == nil || at.Before(*out)) {
				out = at
			}
		}
	}
	// V0.101: a failure waits StopAfter after the last run; the clock
	// re-reduces then, and the reduce turns Stopped on.
	if at := stopCheck(t); at != nil && (out == nil || at.Before(*out)) {
		out = at
	}
	return out
}

func viewRules(v *View) []Rule {
	if v == nil {
		return nil
	}
	return v.Rules
}

// EvalRules is the scheduler pass (7.3) on the records of one Input, in
// place: it suspends and resumes, then fires each Rule whose Condition went
// from false to true, applying its `then` through the Input kinds, until no
// Rule fires or maxFirings is reached (a loop between Rules ends there,
// with a rule.loop event).
func EvalRules(r Rec, now time.Time) Result {
	var res Result
	if r.V == nil {
		r.V = &View{}
	}
	r.T.Rules = slices.DeleteFunc(r.T.Rules, func(x Rule) bool { return x.About != "" && FindChange(r.T, x.About) == nil })
	for fired := 0; ; fired++ {
		id, onView := due(r, now)
		if id == "" {
			return res
		}
		if fired == maxFirings {
			res.Events = append(res.Events, Event{"rule.loop", map[string]any{"rule": id, "firings": fired}})
			return res
		}
		fire(r, id, onView, now, &res)
	}
}

// due evaluates every Rule after the last change and is the first one
// that fires now. The others keep their value, so each sees every change.
func due(r Rec, now time.Time) (string, bool) {
	id, onView := "", false
	for _, view := range []bool{false, true} {
		rules := r.T.Rules
		if view {
			rules = r.V.Rules
		}
		for i := range rules {
			x := &rules[i]
			if view && x.When.workBound() {
				continue // V0.185: wakeWhenDone, at the end of Reduce
			}
			suspend(x, r.T, now)
			if !view && x.When.doneBound() {
				continue // V0.203: settleWhenWorkDone, at the end of Reduce
			}
			if x.Status != RuleArmed || (x.RetryAt != nil && now.Before(*x.RetryAt)) {
				continue
			}
			switch x.When.eval(r.T, x.Armed, now) {
			case yes:
				if !x.LastValue && id == "" {
					id, onView = x.ID, view
				}
			case no:
				x.LastValue = false
			}
		}
	}
	return id, onView
}

// fire is one firing (7.3): count it, apply `then` as rule(id), then disarm
// or keep on ok, or count a rejection.
func fire(r Rec, id string, onView bool, now time.Time, res *Result) {
	find := func() *Rule {
		if onView {
			return findIn(r.V.Rules, id)
		}
		return FindRule(r.T, id)
	}
	x := find()
	x.Firings++
	x.LastValue = true
	rule := *x
	in := Input{Source: FromRule(id), Kind: rule.Then.Kind, Thread: r.T.ID, Args: Args(maps.Clone(rule.Then.Args))}
	t0, v0 := CloneThread(r.T), cloneView(r.V)
	out, err := applyThen(r, in, now)
	outcome := OutcomeOK
	if err != nil {
		*r.T, *r.V = *t0, *v0 // a rejected `then` changes nothing but the Rule
		outcome = OutcomeRejected
	}
	if x = find(); x != nil { // the `then` may have cleared it (settling clears every settle Rule)
		if err == nil {
			x.Rejections, x.RetryAt = 0, nil
			if x.Armed == nil {
				x.Armed = map[string]string{}
			}
			x.When.anchor(r.T, x.Armed)
		} else {
			x.LastValue = false
			x.Rejections++
			retry := now.Add(time.Duration(min(1<<x.Rejections, 60)) * time.Minute)
			x.RetryAt = &retry
		}
		if (err == nil && x.After == AfterDisarm) || x.Rejections >= maxRejections {
			if onView {
				r.V.Rules = slices.DeleteFunc(r.V.Rules, func(y Rule) bool { return y.ID == id })
			} else {
				DropRules(r.T, func(y Rule) bool { return y.ID == id })
			}
		}
	}
	if rule.Purpose != PurposeBuiltins { // a sync on every change move is not news
		data := map[string]any{"rule": id, "purpose": rule.Purpose, "then": rule.Then, "outcome": outcome}
		if err != nil {
			data["reason"] = err.Error()
		}
		res.Events = append(res.Events, Event{"rule.fired", data})
	}
	if err == nil {
		res.Events = append(res.Events, out.Events...)
		res.Effects = append(res.Effects, out.Effects...)
	}
}

func applyThen(r Rec, in Input, now time.Time) (Result, error) {
	k := Kinds[in.Kind]
	if k == nil || !slices.Contains(k.From, ByRule) {
		return Result{}, fmt.Errorf("a rule cannot send %s", in.Kind)
	}
	if k.Available != nil {
		if why := k.Available(r.T, r.V, now); why != "" {
			return Result{}, errors.New(why)
		}
	}
	return k.Reduce(r, in, now)
}

// --- snooze: a Rule on the owner's View (SPEC 7.5) ---

const snoozeRule = "snooze"

// Snooze hides the thread until `until`: a snooze Rule at(until) whose
// firing wakes it.
func Snooze(v *View, until, now time.Time) {
	v.Rules = armIn(v.Rules, &Thread{}, Rule{ID: snoozeRule, Purpose: PurposeSnooze, When: When{Kind: WhenAt, At: &until},
		Then: Then{Kind: KindRuleWake}, After: AfterDisarm, CreatedBy: ByOwner}, now)
}

// Unsnooze disarms the snooze.
func Unsnooze(v *View) {
	v.Rules = slices.DeleteFunc(v.Rules, func(r Rule) bool { return r.Purpose == PurposeSnooze })
	if len(v.Rules) == 0 {
		v.Rules = nil
	}
}

// SnoozeOf is the armed snooze's end and when it was armed, or nils.
func SnoozeOf(v *View) (until, at *time.Time) {
	if v == nil {
		return nil, nil
	}
	r := findIn(v.Rules, snoozeRule)
	if r == nil {
		return nil, nil
	}
	// V0.185: until work is done, the end is the Condition's maximum time.
	for _, w := range append([]When{r.When}, r.When.Any...) {
		if w.At != nil {
			u, a := *w.At, r.ArmedAt
			return &u, &a
		}
	}
	return nil, nil
}

// DecodeView reads a stored View, also in the shape stored before #46: the
// snooze in snoozeUntil and snoozedAt becomes its snooze Rule.
func DecodeView(b []byte) (*View, error) {
	v := &View{}
	if err := json.Unmarshal(b, v); err != nil {
		return nil, err
	}
	var old struct {
		SnoozeUntil *time.Time `json:"snoozeUntil"`
		SnoozedAt   *time.Time `json:"snoozedAt"`
	}
	if err := json.Unmarshal(b, &old); err != nil {
		return nil, err
	}
	if old.SnoozeUntil != nil && findIn(v.Rules, snoozeRule) == nil {
		at := v.SeenAt
		if old.SnoozedAt != nil {
			at = *old.SnoozedAt
		}
		Snooze(v, *old.SnoozeUntil, at)
	}
	return v, nil
}

// --- the agent's own Rules ---

// RuleArmArgs is the agent's watch tool: id is new (the engine makes it),
// then is "notify" or "settle", note the sentence a notify carries.
type RuleArmArgs struct {
	ID   string
	When When
	Then string
	Note string
}

// checkAgentWhen validates a Condition from the agent and resolves change
// refs to ids.
func checkAgentWhen(t *Thread, w *When, depth int) error {
	if depth > 3 {
		return errors.New("conditions nest at most 3 deep")
	}
	parts := 0
	for _, set := range [][]When{w.All, w.Any} {
		if len(set) > 0 {
			parts++
		}
		for i := range set {
			if err := checkAgentWhen(t, &set[i], depth+1); err != nil {
				return err
			}
		}
	}
	if w.Not != nil {
		parts++
		if err := checkAgentWhen(t, w.Not, depth+1); err != nil {
			return err
		}
	}
	if w.Kind != "" {
		parts++
	}
	if parts != 1 {
		return errors.New("a condition is one of: a kind with its params, any, all or not")
	}
	if w.Kind == "" {
		return nil
	}
	if !slices.Contains(AgentWhens, w.Kind) {
		return fmt.Errorf("kind must be %s", OrList(AgentWhens))
	}
	switch w.Kind {
	case WhenReached, WhenCondition:
		c, err := findChange(t, w.Change)
		if err != nil {
			return err
		}
		w.Change = c.ID
		if w.Kind == WhenReached && !w.Phase.Valid() {
			return fmt.Errorf("phase must be %s", OrList(Phases()))
		}
		if w.Kind == WhenCondition && (w.Type == "" || !w.Status.Valid()) {
			return fmt.Errorf("condition needs a type and a status (%s)", OrList(CondStatuses()))
		}
	case WhenAt:
		if w.At == nil {
			return errors.New("at needs a time (RFC 3339)")
		}
	case WhenIdleFor:
		if w.Ms <= 0 {
			return errors.New("idleFor needs ms > 0")
		}
	}
	return nil
}

func reduceRuleArm(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[RuleArmArgs](in)
	if err != nil {
		return Result{}, err
	}
	if err := checkAgentWhen(r.T, &a.When, 0); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrBadArgs, err)
	}
	n := 0
	for _, x := range r.T.Rules {
		if x.CreatedBy == ByAgent {
			n++
		}
	}
	if n >= maxAgentRules {
		return Result{}, fmt.Errorf("at most %d watches; remove one first", maxAgentRules)
	}
	rule := Rule{ID: a.ID, About: a.When.changeOf(), When: a.When, After: AfterDisarm, CreatedBy: ByAgent}
	switch a.Then {
	case "notify":
		rule.Purpose, rule.Then = PurposeWatch, Then{Kind: KindRuleNotify, Args: map[string]any{"note": strings.TrimSpace(a.Note)}}
	case "settle":
		rule.Purpose, rule.When, rule.Then = PurposeSettle, All(a.When, When{Kind: WhenIdle}), Then{Kind: KindRuleSettle}
	default:
		return Result{}, fmt.Errorf("%w: then must be notify or settle", ErrBadArgs)
	}
	ArmRule(r.T, rule, now)
	var effects []Effect
	if x := FindRule(r.T, rule.ID); x != nil && x.When.anchorPR(r.T, x.Armed) {
		rule = *x
		effects = []Effect{{Kind: EffectChangeRefresh}} // read the new head now
	}
	return Result{Events: []Event{{"rule.armed", map[string]any{"rule": rule}}}, Effects: effects, Reply: rule}, nil
}

func reduceRuleRemove(r Rec, in Input, _ time.Time) (Result, error) {
	id, _ := in.Args.(string)
	x := FindRule(r.T, id)
	if x == nil || x.CreatedBy != ByAgent {
		return Result{}, reject(RejectNotFound, "no watch %q of yours on this thread", id)
	}
	DropRules(r.T, func(y Rule) bool { return y.ID == id })
	return Result{Events: []Event{{"rule.removed", map[string]any{"rule": id, "by": ByAgent}}}}, nil
}

var fromRule = []string{ByRule}

func init() {
	for _, k := range []*InputKind{
		{Name: KindRuleArm, From: agentOnly, Reduce: reduceRuleArm},
		{Name: KindRuleRemove, From: agentOnly, Reduce: reduceRuleRemove},
		// The clock: Rules due at their NextCheck are evaluated after it.
		{Name: KindClock, From: observed, Untouched: true, Reduce: func(Rec, Input, time.Time) (Result, error) { return Result{}, nil }},
		{Name: KindRuleSettle, From: fromRule, Reduce: func(r Rec, _ Input, now time.Time) (Result, error) {
			settleNow(r.T, now)
			return Result{}, nil
		}},
		// The snooze's end: its firing disarms it, which wakes the thread.
		{Name: KindRuleWake, From: fromRule, Reduce: func(Rec, Input, time.Time) (Result, error) { return Result{}, nil }},
		// The agent feed's watch rule (feed.go) turns this firing into a
		// notice for the thread's agent; the rule.fired event carries the note.
		{Name: KindRuleNotify, From: fromRule, Reduce: func(Rec, Input, time.Time) (Result, error) { return Result{}, nil }},
		// A Rule whose armed time was the work (an observer reads while it is
		// armed): firing ends it.
		{Name: KindRuleEnd, From: fromRule, Reduce: func(Rec, Input, time.Time) (Result, error) { return Result{}, nil }},
		{Name: KindBuiltins, From: fromRule, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			if c := FindChange(r.T, mapArgs(in).Str("change")); c != nil {
				syncBuiltins(c)
			}
			return Result{}, nil
		}},
	} {
		RegisterKind(k)
	}
}
