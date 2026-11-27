package core

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

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
	{Name: "result.long", Urgency: Next, OnReduce: longRunNotices}, // V0.run-long-notice-next
	{Name: "watch", Urgency: Wake, Budget: true, OnReduce: watchNotices},
	{Name: "fact.blocks", Urgency: Wake, Budget: true, OnReduce: func(r Reduced) []Notice { return factNotices(r, true) }},
	{Name: "fact.moves", Urgency: Next, Budget: true, OnReduce: func(r Reduced) []Notice { return factNotices(r, false) }},
	{Name: "record", Urgency: Next, Budget: true, OnReduce: recordNotices},
	{Name: "upkeep.stop", Urgency: Stop, OnTake: upkeepStop},
	{Name: "upkeep.reminder", Urgency: Next, OnTake: upkeepReminder},
	{Name: "owner", Urgency: Next, OnReduce: ownerNotices},
	{Name: "slot", Urgency: Wake, OnReduce: slotNotices},
	{Name: "task.quiet", Urgency: Wake, Budget: true, OnReduce: taskQuietNotices}, // V0.195
	{Name: "task.stopped", Urgency: Next, OnReduce: taskStopNotices},              // V0.stop-ends-background
	{Name: "runrequest", Urgency: Next, OnReduce: runRequestNotices},
	{Name: "mergetext", Urgency: Wake, OnReduce: mergeTextNotices},          // V0.settle-merge-fixes (1)
	{Name: "overload.retry", Urgency: Wake, OnReduce: overloadRetryNotices}, // V0.overload-retry
	{Name: "quota.continue", Urgency: Wake, OnReduce: quotaContinueNotices}, // V0.quota-auto-continue
	{Name: "setup", Urgency: Next, OnReduce: setupNotices},                  // V0.prompt-fits-setup
	{Name: "noprogress", Urgency: Next, OnReduce: noProgressNotices},        // V0.no-progress
	{Name: "split", Urgency: Next, OnReduce: splitNotices},                  // V0.thread-focus
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

// SlotOverArgs is slot.over's (decision 0029 S3; V0.163: in % of the host): the job's class, its cost
// measured now and the cost Fyr predicted.
type SlotOverArgs struct {
	Class     string  `json:"class"`
	Live      float64 `json:"live"`
	Predicted float64 `json:"predicted"`
}

// frozenFor names how long the work was frozen (V0.guard-wake, decision 0036
// E6), so the agent can tell a timeout the freeze caused from a real failure.
func frozenFor(d map[string]any) string {
	f := str(d["frozen"])
	if f == "" {
		return ""
	}
	return " It was frozen for " + f + "; a timeout or a slow step in that time came from the freeze, not from the work."
}

