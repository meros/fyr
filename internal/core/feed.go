package core

// The agent feed (decision 0020 part 5, #47): the only way Fyr speaks to the
// thread's agent. Notice rules project each reduce (its events and the
// records before and after) into Notices; the thread keeps one queue of
// them, merged by coalesce key; the agent manager takes them at three
// points: when the agent is idle (a message that starts a turn), at a tool
// call (context) and at the run's end (the Stop hook's one reason).

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// Urgency is when a notice reaches the agent.
type Urgency string

const (
	// Wake is delivered now: a turn starts when the agent is idle.
	Wake Urgency = "wake"
	// Next is delivered at the next tool call or turn; it never wakes an
	// idle agent (V0.95 (1), decision 0024 section 8).
	Next Urgency = "next"
	// Stop is delivered at the run's end and blocks the Stop once.
	Stop Urgency = "stop"
	// Silent is only in the log and in fyr_state: never queued.
	Silent Urgency = "silent"
)

// rank orders urgencies for a merge: the earlier delivery wins.
func (u Urgency) rank() int { return map[Urgency]int{Wake: 3, Next: 2, Stop: 1}[u] }

// Notice is one thing the agent is told: one sentence about one record.
type Notice struct {
	// About is the record: "thread", "view", "change:<id>" or "rule:<id>".
	About   string  `json:"about"`
	What    string  `json:"what"`
	Urgency Urgency `json:"urgency"`
	Key     string  `json:"coalesceKey"`
	// Subject and States make a coalesced line: "CI on PR #12: failed →
	// passed". A notice without a subject is replaced by a newer one.
	Subject string   `json:"subject,omitempty"`
	States  []string `json:"states,omitempty"`
	// Self: the agent's own Input caused it (no loop), or it needs nothing
	// from the agent (the owner's settle, V0.89), so it never wakes the
	// agent; it goes out with the next tool call or delivery.
	Self bool `json:"self,omitempty"`
	// Budget: an observer's or a Rule's wake, which counts against the
	// thread's wake budget (V0.95 (3)).
	Budget bool      `json:"budget,omitempty"`
	At     time.Time `json:"at"`
	// said is the dedupe key (change, field, head) of a fact notice, and
	// state what it says (V0.95 (2)); set by the rule, never stored.
	said, state string
}

func (n *Notice) render() {
	if n.Subject != "" {
		n.What = n.Subject + ": " + strings.Join(n.States, " → ") + "."
	}
}

// Feed is the thread's queue of notices not delivered yet.
type Feed struct {
	Queue []Notice `json:"queue,omitempty"`
	// Told is the last delivery per coalesce key: the interval of the
	// mid-turn reminders.
	Told map[string]time.Time `json:"told,omitempty"`
	// Said is the last state said per (change, field, head) (V0.95 (2)): a
	// run does not clear it; it keeps the newest maxSaid keys.
	Said map[string]Said `json:"said,omitempty"`
	// Woke are the times an observer's or a Rule's notice woke the agent
	// within the last wakeWindow (V0.95 (3)).
	Woke []time.Time `json:"woke,omitempty"`
}

// Said is one fact said to the agent: its state and when.
type Said struct {
	State string    `json:"state"`
	At    time.Time `json:"at"`
}

const (
	maxQueue = 50
	maxSaid  = 200
	// The wake budget (V0.95 (3)): at most wakeBudget wakes from observers
	// and Rules per wakeWindow.
	wakeBudget = 5
	wakeWindow = 10 * time.Minute
)

// said reports a fact notice said before on the same head with the same
// state, and otherwise records it.
func (f *Feed) said(n Notice, now time.Time) bool {
	if n.said == "" {
		return false
	}
	if s, ok := f.Said[n.said]; ok && s.State == n.state {
		return true
	}
	if f.Said == nil {
		f.Said = map[string]Said{}
	}
	f.Said[n.said] = Said{State: n.state, At: now}
	for len(f.Said) > maxSaid {
		old := ""
		for k, s := range f.Said {
			if old == "" || s.At.Before(f.Said[old].At) || (s.At.Equal(f.Said[old].At) && k < old) {
				old = k
			}
		}
		delete(f.Said, old)
	}
	return false
}

