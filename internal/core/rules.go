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
	KindQuotaSync  = "quota.sync"
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
	ObsQuota       = "quota"
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
	Kind     string     `json:"kind,omitempty"`
	Change   string     `json:"change,omitempty"`
	Phase    Phase      `json:"phase,omitempty"`
	Type     CondType   `json:"type,omitempty"`
	Status   CondStatus `json:"status,omitempty"`
	At       *time.Time `json:"at,omitempty"`
	Ms       int64      `json:"ms,omitempty"`
	Provider string     `json:"provider,omitempty"`
	Window   string     `json:"window,omitempty"`
	Any      []When     `json:"any,omitempty"`
	All      []When     `json:"all,omitempty"`
	Not      *When      `json:"not,omitempty"`
}

// The Condition kinds. Each reads the thread record only.
const (
	WhenRunEnded  = "runEnded"      // anchored: a run ended after the Rule was armed
	WhenReached   = "changeReached" // {change, phase}: the change got at least that far
	WhenCondition = "condition"     // {change, type, status}: a condition of the change has that status
	WhenAt        = "at"            // anchored: {at}: now ≥ at
	WhenQuota     = "quota"         // anchored: {provider, window, at}: now ≥ the provider window's reset time (V0.quota-reset-wake)
	WhenIdleFor   = "idleFor"       // {ms}: nothing happened on the thread for ms
	WhenIdle      = "idle"          // no run is live and the last one did not fail
	WhenMoved     = "changeMoved"   // anchored: {change}: the change's phase, conditions or evidence moved since the last firing
	WhenEnded     = "changeEnded"   // {change}: the change ended (abandoned or reverted)
	WhenWorkOver  = "workOver"      // V0.185: no work goes on, or the thread needs the owner
	WhenWorkDone  = "workDone"      // V0.203: no work goes on, nothing waits for the owner, no PR is open
	WhenNoOpenPR  = "noOpenPR"      // V0.settle-merge-fixes (2): no PR of the thread is open
)

// AgentWhens are the kinds the agent may use in its own Rules.
// workDone only alone and with then settle (V0.one-tool-vocabulary: it was
// settle_when done).
var AgentWhens = []string{WhenRunEnded, WhenReached, WhenCondition, WhenAt, WhenQuota, WhenIdleFor, WhenWorkDone}

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
	case WhenAt, WhenQuota:
		return truth(w.At != nil && !now.Before(*w.At))
	case WhenIdleFor:
		return truth(now.Sub(t.ActivityAt) >= time.Duration(w.Ms)*time.Millisecond)
	case WhenIdle:
		// V0.settle-waits-unread-message: a message the agent has not read is a
		// run about to start, so the thread is not idle yet.
		return truth(!t.Running && t.Starting == nil && t.Failure == nil && len(t.Outbox) == 0)
	case WhenWorkOver:
		return truth(WorkOverAt(t, now))
	case WhenWorkDone:
		return truth(WorkDoneAt(t, now))
	case WhenNoOpenPR:
		return truth(!OpenPR(t))
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

// walk calls f on w and then on every Condition it holds (All, Any, Not),
// depth first, and reports true at the first f that does
// (V0.one-predicate (2)): the one way to look through a Condition's
// structure, so no reader skips a branch.
func (w When) walk(f func(When) bool) bool {
	if f(w) {
		return true
	}
	for _, x := range w.All {
		if x.walk(f) {
			return true
		}
	}
	for _, x := range w.Any {
		if x.walk(f) {
			return true
		}
	}
	return w.Not != nil && w.Not.walk(f)
}

// has reports a Condition of this kind anywhere in w.
func (w When) has(kinds ...string) bool {
	return w.walk(func(x When) bool { return slices.Contains(kinds, x.Kind) })
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
	w.walk(func(x When) bool {
		switch x.Kind {
		case WhenAt, WhenQuota:
			take(x.At)
		case WhenIdleFor:
			at := t.ActivityAt.Add(time.Duration(x.Ms) * time.Millisecond)
			take(&at)
		}
		return false
	})
	return out
}

// anchored reports a Condition made true by an event after the Rule was
// armed (7.2), not by the current state.
func (w When) anchored() bool {
	return w.has(WhenRunEnded, WhenAt, WhenQuota, WhenMoved)
}

// anchor takes the snapshot of the anchored kinds in w.
func (w When) anchor(t *Thread, armed map[string]string) {
	w.walk(func(x When) bool {
		switch x.Kind {
		case WhenRunEnded:
			armed["run"] = strconv.Itoa(t.RunEnds)
		case WhenMoved:
			if c := FindChange(t, x.Change); c != nil {
				armed["change:"+c.ID] = changeDigest(c)
			}
		}
		return false
	})
}