// slot: a job the agent was refused for a full host is admitted now; it ended
// its turn to wait, so wake it.
func slotNotices(r Reduced) []Notice {
	var out []Notice
	for _, ev := range r.Events {
		d, _ := ev.Data.(map[string]any)
		if ev.Type != EventSlot {
			continue
		}
		if txt, ok := d["leftovers"].(string); ok { // V0.176
			out = append(out, Notice{About: "thread", Key: "leftovers", Urgency: Next, What: txt + ". Clean up what you start, prefer a timeout, and for a process that must stay call mcp__fyr__keep_process."})
			continue
		}
		if why, ok := d["paused"].(string); ok && str(d["work"]) != "" {
			// V0.admission-tidy: no "ask with slot_acquire": V0.209 admits by itself.
			out = append(out, Notice{About: "thread", Key: "slot.pause", Urgency: Next, What: fmt.Sprintf("Fyr paused your undeclared heavy work (`%s`): memory is short on this host (%s). Its processes are frozen, not killed, and resume when memory has recovered for 30 s, or after 10 min unless memory still stalls critically then. A timeout inside the job may fire after it resumes; check its result.", str(d["work"]), why)})
			continue
		}
		if d["waits"] == true { // V0.oomd-guard (decision 0036 E2)
			what := "Your heavy job"
			if d["work"] == true {
				what = "Your heavy work"
			}
			out = append(out, Notice{About: "thread", Key: "slot.pause", Urgency: Next, What: what + " stays frozen past 10 min: memory still stalls critically on this host, and thawing it now would make that worse. It resumes when the stall eases; Fyr tells you then."})
			continue
		}
		if class, ok := d["killed"].(string); ok { // V0.rerun-classes (decision 0036 (F))
			what := "heavy job"
			if d["work"] == true {
				what = "heavy work"
			}
			out = append(out, Notice{About: "thread", Key: "slot.pause", Urgency: Next, What: fmt.Sprintf("Fyr killed your %s (`%s`) to free memory on this host: the owner marked it rerunnable. Its failure or exit 137 came from that, not from your change. Run the command again: Fyr admits it once memory has recovered and the job fits.", what, class)})
			continue
		}
		if cmd, ok := d["ended"].(string); ok { // V0.oomd-frozen-work
			what := "heavy job"
			if d["work"] == true {
				what = "heavy work"
			}
			out = append(out, Notice{About: "thread", Key: "slot.pause", Urgency: Next, What: fmt.Sprintf("Fyr killed your %s (`%s`): the agents' memory stalled past systemd-oomd's limit, and oomd would have ended this whole thread, you with it, within seconds. Its failure or exit 137 came from that, not from your change. Run it again when you need it: Fyr admits it once memory has recovered and it fits.", what, cmd)})
			continue
		}
		if d["retry"] == true { // V0.guard-wake (decision 0036 E1)
			out = append(out, Notice{About: "thread", Key: "slot.pause", Urgency: Wake, What: HeldRetryText(str(d["agent"]), str(d["agentType"]))})
			continue
		}
		if d["resumed"] == true && d["work"] == true {
			out = append(out, Notice{About: "thread", Key: "slot.pause", Urgency: Next, What: "Your heavy work runs again: Fyr thawed it." + frozenFor(d)})
			continue
		}
		if why, ok := d["paused"].(string); ok {
			out = append(out, Notice{About: "thread", Key: "slot.pause", Urgency: Next, What: fmt.Sprintf("Fyr paused your heavy job: memory is short on this host (%s). Its processes are frozen, not killed, and resume when memory has recovered for 30 s, or after 10 min unless memory still stalls critically then. A timeout inside the job may fire after it resumes; check its result.", why)})
			continue
		}
		if d["resumed"] == true {
			out = append(out, Notice{About: "thread", Key: "slot.pause", Urgency: Next, What: "Your heavy job runs again: Fyr thawed it." + frozenFor(d)})
			continue
		}
		if class, ok := d["over"].(string); ok {
			live, _ := d["live"].(float64)
			pred, _ := d["predicted"].(float64)
			out = append(out, Notice{About: "thread", Key: "slot.over", Urgency: Next, What: fmt.Sprintf("Your job (%s) uses ≈ %.0f %% of this host; Fyr expected ≈ %.0f %%. Other heavy jobs wait until it ends; nothing is stopped. Fyr learns the new cost from this run.", class, live, pred)})
			continue
		}
		if why, ok := d["memory"].(string); ok {
			grew, _ := d["grew"].(uint64)
			out = append(out, Notice{About: "thread", Key: "slot.memory", Urgency: Next, What: fmt.Sprintf("Memory is short on this host (%s); your job grew %.1f GB since its slot was granted, the most of the running jobs. New heavy jobs wait until memory recovers; nothing is stopped. If you can, finish or shrink the job.", why, float64(grew)/1e9)})
			continue
		}
		if d["granted"] != true {
			continue
		}
		out = append(out, Notice{About: "thread", Key: "slot", What: AdmittedText(str(d["kind"]), str(d["agent"]), str(d["agentType"]))})
	}
	return out
}

// AdmittedText is the line that a queued heavy job is admitted now. With no
// agent it is for the actor that was refused, the thread's own agent or the
// subagent itself at its next tool call; with one it tells the thread's agent
// that the subagent it names was refused the job and has ended without
// running it (V0.subagent-admission).
func AdmittedText(kind, agent, agentType string) string {
	if agent == "" {
		return "Your " + kind + " job is admitted now: run the command again."
	}
	return "The " + kind + " job your subagent " + SubagentName(agent, agentType) + " was queued for is admitted now, but that subagent has ended without running it: run the command again yourself if you still need its result."
}

// HeldRetryText is the line that memory has recovered for a heavy command
// the gate held, worded as AdmittedText (V0.subagent-admission).
func HeldRetryText(agent, agentType string) string {
	if agent == "" {
		return "Memory has recovered on this host: run the heavy command Fyr held again."
	}
	return "Memory has recovered on this host, so the heavy command Fyr held for your subagent " + SubagentName(agent, agentType) + " can run now, but that subagent has ended without running it: run it again yourself if you still need its result."
}

// SubagentName names a subagent as the agent knows it: its type and its id.
func SubagentName(agent, agentType string) string {
	if agentType == "" {
		return agent
	}
	return agentType + " (" + agent + ")"
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
	revived := !r.Before.wakeableAt(now) && t.wakeableAt(now)
	freed := r.In.Kind == KindClock
	if !t.wakeableAt(now) || !t.Feed.wakes(now) || !(added || revived || freed) {
		return nil
	}
	return []Effect{{Kind: EffectFeed}}
}