// woke are the budgeted wakes within the window before now.
func (f *Feed) woke(now time.Time) []time.Time {
	return slices.DeleteFunc(slices.Clone(f.Woke), func(at time.Time) bool { return now.Sub(at) >= wakeWindow })
}

// budgetLeft reports whether an observer's or a Rule's notice may wake the
// agent now.
func (f *Feed) budgetLeft(now time.Time) bool { return len(f.woke(now)) < wakeBudget }

// waking reports a notice that may start a turn now.
func (f *Feed) waking(n Notice, now time.Time) bool {
	return n.Urgency == Wake && !n.Self && (!n.Budget || f.budgetLeft(now))
}

// FreeAt is when the wake budget frees for a wake it holds back, or nil
// (V0.95 (3)): the thread's NextCheck, so the clock delivers it then.
func (f *Feed) FreeAt(now time.Time) *time.Time {
	held := slices.ContainsFunc(f.Queue, func(n Notice) bool { return n.Urgency == Wake && !n.Self && n.Budget })
	w := f.woke(now)
	if !held || len(w) < wakeBudget {
		return nil
	}
	slices.SortFunc(w, func(a, b time.Time) int { return a.Compare(b) })
	at := w[len(w)-wakeBudget].Add(wakeWindow)
	return &at
}

// Enqueue adds a notice, merging it into a pending one with its key.
func (f *Feed) Enqueue(n Notice) {
	if n.Urgency == Silent || n.Urgency == "" {
		return
	}
	n.render()
	for i := range f.Queue {
		q := &f.Queue[i]
		if q.Key != n.Key {
			continue
		}
		switch {
		case n.Urgency.rank() > q.Urgency.rank():
			q.Urgency, q.Budget = n.Urgency, n.Budget
		case n.Urgency == q.Urgency:
			q.Budget = q.Budget && n.Budget
		}
		q.Self = q.Self && n.Self
		q.At = n.At
		if q.Subject != "" && n.Subject == q.Subject {
			for _, s := range n.States {
				if len(q.States) == 0 || q.States[len(q.States)-1] != s {
					q.States = append(q.States, s)
				}
			}
			q.render()
		} else {
			q.What, q.Subject, q.States, q.About = n.What, n.Subject, n.States, n.About
		}
		return
	}
	f.Queue = keepLast(append(f.Queue, n), maxQueue)
}

// wakes reports a pending notice that may start a turn: only a wake, and
// an observer's or a Rule's only within the wake budget (V0.95 (1), (3)).
func (f *Feed) wakes(now time.Time) bool {
	return slices.ContainsFunc(f.Queue, func(n Notice) bool { return f.waking(n, now) })
}

// take removes and returns the queued notices that match.
func (f *Feed) take(match func(Notice) bool) []Notice {
	var out, keep []Notice
	for _, n := range f.Queue {
		if match(n) {
			out = append(out, n)
		} else {
			keep = append(keep, n)
		}
	}
	f.Queue = keep
	return out
}

// remember records the delivery time of each key during a run: the
// interval of the reminders. Older entries go.
func (f *Feed) remember(ns []Notice, run *time.Time, now time.Time) {
	if f.Told == nil {
		f.Told = map[string]time.Time{}
	}
	for _, n := range ns {
		f.Told[n.Key] = now
	}
	for k, at := range f.Told {
		if run == nil || at.Before(*run) {
			delete(f.Told, k)
		}
	}
	if len(f.Told) == 0 {
		f.Told = nil
	}
}

// Asleep reports a thread whose agent is not woken: settled or archived.
// Its notices wait until it is revived.
func (t *Thread) Asleep() bool { return t.Settle.At != nil || t.ArchivedAt != nil }

// wakeable reports a thread whose agent a notice may wake now: not asleep,
// and not moving to another profile (the handover's end wakes it).
func (t *Thread) wakeable() bool { return !t.Asleep() && t.Handover == nil }

// NoticeText is how notices read to the agent: one line each.
func NoticeText(ns []Notice) string {
	lines := make([]string, len(ns))
	for i, n := range ns {
		lines[i] = "[Fyr] " + n.What
	}
	return strings.Join(lines, "\n")
}

// --- the notice rules (0020, "Who is told what") ---

// Point is where the agent manager takes notices.
type Point string