// prFact: a condition the PR poller reads from GitHub, about the PR's head.
func prFact(typ CondType) bool {
	return typ == CondChecks || typ == CondReview || typ == CondConflict
}

// anchorPR anchors each PR fact the agent's watch reads to the PR read the
// change holds now (V0.checks-follow-head, #120): the watch holds only on a
// later read: one GitHub was asked for after now, and after the read the
// change holds. A watch armed right after a push would otherwise fire on
// the previous head's checks before the poller has read the new head. Only
// a PR Fyr polls is anchored, so a watch never waits on a read that does
// not come. It reports whether the watch reads one, so Fyr reads the PR at
// once.
func (w When) anchorPR(t *Thread, armed map[string]string, now time.Time) bool {
	read := false
	w.walk(func(x When) bool {
		if x.Kind != WhenCondition || !prFact(x.Type) {
			return false
		}
		c := FindChange(t, x.Change)
		if c == nil || c.PR == nil || c.PR.State != "OPEN" || c.PR.CheckedAt == nil || !slices.Contains(PRURLs(t, now), c.PRURL()) {
			return false
		}
		at := now
		if c.PR.CheckedAt.After(at) {
			at = *c.PR.CheckedAt
		}
		armed["pr:"+c.ID] = at.UTC().Format(time.RFC3339Nano)
		read = true
		return false
	})
	return read
}

// readAfter: the PR was read from GitHub after the anchored read at.
func readAfter(p *PR, at string) bool {
	t0, err := time.Parse(time.RFC3339Nano, at)
	return err != nil || (p.CheckedAt != nil && p.CheckedAt.After(t0))
}

// changeDigest is what changeMoved compares: the change's phase, PR,
// conditions and evidence.
func changeDigest(c *Change) string {
	// An empty list is nil: dropConditions leaves an empty slice, a record
	// saved and loaded again (omitempty) holds nil, and the digest of a
	// change that did not move must not differ (V0.rule-digest-stable).
	conds, ev := c.Conditions, c.Evidence
	if len(conds) == 0 {
		conds = nil
	}
	if len(ev) == 0 {
		ev = nil
	}
	b, _ := json.Marshal([]any{c.Title, c.Phase, c.PRURL(), conds, ev})
	h := fnv.New64a()
	h.Write(b)
	return fmt.Sprintf("%016x", h.Sum64())
}

// changeOf is the change a Condition names first, or "".
func (w When) changeOf() string {
	out := ""
	w.walk(func(x When) bool {
		out = x.Change
		return out != ""
	})
	return out
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

// unwatchable reports a Rule whose Condition its change can no longer make
// true: a checks, review or conflict condition of a change that has left the
// in-flight phases (V0.watch-stale), because landing or ending drops those
// conditions (decision 0018). Compound All/Any conditions (such as a settle
// rule wrapping WhenCondition with WhenIdle) are checked recursively. Every
// other Condition kind is readable after landing (a phase, a move, the run
// count), so it stays.
func unwatchable(t *Thread, x Rule) bool {
	return unwatchableWhen(t, x.When)
}

func unwatchableWhen(t *Thread, w When) bool {
	// This is the Condition's logic, not a walk: All is stuck when one part
	// is, Any when every part is, and Not(x) of a stuck x is true from now
	// on, which fires rather than waits, so it is never stuck.
	if w.Not != nil {
		return false
	}
	if len(w.All) > 0 {
		return slices.ContainsFunc(w.All, func(x When) bool { return unwatchableWhen(t, x) })
	}
	if len(w.Any) > 0 {
		for _, x := range w.Any {
			if !unwatchableWhen(t, x) {
				return false
			}
		}
		return true
	}
	if w.Kind != WhenCondition || !prFact(w.Type) {
		return false
	}
	c := FindChange(t, w.Change)
	return c != nil && !c.Phase.InFlight()
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
	if oneShotWatch(*r) {
		// V0.settle-merge-fixes (3): a watch that fires once fires when it
		// resumes on a state that came true in the pause. A firing drops it
		// and a rejection clears the value, so an armed one never keeps a
		// true value: one stored with it (the old resume) fires now.
		r.LastValue = false
		return
	}
	if was && !r.When.anchored() {
		r.LastValue = r.When.eval(t, r.Armed, now) == yes
	}
}

// oneShotWatch is the agent's or the owner's watch that is dropped when it
// fires.
func oneShotWatch(r Rule) bool { return r.Purpose == PurposeWatch && r.After == AfterDisarm }

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
	// V0.quota-wait: the clock re-reduces at the reset the notice names, so
	// a thread Waiting for quota becomes the owner's again by itself.
	if at := quotaCheck(t, now); at != nil && (out == nil || at.Before(*out)) {
		out = at
	}
	// V0.no-progress: the clock re-reduces a live run when its silence
	// reaches NoProgressAfter.
	if at := noProgressCheck(t, now); at != nil && (out == nil || at.Before(*out)) {
		out = at
	}
	return out
}