// result.long: a pressed run request still runs after LongRunAfter. It
// never starts a turn (V0.run-long-notice-next): it goes with the next
// delivery, and the result, under the same key, replaces it while queued
// (V0.run-long-notice (3)).
func longRunNotices(r Reduced) []Notice {
	var out []Notice
	for _, ev := range r.Events {
		if ev.Type != EventActionLong {
			continue
		}
		d, _ := ev.Data.(map[string]any)
		root, _ := d["root"].(bool)
		out = append(out, Notice{About: "thread", Key: fmt.Sprintf("result:thread:%v", d["action"]),
			What: requestLongText(str(d["why"]), root)})
	}
	return out
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
		case ev.Type == "decision.closed" && d["cancelled"] == true && d["asked"] == true: // V0.ask-cancel
			out = append(out, Notice{About: "thread", Key: "result:ask:" + str(d["id"]), What: askCancelText(str(d["answer"]))})
		case ev.Type == "decision.closed" && d["asked"] == true: // V0.ask-tool
			out = append(out, Notice{About: "thread", Key: "result:ask:" + str(d["id"]), What: askAnswerText(str(d["answer"]))})
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
	what := action // V0.one-tool-vocabulary: every ask is a change_request
	if ask, isAsk := strings.CutPrefix(action, "change."); isAsk {
		what = ask
	}
	status := "failed"
	switch {
	case ok:
		status = "succeeded"
	case declined:
		status = "was declined by the owner"
	}
	return fmt.Sprintf("Your change_request %s on %s %s%s.", what, target(args), status, detail(text))
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
		if d["by"] == ByOwner {
			what = "The quota wake fired"
			if win := str(d["window"]); win != "" {
				what = "The " + win + " quota window reset"
			}
		}
		if note != "" {
			what += ": " + strings.TrimSuffix(note, ".")
		}
		out = append(out, Notice{About: "rule:" + id, Key: "rule:" + id, What: what + "."})
	}
	return out
}

// quota.continue: Fyr automatically continued a quota-stopped thread after its
// reset time passed (V0.quota-auto-continue).
func quotaContinueNotices(r Reduced) []Notice {
	var out []Notice
	for _, ev := range r.Events {
		if ev.Type != "quota.continued" {
			continue
		}
		d, _ := ev.Data.(map[string]any)
		what := "The provider quota has reset. Continue from where you stopped."
		if msg := strings.TrimSpace(str(d["message"])); msg != "" {
			what = "The provider quota has reset (the last run stopped with: " + msg + "). Continue from where you stopped."
		}
		out = append(out, Notice{About: "thread", Key: "quota.continue", What: what})
	}
	return out
}

// overload.retry: Fyr retried a thread a passing provider failure stopped
// (V0.overload-retry).
func overloadRetryNotices(r Reduced) []Notice {
	var out []Notice
	for _, ev := range r.Events {
		if ev.Type != "overload.retried" {
			continue
		}
		d, _ := ev.Data.(map[string]any)
		what := "The last run stopped on a provider error; this is an automatic retry. Continue from where you stopped."
		if msg := strings.TrimSpace(str(d["message"])); msg != "" {
			what = "The last run stopped on a provider error (" + msg + "); this is an automatic retry. Continue from where you stopped."
		}
		out = append(out, Notice{About: "thread", Key: "overload.retry", What: what})
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
			said := state
			if threadsOnly(&k) {
				// V0.review-thread-notice: name the count; threads resolved
				// and new ones left on the same head are news again.
				state, said = k.Reason, k.Reason+"@"+k.At.UTC().Format(time.RFC3339Nano)
			}
			// V0.project-playbook: a stage reached says where to look at it.
			if env, ok := strings.CutPrefix(string(k.Type), condDeployPrefix); ok && k.Status == CondOK {
				for _, ev := range c.Evidence {
					if ev.Kind == "deployment" && ev.Title == env && ev.URL != "" {
						subj += " (" + ev.URL + ")"
						break
					}
				}
			}
			key := "cond:" + c.ID + ":" + string(k.Type)
			out = append(out, Notice{About: about, Key: key, Subject: subj + " on " + changeName(c), States: []string{state}, said: key + "@" + changeHead(c), state: said})
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
	if a.Title != b.Title && b.TitleAuto != TitleAutoEmpty { // a thread started empty is named by its first message, not by the owner
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
	if r.In.Kind == KindCancelWake { // V0.wakeups-via-watch; a firing in the same reduce is not a cancel
		out = append(out, cancelledWakes(b, a)...)
	}
	if r.In.Kind == KindCancelWait { // V0.waits-strip
		out = append(out, cancelledWaits(b, a, time.Now())...)
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
// and never dictate a reply. V0.101: the
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
		if !t.wakeableAt(now) || !f.wakes(now) {
			return Result{}, nil
		}
		out = f.take(func(Notice) bool { return true })
		// V0.95 (3): a turn that only an observer's or a Rule's notice
		// started is one wake of the budget.
		if !slices.ContainsFunc(out, func(n Notice) bool { return n.Urgency == Wake && !n.Self && !n.Budget }) {
			f.Woke = append(f.woke(now), now)
		}
		// V0.thread-claims: the notices start a turn, so the thread moves
		// from the take, not from the turn's first report; until now the row
		// read Finished in between.
		markStarting(t, now)
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