const (
	AtIdle Point = "idle" // the agent is idle: a message that starts a turn
	AtTool Point = "tool" // after a tool call: context
	AtStop Point = "stop" // the run's end: the Stop hook's reason
	// AtTurn: a message that starts a turn (the owner's, another thread's)
	// carries every pending notice but the stop ones (V0.95 (1)).
	AtTurn Point = "turn"
)

// Reduced is what a notice rule reads of one reduce.
type Reduced struct {
	Before, After *Thread
	In            Input
	Events        []Event
}

// Taking is what a notice rule reads at a delivery point.
type Taking struct {
	T   *Thread
	At  Point
	Arg FeedTake
	Now time.Time
}

// NoticeRule is one row of the table: it projects a reduce (OnReduce) or a
// delivery point (OnTake) into notices.
type NoticeRule struct {
	Name    string
	Urgency Urgency
	// Budget: the rule speaks for observers or Rules, so its wakes count
	// against the wake budget when its Input is not the owner's or the
	// agent's (V0.95 (3)).
	Budget   bool
	OnReduce func(r Reduced) []Notice
	OnTake   func(x Taking) []Notice
}

// NoticeRules is the table; each row has a test (feed_test.go).
var NoticeRules = []NoticeRule{
	{Name: "result", Urgency: Wake, OnReduce: resultNotices},
	{Name: "watch", Urgency: Wake, Budget: true, OnReduce: watchNotices},
	{Name: "fact.blocks", Urgency: Wake, Budget: true, OnReduce: func(r Reduced) []Notice { return factNotices(r, true) }},
	{Name: "fact.moves", Urgency: Next, Budget: true, OnReduce: func(r Reduced) []Notice { return factNotices(r, false) }},
	{Name: "record", Urgency: Next, Budget: true, OnReduce: recordNotices},
	{Name: "upkeep.stop", Urgency: Stop, OnTake: upkeepStop},
	{Name: "upkeep.reminder", Urgency: Next, OnTake: upkeepReminder},
	{Name: "owner", Urgency: Next, OnReduce: ownerNotices},
	{Name: "slot", Urgency: Wake, OnReduce: slotNotices},
	{Name: "runrequest", Urgency: Next, OnReduce: runRequestNotices},
}

// EventSlot is the event of a slot for a heavy job that became the
// thread's (V0.86).
const EventSlot = "slot"

// SlotMemoryArgs is slot.memory's (decision 0029 S2): why memory is short
// and how many bytes the thread's job grew since its grant.
type SlotMemoryArgs struct {
	Why  string `json:"why"`
	Grew uint64 `json:"grew"`
}

// SlotIdleArgs is slot.idle's (#174): the held slot's kind and how many
// minutes it ran no heavy job.
type SlotIdleArgs struct {
	Kind    string `json:"kind"`
	Minutes int    `json:"minutes"`
}

// slot: a slot the agent queued for is its own now; it ended its turn to
// wait, so wake it. #174: a held slot that ran no heavy job for a while is
// a reminder at the next tool call, never a wake.
func slotNotices(r Reduced) []Notice {
	var out []Notice
	for _, ev := range r.Events {
		d, _ := ev.Data.(map[string]any)
		if ev.Type != EventSlot {
			continue
		}
		if why, ok := d["memory"].(string); ok {
			grew, _ := d["grew"].(uint64)
			out = append(out, Notice{About: "thread", Key: "slot.memory", Urgency: Next, What: fmt.Sprintf("Memory is short on this host (%s); your job grew %.1f GB since its slot was granted, the most of the running jobs. New heavy jobs wait until memory recovers; nothing is stopped. If you can, finish or shrink the job.", why, float64(grew)/1e9)})
			continue
		}
		if m, ok := d["idle"].(int); ok {
			out = append(out, Notice{About: "thread", Key: "slot.idle", Urgency: Next, What: fmt.Sprintf("You hold %s slot but no heavy job ran in it for %d min. If the job ended, call mcp__fyr__slot_release so another thread can run.", article(str(d["kind"])), m)})
			continue
		}
		if d["granted"] != true {
			continue
		}
		kind := str(d["kind"])
		a := "A "
		if strings.ContainsAny(kind[:min(1, len(kind))], "aeiou") {
			a = "An "
		}
		out = append(out, Notice{About: "thread", Key: "slot", What: a + kind + " slot is yours now: run the job, then call slot_release."})
	}
	return out
}