// quotaCheck is when the clock must re-reduce a thread Waiting for quota so
// Fyr automatically continues it after the reset + buffer (V0.quota-auto-continue),
// or nil when auto-continue does not apply.
func quotaCheck(t *Thread, _ time.Time) *time.Time {
	if !QuotaCanAutoContinue(t) {
		return nil
	}
	return QuotaResumeAt(t)
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
// with a rule.loop event; the limit is per Rule).
func EvalRules(r Rec, now time.Time) Result {
	var res Result
	if r.V == nil {
		r.V = &View{}
	}
	r.T.Rules = slices.DeleteFunc(r.T.Rules, func(x Rule) bool { return x.About != "" && FindChange(r.T, x.About) == nil })
	// V0.watch-stale: a watch on a condition its change no longer reports can
	// never fire (the condition is dropped when the change lands or ends, as
	// checks, review and conflict are, decision 0018). Drop it rather than
	// leave it armed forever. The watcher's own thread is told; a dropped Rule
	// does not act.
	r.T.Rules = slices.DeleteFunc(r.T.Rules, func(x Rule) bool { return unwatchable(r.T, x) })
	// V0.rule-fanout: the limit counts one Rule's firings, not all of them, so
	// an Input that moves many changes fires each one's Rule once; a loop
	// between Rules still stops, as each of them reaches the limit.
	fired := map[string]int{}
	for {
		id, onView := due(r, now)
		if id == "" {
			return res
		}
		if fired[id] == maxFirings {
			res.Events = append(res.Events, Event{"rule.loop", map[string]any{"rule": id, "firings": fired[id]}})
			return res
		}
		fired[id]++
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
		data := map[string]any{"rule": id, "purpose": rule.Purpose, "then": rule.Then, "outcome": outcome, "by": rule.CreatedBy}
		if q := quotaWhen(rule.When); q != nil {
			data["provider"], data["window"] = q.Provider, q.Window
			if q.At != nil {
				data["at"] = *q.At
			}
		}
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
	if w.Kind == WhenWorkDone && depth > 0 {
		return errors.New("workDone stands alone, with then settle")
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
		// V0.watch-stale: a change that has landed or ended drops checks,
		// review and conflict (decision 0018), so a watch on one of those can
		// never fire. Refuse it rather than arm a watch that only looks armed.
		if w.Kind == WhenCondition && prFact(w.Type) && !c.Phase.InFlight() {
			return fmt.Errorf("%s no longer reports %s: the change is %s", c.Key, w.Type, c.Phase)
		}
		if w.Kind == WhenCondition && (w.Type == "" || !w.Status.Valid()) {
			return fmt.Errorf("condition needs a type and a status (%s)", OrList(CondStatuses()))
		}
	case WhenAt:
		if w.At == nil {
			return errors.New("at needs a time (RFC 3339)")
		}
	case WhenQuota:
		if strings.TrimSpace(w.Provider) == "" {
			return errors.New("quota needs a provider")
		}
		if w.At == nil {
			return errors.New("quota window has no known reset time")
		}
	case WhenIdleFor:
		if w.Ms <= 0 {
			return errors.New("idleFor needs ms > 0")
		}
	}
	return nil
}

func reduceRuleArm(r Rec, in Input, now time.Time) (Result, error) {
	if r.T.ArchivedAt != nil {
		return Result{}, reject(RejectUnavailable, "the thread is archived")
	}
	if r.T.Settle.At != nil {
		return Result{}, reject(RejectUnavailable, "the thread is already settled")
	}
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
	if a.When.Kind == WhenWorkDone { // V0.one-tool-vocabulary: settle_when done
		if a.Then != "settle" {
			return Result{}, fmt.Errorf("%w: workDone is for then settle", ErrBadArgs)
		}
		SettleWhenDone(r.T, ByAgent, now)
		rule := *FindRule(r.T, settleDoneRule)
		return Result{Events: []Event{{"rule.armed", map[string]any{"rule": rule}}}, Reply: rule}, nil
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
	if x := sameRule(r.T, rule); x != nil { // a retry: the watch is armed already
		return Result{Reply: *x}, nil
	}
	ArmRule(r.T, rule, now)
	var effects []Effect
	if x := FindRule(r.T, rule.ID); x != nil && x.When.anchorPR(r.T, x.Armed, now) {
		rule = *x
		effects = []Effect{{Kind: EffectChangeRefresh}} // read the new head now
	}
	return Result{Events: []Event{{"rule.armed", map[string]any{"rule": rule}}}, Effects: effects, Reply: rule}, nil
}

// sameRule is the agent's armed Rule that watches the same condition with
// the same outcome as r, or nil (V0.one-tool-vocabulary): an agent that
// retries a watch whose reply it lost gets the watch it armed.
func sameRule(t *Thread, r Rule) *Rule {
	key := func(x Rule) string {
		b, _ := json.Marshal([]any{x.Purpose, stripQuotaAt(x.When), x.Then})
		return string(b)
	}
	want := key(r)
	for i := range t.Rules {
		x := &t.Rules[i]
		if x.CreatedBy != ByAgent || x.Status != RuleArmed {
			continue
		}
		if key(*x) == want {
			return x
		}
	}
	return nil
}

// stripQuotaAt removes the resolved reset timestamp from WhenQuota nodes so
// retrying the same quota watch matches even if ResetsAt drifted slightly.
func stripQuotaAt(w When) When {
	if w.Kind == WhenQuota {
		w.At = nil
	}
	if len(w.All) > 0 {
		w.All = slices.Clone(w.All)
		for i := range w.All {
			w.All[i] = stripQuotaAt(w.All[i])
		}
	}
	if len(w.Any) > 0 {
		w.Any = slices.Clone(w.Any)
		for i := range w.Any {
			w.Any[i] = stripQuotaAt(w.Any[i])
		}
	}
	if w.Not != nil {
		n := stripQuotaAt(*w.Not)
		w.Not = &n
	}
	return w
}

// quotaWhen returns the first WhenQuota node inside w, or nil.
func quotaWhen(w When) *When {
	if w.Kind == WhenQuota {
		return &w
	}
	for _, x := range slices.Concat(w.All, w.Any) {
		if q := quotaWhen(x); q != nil {
			return q
		}
	}
	if w.Not != nil {
		return quotaWhen(*w.Not)
	}
	return nil
}

// QuotaUpdate is one provider window's freshly observed reset time
// (V0.quota-reset-wake).
type QuotaUpdate struct {
	Provider string
	Window   string
	ResetsAt time.Time
}

// QuotaSyncArgs carries fresh quota reset times into a thread so its
// WhenQuota rules follow a reset time that moved.
type QuotaSyncArgs struct {
	Updates []QuotaUpdate
}

// maxQuotaDrift bounds how far forward a fresh quota read may push an armed
// reset time: a jump larger than an hour means a new window already started,
// not drift of the armed window's reset.
const maxQuotaDrift = time.Hour

func syncQuotaWhen(w *When, u QuotaUpdate, now time.Time) bool {
	changed := false
	for i := range w.All {
		if syncQuotaWhen(&w.All[i], u, now) {
			changed = true
		}
	}
	for i := range w.Any {
		if syncQuotaWhen(&w.Any[i], u, now) {
			changed = true
		}
	}
	if w.Not != nil && syncQuotaWhen(w.Not, u, now) {
		changed = true
	}
	if w.Kind == WhenQuota && w.Provider == u.Provider && w.Window == u.Window && w.At != nil {
		if now.Before(*w.At) && !u.ResetsAt.Equal(*w.At) && u.ResetsAt.Sub(*w.At) <= maxQuotaDrift {
			at := u.ResetsAt.UTC()
			w.At = &at
			changed = true
		}
	}
	return changed
}

func reduceQuotaSync(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[QuotaSyncArgs](in)
	if err != nil {
		return Result{}, err
	}
	// V0.review-correctness (5): a suspended rule follows too, so it does not
	// resume with an earlier reset and fire early.
	for i := range r.T.Rules {
		for _, u := range a.Updates {
			syncQuotaWhen(&r.T.Rules[i].When, u, now)
		}
	}
	return Result{}, nil
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
		// V0.quota-reset-wake: follow a moving quota window reset time.
		{Name: KindQuotaSync, From: observed, Untouched: true, Reduce: reduceQuotaSync},
		{Name: KindRuleSettle, From: fromRule, Reduce: func(r Rec, _ Input, now time.Time) (Result, error) {
			settleNow(r.T, now)
			Unsnooze(r.V) // SPEC 7.4: settling clears the snooze
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