// project runs the reduce rules on one reduce and queues their notices. It
// returns the effect that wakes the agent when a notice may start a turn:
// a new one, a pending one on a thread that was just revived, or one the
// wake budget held back, at the clock (V0.95 (3)).
func project(r Reduced, now time.Time) []Effect {
	t := r.After
	if r.In.Source == FromOwner && r.In.Kind == "thread.send" {
		t.Feed.Woke = nil // V0.95 (3): the owner's message resets the budget
	}
	budget := r.In.Source != FromOwner && r.In.Source != FromAgent
	added := false
	for _, rule := range NoticeRules {
		if rule.OnReduce == nil {
			continue
		}
		for _, n := range rule.OnReduce(r) {
			if n.Urgency == "" {
				n.Urgency = rule.Urgency
			}
			if n.Urgency == Silent {
				continue
			}
			n.Self = n.Self || r.In.Source == FromAgent // the loop guard
			n.Budget = rule.Budget && budget
			n.At = now
			if t.Feed.said(n, now) {
				continue // V0.95 (2): said on this head already
			}
			t.Feed.Enqueue(n)
			added = added || t.Feed.waking(n, now)
		}
	}
	revived := !r.Before.wakeable() && t.wakeable()
	freed := r.In.Kind == KindClock
	if !t.wakeable() || !t.Feed.wakes(now) || !(added || revived || freed) {
		return nil
	}
	return []Effect{{Kind: EffectFeed}}
}

// result: the result of an effect the agent asked for, or of an action
// anyone pressed on its records (V0.60 (4)).
func resultNotices(r Reduced) []Notice {
	var out []Notice
	refused := false // V0.136: merge.refused is the one notice of a failed merge
	for _, ev := range r.Events {
		refused = refused || ev.Type == "merge.refused"
	}
	for _, ev := range r.Events {
		d, _ := ev.Data.(map[string]any)
		failedMerge := d["ok"] == false && (d["action"] == FyrMerge || d["action"] == "fyr.merge")
		switch {
		case refused && failedMerge && (ev.Type == "effect.result" || ev.Type == "action"):
		case ev.Type == "merge.refused":
			n := Notice{About: "thread", Key: "merge.refused:" + str(d["pr"]),
				What: fmt.Sprintf("The merge of %s failed: %s. Next: %s.", target(map[string]any{"pr": d["pr"]}), str(d["reason"]), str(d["next"]))}
			if c := str(d["change"]); c != "" {
				n.About = "change:" + c
			}
			out = append(out, n)
		case ev.Type == "action" && d["ended"] == true && d["run"] == RunAgent && d["ok"] == true && str(d["output"]) == "":
			// An agent action sent to this agent: the message itself is the news.
		case ev.Type == "action" && d["dismissed"] == true && d["ran"] != true:
			out = append(out, Notice{About: "thread", Key: fmt.Sprintf("result:thread:%v", d["action"]),
				What: fmt.Sprintf("The owner chose Not now for %q; do not ask again unless they ask.", str(d["why"]))})
		case ev.Type == "action" && d["dismissed"] == true:
		case ev.Type == "action" && d["ended"] == true && d["request"] == true:
			ok, _ := d["ok"].(bool)
			exit, _ := d["exit"].(int)
			root, _ := d["root"].(bool)
			out = append(out, Notice{About: "thread", Key: fmt.Sprintf("result:thread:%v", d["action"]),
				What: requestResultText(str(d["why"]), root, ok, exit, str(d["output"]))})
		case ev.Type == "action" && d["ended"] == true:
			ok, _ := d["ok"].(bool)
			exit, _ := d["exit"].(int)
			out = append(out, Notice{About: aboutOn(d["on"]), Key: fmt.Sprintf("result:%v:%v", d["on"], d["action"]),
				What: ranText(fmt.Sprint(d["label"]), fmt.Sprint(d["title"]), ok, exit, str(d["output"]))})
		case ev.Type == "decision.closed" && d["gate"] == true: // V0.136
			args, _ := d["args"].(map[string]any)
			n := Notice{About: "thread", Key: "result:" + str(d["action"]) + ":" + target(args),
				What: fmt.Sprintf("Fyr did not ask the owner to merge %s: %s. Next: %s; then ask again, and Fyr shows the ask when the PR can merge.", target(args), str(d["reason"]), str(d["next"]))}
			if c, _ := args["change"].(string); c != "" {
				n.About = "change:" + c
			}
			out = append(out, n)
		case ev.Type == "decision.closed" && d["void"] == true: // 0024 §5
			action := str(d["action"])
			args, _ := d["args"].(map[string]any)
			n := Notice{About: "thread", Key: "result:" + action + ":" + target(args),
				What: fmt.Sprintf("Your request %s on %s is void: %s. Do not ask again.", action, target(args), str(d["reason"]))}
			if c, _ := args["change"].(string); c != "" {
				n.About = "change:" + c
			}
			out = append(out, n)
		case ev.Type == "effect.result":
			ok, _ := d["ok"].(bool)
			action := str(d["action"])
			args, _ := d["args"].(map[string]any)
			n := Notice{About: "thread", Key: "result:" + action + ":" + target(args)}
			if c, _ := args["change"].(string); c != "" {
				n.About = "change:" + c
			}
			if d["by"] == "owner" {
				label := action
				if fx := FyrEffects[action]; fx != nil {
					label = fx.Label
				}
				n.What = ranText(label, target(args), ok, 0, str(d["output"]))
			} else {
				declined, _ := d["declined"].(bool)
				n.What = requestText(action, args, ok, declined, str(d["text"]))
			}
			out = append(out, n)
		}
	}
	return out
}

func str(v any) string { s, _ := v.(string); return s }

func aboutOn(on any) string {
	s := str(on)
	if s == OnThread || s == OnView || s == "" {
		return cmpOr(s, "thread")
	}
	return "change:" + s
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// detail is the first line of an output, short, after a colon.
func detail(out string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(out), "\n", 2)[0])
	if line == "" {
		return ""
	}
	return ": " + strings.TrimSuffix(TruncHead(line, 160), ".")
}

// ranText is the sentence for something the owner ran.
func ranText(label, title string, ok bool, exit int, out string) string {
	status := "succeeded"
	if !ok {
		status = "failed"
		if exit != 0 {
			status = fmt.Sprintf("failed (exit %d)", exit)
		}
	}
	return fmt.Sprintf("The owner ran %q on %s: it %s%s.", label, title, status, detail(out))
}

// requestText is the sentence for the result of the agent's request.
func requestText(action string, args map[string]any, ok, declined bool, text string) string {
	tool, what := "effect_request", action
	if ask, isAsk := strings.CutPrefix(action, "change."); isAsk {
		tool, what = "change_request", ask
	} else if _, ok := args["change"]; ok {
		tool = "change_request"
	}
	status := "failed"
	switch {
	case ok:
		status = "succeeded"
	case declined:
		status = "was declined by the owner"
	}
	return fmt.Sprintf("Your %s %s on %s %s%s.", tool, what, target(args), status, detail(text))
}

// target names what an effect acted on.
func target(args map[string]any) string {
	if t, _ := args["title"].(string); t != "" {
		return fmt.Sprintf("the change %q", t)
	}
	if url, _ := args["pr"].(string); url != "" {
		if _, n := parsePRURL(url); n != 0 {
			return fmt.Sprintf("PR #%d", n)
		}
	}
	return "this thread"
}

// watch: the agent's own watch fired (then notify).
func watchNotices(r Reduced) []Notice {
	var out []Notice
	for _, ev := range r.Events {
		d, _ := ev.Data.(map[string]any)
		if ev.Type != "rule.fired" || d["purpose"] != PurposeWatch || d["outcome"] != OutcomeOK {
			continue
		}
		id := str(d["rule"])
		note := ""
		if th, ok := d["then"].(Then); ok {
			note = strings.TrimSpace(str(th.Args["note"]))
		}
		what := "Your watch " + id + " fired"
		if note != "" {
			what += ": " + strings.TrimSuffix(note, ".")
		}
		out = append(out, Notice{About: "rule:" + id, Key: "rule:" + id, What: what + "."})
	}
	return out
}

// The condition types a fact notice is about: the ones Fyr observes from
// outside the agent's own checkout (pushed and clean are its own work).
func factType(typ CondType) bool {
	switch typ {
	case CondChecks, CondReview, CondConflict, CondDeploy:
		return true
	}
	return strings.HasPrefix(string(typ), condDeployPrefix)
}

// condWords are a condition's subject and its ok and fail words.
func condWords(typ CondType) (string, string, string) {
	switch typ {
	case CondChecks:
		return "CI", "passed", "failed"
	case CondReview:
		return "Review", "approved", "changes requested"
	case CondConflict:
		return "Merge conflicts", "resolved", "conflicting"
	case CondDeploy:
		return "Deploy", "deployed", "failed"
	}
	return "Deploy to " + strings.TrimPrefix(string(typ), condDeployPrefix), "succeeded", "failed"
}

// changeName names a change in a notice.
func changeName(c *Change) string {
	if c.PR != nil && c.PR.Number != 0 {
		return fmt.Sprintf("PR #%d", c.PR.Number)
	}
	return fmt.Sprintf("%q", c.Title)
}

// fact.blocks (blocks true): a fact that makes a change fail or block (CI
// failed, a conflict, changes requested, a deploy failed, closed). fact.moves
// (blocks false): a fact that moves it forward (checks passed, approved,
// merged, live). Only facts: what the agent reported itself is not news.
func factNotices(r Reduced, blocks bool) []Notice {
	var out []Notice
	for i := range r.After.Changes {
		c := &r.After.Changes[i]
		was := FindChange(r.Before, c.ID)
		about := "change:" + c.ID
		for _, k := range c.Conditions {
			if !factType(k.Type) || k.Source == ByAgent || (k.Type == CondDeploy && k.Status == CondOK) {
				continue // a production deploy that went ok is the phase live
			}
			var prev CondStatus
			if was != nil {
				if p := condition(was, k.Type); p != nil {
					prev = p.Status
				}
			}
			if k.Status == prev || k.Status == CondPending || (k.Status == CondFail) != blocks {
				continue
			}
			if blocks && c.Phase.Ended() {
				continue
			}
			subj, okWord, failWord := condWords(k.Type)
			state := okWord
			if k.Status == CondFail {
				state = failWord
			}
			key := "cond:" + c.ID + ":" + string(k.Type)
			out = append(out, Notice{About: about, Key: key, Subject: subj + " on " + changeName(c), States: []string{state}, said: key + "@" + changeHead(c), state: state})
		}
		var prevPhase Phase
		if was != nil {
			prevPhase = was.Phase
		}
		if c.Phase == prevPhase || c.PhaseBy == ByAgent || c.PhaseBy == ByOwner {
			continue // the owner's end is the owner rule's
		}
		state := ""
		switch {
		case blocks && c.Phase.Ended():
			state = string(c.Phase)
			if c.PR != nil && c.PR.State == "CLOSED" {
				state = "closed"
			}
			if c.PR != nil && c.PR.State == PRMissing {
				state = "does not exist on GitHub" // V0.72
			}
		case !blocks && c.Phase == PhaseLanded:
			state = string(PhaseLanded)
			if c.PR != nil && c.PR.State == "MERGED" {
				state = "merged"
			}
		case !blocks && c.Phase == PhaseLive:
			state = string(PhaseLive)
		}
		if state != "" {
			// One key per change: "PR #12: merged → live" is one line. An
			// end is done, not a failure: next (V0.95 (1)).
			key := "phase:" + c.ID
			out = append(out, Notice{About: about, Key: key, Subject: changeName(c), States: []string{state}, Urgency: Next, said: key + "@" + changeHead(c), state: state})
		}
	}
	return out
}

// record: a host record the thread shows merged or went (#35). A change of
// the thread that follows the record speaks for it (fact.*).
func recordNotices(r Reduced) []Notice {
	a, ok := r.In.Args.(ChangeObservedArgs)
	if r.In.Kind != KindChangeObserved || !ok || a.Before == nil || a.Before.Phase == a.After.Phase {
		return nil
	}
	id := a.After.ID
	if slices.ContainsFunc(r.After.Changes, func(c Change) bool { return c.Observed == id }) {
		return nil
	}
	state := ""
	switch a.After.Phase {
	case PhaseLanded:
		state = "merged"
	case PhaseAbandoned:
		state = "gone without a merge"
	default:
		return nil
	}
	key := "phase:" + id
	return []Notice{{About: "change:" + id, Key: key, Subject: "Branch " + a.After.Branch, States: []string{state}, said: key + "@" + changeHead(&a.After), state: state}}
}

// changeHead is what a fact about c is said on (V0.95 (2)): the PR's head
// commit, else the newest revision, else the head the observer stored.
func changeHead(c *Change) string {
	if c.PR != nil && c.PR.Head != "" {
		return c.PR.Head
	}
	return RecordHead(c)
}

// owner: a change by the owner to the thread's settings, project or settle
// state.
func ownerNotices(r Reduced) []Notice {
	if r.In.Source != FromOwner || r.In.Kind == "thread.create" {
		return nil
	}
	b, a := r.Before, r.After
	var out []Notice
	add := func(key, what string) { out = append(out, Notice{About: "thread", Key: "owner:" + key, What: what}) }
	if a.Title != b.Title {
		add("title", fmt.Sprintf("The owner renamed this thread to %q.", a.Title))
	}
	if a.ProjectID != b.ProjectID {
		if a.ProjectID == "" {
			add("project", "The owner took this thread out of its project.")
		} else {
			add("project", "The owner moved this thread to another project.")
		}
	}
	if a.ViewOn() != b.ViewOn() {
		if a.ViewOn() {
			add("view", ViewOnText)
		} else {
			add("view", "The owner turned the live view off.")
		}
	}
	settle := func(t *Thread) string {
		switch {
		case t.Settle.At != nil:
			return "settled"
		case SettlesWhenMerged(t):
			return "settles when its PR merges"
		case Settling(t):
			return "settles when its condition holds"
		}
		return "not settled"
	}
	if s := settle(a); s != settle(b) {
		// V0.89: a settle needs nothing from the agent, so it never wakes it
		// (settle when merged leaves the thread awake). Unsettle wakes.
		out = append(out, Notice{About: "thread", Key: "owner:settle", Subject: "The owner set this thread", States: []string{s}, Self: s != "not settled"})
	}
	for i := range a.Changes {
		c := &a.Changes[i]
		if was := FindChange(b, c.ID); c.PhaseBy == ByOwner && c.Phase.Ended() && (was == nil || !was.Phase.Ended()) {
			what := fmt.Sprintf("The owner ended %s as %s", changeName(c), c.Phase)
			if c.Label != "" {
				what += ": " + c.Label
			}
			add("end:"+c.ID, what+". Fyr no longer watches it; open a new change with another key if the work goes on.")
		}
		// V0.84: Not now and its Undo.
		was := FindChange(b, c.ID)
		switch {
		case c.Parked != nil && (was == nil || was.Parked == nil):
			add("park:"+c.ID, fmt.Sprintf("The owner set %s to Not now: it is not in Needs you now. It comes back by itself when its head changes or a new failure appears; do not ask about it again until then.", changeName(c)))
		case c.Parked == nil && was != nil && was.Parked != nil && r.In.Kind == KindChangeUnpark:
			add("park:"+c.ID, fmt.Sprintf("The owner took back Not now on %s; it needs the owner again.", changeName(c)))
		}
	}
	if (a.ArchivedAt != nil) != (b.ArchivedAt != nil) {
		s := "unarchived"
		if a.ArchivedAt != nil {
			s = "archived"
		}
		out = append(out, Notice{About: "thread", Key: "owner:archive", Subject: "The owner set this thread", States: []string{s}})
	}
	return out
}

// The upkeep sentences (SPEC 0.5 V0.37, V0.52). They name what to update
// and never dictate a reply (research/stop-hook-spike.md). V0.101: the
// changes are not upkeep any more; Fyr reads them.
const (
	// ViewOnText tells a running agent that the owner turned the view on
	// (V0.107): the view tools work at once; the view's prompt comes with
	// the next process, so the sentence carries the essentials.
	ViewOnText       = "The owner turned the live view on: publish it now with mcp__fyr__view_publish {html or path, state} (a small page pinned above the conversation that shows where your work stands; state.headline is its one-line status), then keep it current with view_state {patch} as items start and finish."
	ViewStopText     = "Update this thread's live view (view_state or view_publish), or call view_unchanged with a reason, then stop."
	ViewReminderText = "The live view has not changed for a few minutes while you work. Update it now with view_state (the headline and the items that moved), or call view_unchanged with a reason, then continue."
	// ReminderAfter is how long a running turn may go without touching its
	// view before the reminder.
	ReminderAfter = 4 * time.Minute
)

// upkeep.stop: the view the agent keeps was not touched in this run
// (V0.37).
func upkeepStop(x Taking) []Notice {
	if x.At != AtStop {
		return nil
	}
	var out []Notice
	if x.Arg.View && x.T.ViewOn() && !ViewTouchedThisRun(x.T) {
		out = append(out, Notice{About: "view", Key: "upkeep:view", What: ViewStopText})
	}
	return out
}

// upkeep.reminder: a running turn went ReminderAfter since the latest of the
// run start, the last touch and the last reminder (V0.52, V0.62).
func upkeepReminder(x Taking) []Notice {
	t := x.T
	if x.At != AtTool || t.RunStartedAt == nil {
		return nil
	}
	due := func(key string, checked time.Time) bool {
		last := *t.RunStartedAt
		for _, at := range []time.Time{checked, t.Feed.Told[key]} {
			if at.After(last) {
				last = at
			}
		}
		return x.Now.Sub(last) >= ReminderAfter
	}
	var out []Notice
	if x.Arg.View && t.ViewOn() {
		if due("upkeep:view", t.Live.CheckedAt) {
			out = append(out, Notice{About: "view", Key: "upkeep:view", What: ViewReminderText})
		}
	}
	return out
}

// --- taking notices ---

// KindFeedTake is the agent manager taking notices at a point.
const KindFeedTake = "feed.take"

// FeedTake is where the manager takes notices, and which tools the
// agent's process has (the upkeep asks only for what it can update).
type FeedTake struct {
	At            Point
	View, Changes bool
}

func init() {
	RegisterKind(&InputKind{Name: KindFeedTake, From: observed, Untouched: true, Reduce: reduceFeedTake})
}

// reduceFeedTake removes and returns what is delivered at the point: when
// idle, every pending notice when one may wake the agent (never on a
// settled or archived thread); at a tool call, all but the stop ones, and
// the reminders that are due; at the run's end, the stop ones and the
// upkeep.
func reduceFeedTake(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[FeedTake](in)
	if err != nil {
		return Result{}, err
	}
	t, f := r.T, &r.T.Feed
	var out []Notice
	switch a.At {
	case AtIdle:
		if !t.wakeable() || !f.wakes(now) {
			return Result{}, nil
		}
		out = f.take(func(Notice) bool { return true })
		// V0.95 (3): a turn that only an observer's or a Rule's notice
		// started is one wake of the budget.
		if !slices.ContainsFunc(out, func(n Notice) bool { return n.Urgency == Wake && !n.Self && !n.Budget }) {
			f.Woke = append(f.woke(now), now)
		}
	case AtTurn:
		out = f.take(func(n Notice) bool { return n.Urgency != Stop })
	case AtTool:
		out = f.take(func(n Notice) bool { return n.Urgency != Stop })
	case AtStop:
		out = f.take(func(n Notice) bool { return n.Urgency == Stop })
	default:
		return Result{}, fmt.Errorf("%w: no point %q", ErrBadArgs, a.At)
	}
	for _, rule := range NoticeRules {
		if rule.OnTake == nil {
			continue
		}
		for _, n := range rule.OnTake(Taking{T: t, At: a.At, Arg: a, Now: now}) {
			if n.Urgency == "" {
				n.Urgency = rule.Urgency
			}
			n.At = now
			out = append(out, n)
		}
	}
	if a.At == AtTool {
		f.remember(out, t.RunStartedAt, now)
	} else if a.At == AtIdle {
		f.Told = nil
	}
	if len(out) == 0 {
		return Result{}, nil
	}
	// The most urgent first: a result before the facts that moved.
	slices.SortStableFunc(out, func(x, y Notice) int { return y.Urgency.rank() - x.Urgency.rank() })
	return Result{Reply: out}, nil
}

// article is "a test", "an e2e".
func article(kind string) string {
	if kind != "" && strings.ContainsAny(kind[:1], "aeiou") {
		return "an " + kind
	}
	return "a " + kind
}
