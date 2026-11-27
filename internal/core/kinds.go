package core

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode/utf8"
)

// The Input kinds of the thread record besides the owner's commands (which
// register() adds): what the agent's tools, the observers and the owner's
// presses send (decision 0020). Each is a guard and a reducer; the engine's
// one path stores and broadcasts the result.

// The kinds' names.
const (
	KindRecord         = "record"
	KindThreadRename   = "thread.retitle"
	KindChangeRequest  = "change.request"
	KindSettleRequest  = "settle.request" // V0.settle-request
	KindChangeOpen     = "change.open"
	KindChangeUpdate   = "change.update" // V0.101: status, revision and proof in one
	KindChangeEnd      = "change.end"    // the owner's command (V0.72)
	KindChangePark     = "change.park"   // the owner's Not now (V0.84)
	KindChangeUnpark   = "change.unpark" // its Undo (V0.84)
	KindActionOffer    = "action.offer"
	KindActionPress    = "action.run"
	KindViewPublish    = "view.publish"
	KindViewState      = "view.state"
	KindViewUnchanged  = "view.unchanged"
	KindPRObserved     = "pr.observed"
	KindIssueObserved  = "issue.observed" // V0.thread-issues: the poller's read of a linked issue
	KindIssueLink      = "issue.link"     // V0.thread-issues: the agent's issue_link
	KindDeployObserved = "deploy.observed"
	KindSelfDeployed   = "deploy.self" // V0.82
	KindHostRestarted  = "host.restarted"
	KindMessageSent    = "message.sent"
	KindMessageState   = "message.state"
	KindSendFailed     = "send.failed"
	KindRunStarted     = "run.started"
	KindRunEnded       = "run.ended"
	KindProcessLost    = "process.lost"
	// V0.143: the agent process stops or starts taking what Fyr writes,
	// and Fyr restarts it.
	KindUnresponsive = "agent.unresponsive"
	KindResponsive   = "agent.responsive"
	KindRestarted    = "agent.restarted"
	// V0.153: Fyr waits for a thrashing host ("" Why: it no longer waits).
	KindMachineWait      = "agent.machineWait"
	KindGateHold         = "agent.gateHold" // V0.gate-hold-visible
	KindFrozen           = "agent.frozen"   // V0.freeze-visible
	KindTask             = "task"
	KindTaskQuiet        = "task.quiet" // V0.195: a background command waits while the thread uses no CPU
	KindSession          = "session"
	KindDigest           = "digest"
	KindDecisionOpened   = "decision.opened"
	KindDecisionGone     = "decision.withdrawn"
	KindHandoverFailed   = "handover.failed"
	KindHandoverDone     = "handover.done"
	KindThreadHandedOver = "thread.handedOver" // decision 0039, V0.trans-host-handover
	KindSlotGranted      = "slot.granted"      // V0.86: a queued slot is the thread's now
	KindSlotQueued       = "slot.queued"       // V0.185: the thread joined or left the slot queue
	KindSlotMemory       = "slot.memory"       // decision 0029 S2: memory is short; this job grew most
	KindSlotOver         = "slot.over"         // decision 0029 S3: a running job is heavier than predicted
	KindSlotPause        = "slot.pause"        // decision 0029 S5: the memory guard froze or thawed this job
)

// The typed args of the kinds that are not a map.
type (
	// ChangeRequest is the agent's change_request: a Decision for the
	// owner. FyrRepo: the thread works on Fyr's own repo (the engine reads
	// it), the only place a fyr.deploy may be asked (V0.one-tool-vocabulary).
	ChangeRequest struct {
		ID, Change, Reason string
		Ask                ChangeAsk
		Args               map[string]any
		FyrRepo            bool
	}
	// ChangeRequestReply is what change_request returns: the change it asks
	// about (nil for a deploy that names none), the ask's id, and Again when
	// an identical ask was still open, so a retry adds nothing
	// (V0.one-tool-vocabulary).
	ChangeRequestReply struct {
		Change *Change
		ID     string
		Again  bool
	}
	// ChangeOpenArgs is change_open; PR is its canonical PR URL or "".
	ChangeOpenArgs struct {
		Open ChangeOpen
		PR   string
	}
	// ChangeReply is what a change kind returns: a copy of the change, and
	// whether the change or the revision is new.
	ChangeReply struct {
		Change Change
		New    bool
		// Notes are for the agent: the value Fyr reads of an observed field
		// it sent, or that a PR it abandoned is still open (V0.101).
		Notes []string
	}
	// ChangeUpdateArgs is change_update (V0.101): the report, a new
	// revision and proof, each optional.
	ChangeUpdateArgs struct {
		Change   string
		Update   ChangeUpdate
		Revision *Revision
		Evidence []Evidence
	}
	OfferArgs struct {
		On     string
		Spec   ActionSpec
		Remove bool
	}
	OfferReply struct {
		Label, Where string
		Removed      bool
		// Replaced: the whys of the earlier cards this run request replaced
		// (V0.run-card-replaces).
		Replaced []string
	}
	ViewPublishArgs struct {
		PageID string
		State  map[string]any
	}
	DeployObservedArgs struct {
		Change string
		Obs    DeployObs
		// Probes: the thread's project has stage probes Fyr runs
		// (V0.project-playbook), so a repo without any GitHub deployment
		// keeps its watch.
		Probes bool
	}
	// SelfDeployedArgs are the changes of a thread whose merge commit is in
	// the build a self-deploy made live (V0.82).
	SelfDeployedArgs struct {
		Build   string
		Changes []string
	}
	MessageSentArgs struct {
		ID    string
		First bool
		Log   []Event
		// Text is what went to the CLI (V0.143), kept in the outbox.
		Text string
	}
	// UnresponsiveArgs: since the process's last output, and why (V0.143).
	UnresponsiveArgs struct {
		Since  time.Time
		Reason string
	}
	// MachineWaitArgs: why Fyr waits for the machine; "" ends the wait.
	MachineWaitArgs struct{ Why string }
	// GateHoldArgs: why the gate holds a heavy command back; "" ends the hold.
	GateHoldArgs struct{ Why string }
	// FrozenArgs: a freeze of the thread's work (Key "job" or "work") and
	// why; "" ends that freeze.
	FrozenArgs struct{ Key, Why string }
	// SlotGrantedArgs: the kind of a queued job admitted now, and the
	// subagent that was refused it when that subagent has ended
	// (V0.subagent-admission); a plain string is the kind alone.
	SlotGrantedArgs struct{ Kind, Agent, AgentType string }
	// RestartedArgs: why Fyr restarted the process and the background
	// tasks that ended with it (V0.143).
	RestartedArgs struct {
		Why   string
		Tasks []string
	}
	MessageStateArgs struct{ ID, State string }
	SendFailedArgs   struct {
		Message string
		Log     []Event
	}
	RunEndedArgs struct {
		Failure *Failure
		Git     GitFacts
		// Summary, when set, is kept as the thread's summary (V0.37).
		Summary *string
		Data    map[string]any
	}
	TaskArgs struct {
		Task Task
		Log  []Event
		// ByStop: the task ended as stopped right after the owner's Stop,
		// whose interrupt the CLI applies to every background task too
		// (V0.stop-ends-background).
		ByStop bool `json:",omitempty"`
	}
	// TaskQuietArgs is task.quiet's (V0.195): the tasks that sit quiet and
	// since when. No Since: the thread uses CPU again.
	TaskQuietArgs struct {
		IDs   []string
		Since *time.Time
	}
	SessionArgs struct{ SessionID, Model string }
	// DigestArgs is one stream message's digest (agent/digest.go).
	DigestArgs struct {
		Activity, Summary string
		Subagents         int
		PR                *PR
		PRs               []PR
		// Issues are the issue URLs gh issue create printed (V0.thread-issues).
		Issues []string
		Log    []Event
		// OpenBash is how many Bash calls run now, any agent's; nil: the
		// message says nothing of it (V0.no-progress).
		OpenBash *int
	}
	HandoverDoneArgs struct {
		Profile, Model, SessionID string
		// Effort is the thread's effort from now on (V0.model-choice); nil keeps it.
		Effort *string
		Data   map[string]any
		// HostNotice, when non-empty, is queued into Feed with urgency Next
		// so a resumed session learns about a cross-host move on its next
		// turn (decision 0039, V0.trans-host-handover).
		HostNotice string
		// FirstMessage: Fyr sends the new session its first message right
		// after this (V0.handover-first-message-starting). The thread is
		// Starting from here, so a settle Rule that waits for the work to be
		// done does not settle it before that message starts the run.
		FirstMessage bool
	}
)

func init() {
	for _, k := range []*InputKind{
		{Name: KindRecord, From: anyone, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
			evs, err := argsOf[[]Event](in)
			return Result{Events: evs}, err
		}},
		{Name: KindChangeRequest, From: agentOnly, Reduce: reduceChangeRequest},
		{Name: KindSettleRequest, From: agentOnly, Reduce: reduceSettleRequest},
		{Name: KindChangeOpen, From: agentOnly, Reduce: changeKind("open", func(t *Thread, in Input, now time.Time) (*Change, bool, error) {
			a, err := argsOf[ChangeOpenArgs](in)
			if err != nil {
				return nil, false, err
			}
			if err := codeOnly(&a); err != nil {
				return nil, false, err
			}
			c, made, err := OpenChange(t, a.Open, ByAgent, now)
			if err != nil {
				return nil, false, err
			}
			if a.PR != "" {
				repo, n := parsePRURL(a.PR)
				pr := PR{URL: a.PR, Repo: repo, Number: n, State: "OPEN", Checks: ChecksPending, Branch: c.Branch}
				group := c.Group
				if t.MainPR() == nil {
					// The first PR is also the thread's main PR (the PR tab, the merge gate).
					PRObserved(t, pr, now)
				} else if cur := changeByPR(t, a.PR); cur == nil || cur.PR == nil || cur.PR.CheckedAt == nil {
					if pc := ChangePRObserved(t, pr, now); pc != nil && pc.Group == "" && group != "" {
						pc.Group = group
					}
				}
				if updated := FindChange(t, c.ID); updated != nil {
					c = updated
				}
			}
			return c, made, nil
		})},
		{Name: KindChangeUpdate, From: agentOnly, Reduce: changeKindNotes("update", reportChange)},
		{Name: KindActionOffer, From: agentOnly, Reduce: reduceOffer},
		{Name: KindActionPress, From: ownerOnly, Reduce: reducePress},
		{Name: KindViewPublish, From: agentOnly, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[ViewPublishArgs](in)
			if err != nil {
				return Result{}, err
			}
			if err := CheckViewState(a.State); err != nil {
				return Result{}, err
			}
			ViewPublished(r.T, a.PageID, a.State, now)
			return Result{Events: []Event{{"view.published", map[string]any{"pageId": a.PageID, "state": a.State}}}}, nil
		}},
		{Name: KindViewState, From: agentOnly, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			patch, err := argsOf[map[string]any](in)
			if err != nil {
				return Result{}, err
			}
			if r.T.Live == nil || r.T.Live.PageID == "" {
				return Result{}, errors.New("this thread has no view page yet; publish one with view_publish first")
			}
			next, _ := MergePatch(r.T.Live.State, patch).(map[string]any)
			if next == nil {
				next = map[string]any{}
			}
			if err := CheckViewState(next); err != nil {
				return Result{}, err
			}
			ViewStateSet(r.T, next, now)
			return Result{Events: []Event{{"view.state", map[string]any{"state": next}}}}, nil
		}},
		{Name: KindViewUnchanged, From: agentOnly, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			reason, _ := in.Args.(string)
			if strings.TrimSpace(reason) == "" {
				return Result{}, errors.New("give a reason")
			}
			ViewChecked(r.T, now)
			return Result{Events: []Event{{"view.unchanged", map[string]any{"reason": reason}}}}, nil
		}},
		{Name: KindPRObserved, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			p, err := argsOf[PR](in)
			if err == nil {
				ChangePRObserved(r.T, p, now) // #32: the PR lives on its change
				linkIssuesOf(r.T, p, now)     // V0.thread-issues
			}
			return Result{}, err
		}},
		{Name: KindDeployObserved, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[DeployObservedArgs](in)
			if err == nil {
				ChangeDeployObserved(r.T, a.Change, a.Obs, a.Probes, now)
			}
			return Result{}, err
		}},
		{Name: KindSelfDeployed, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[SelfDeployedArgs](in)
			if err == nil {
				ChangesSelfDeployed(r.T, a, now)
			}
			return Result{}, err
		}},
		{Name: KindHostRestarted, From: observed, Untouched: true, Reduce: reduceRestarted},
		{Name: KindSlotQueued, From: observed, Untouched: true, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			r.T.SlotQueued, _ = in.Args.(bool)
			return Result{}, nil
		}},
		{Name: KindSlotGranted, From: observed, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			// The kind alone, or SlotGrantedArgs when a subagent that has
			// ended was refused the job (V0.subagent-admission).
			a, ok := in.Args.(SlotGrantedArgs)
			if !ok {
				a.Kind, _ = in.Args.(string)
			}
			if a.Kind == "" {
				return Result{}, errors.New("slot.granted needs the kind")
			}
			r.T.SlotQueued = false
			d := map[string]any{"kind": a.Kind, "granted": true}
			if a.Agent != "" {
				d["agent"], d["agentType"] = a.Agent, a.AgentType
			}
			return Result{Events: []Event{{EventSlot, d}}}, nil
		}},
		{Name: KindSlotMemory, From: observed, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
			a, err := argsOf[SlotMemoryArgs](in)
			if err != nil || a.Why == "" {
				return Result{}, errors.New("slot.memory needs the reason")
			}
			return Result{Events: []Event{{EventSlot, map[string]any{"memory": a.Why, "grew": a.Grew}}}}, nil
		}},
		{Name: KindSlotOver, From: observed, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
			a, err := argsOf[SlotOverArgs](in)
			if err != nil || a.Class == "" {
				return Result{}, errors.New("slot.over needs the class")
			}
			return Result{Events: []Event{{EventSlot, map[string]any{"over": a.Class, "live": a.Live, "predicted": a.Predicted}}}}, nil
		}},
		{Name: KindSlotPause, From: observed, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			d := mapArgs(in)
			_, p := d["paused"].(string)
			_, k := d["killed"].(string)
			if _, e := d["ended"].(string); e { // V0.oomd-frozen-work
				k = true
			}
			if !p && !k && d["resumed"] != true && d["waits"] != true && d["retry"] != true {
				return Result{}, errors.New("slot.pause needs paused, resumed, waits, retry, killed or ended")
			}
			if k || d["resumed"] == true {
				unfreeze(r.T, FreezeKey(d["work"])) // V0.freeze-visible
			}
			return Result{Events: []Event{{EventSlot, map[string]any(d)}}}, nil
		}},
		{Name: KindMessageSent, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[MessageSentArgs](in)
			if err != nil {
				return Result{}, err
			}
			if a.First {
				RunStarted(r.T, now)
			}
			SetOutbox(r.T, a.ID, "sent")
			if a.Text != "" {
				SetOutboxText(r.T, a.ID, a.Text)
			}
			return Result{Events: a.Log}, nil
		}},
		{Name: KindMessageState, From: observed, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			a, err := argsOf[MessageStateArgs](in)
			if err != nil {
				return Result{}, err
			}
			SetOutbox(r.T, a.ID, a.State)
			return Result{Events: []Event{{"message.state", map[string]any{"id": a.ID, "state": a.State}}}}, nil
		}},
		{Name: KindSendFailed, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[SendFailedArgs](in)
			if err != nil {
				return Result{}, err
			}
			// V0.thread-invariants: clear any message marked lost/cancelled/read in the failure log.
			for _, ev := range a.Log {
				if ev.Type != "message.state" {
					continue
				}
				if d, ok := ev.Data.(map[string]any); ok {
					if id, _ := d["id"].(string); id != "" {
						if st, _ := d["state"].(string); st != "" && st != "sent" && st != "queued" {
							SetOutbox(r.T, id, st)
						}
					}
				}
			}
			RunEnded(r.T, &Failure{Class: "process", Message: a.Message, At: now}, now)
			return Result{Events: a.Log}, nil
		}},
		{Name: KindRunStarted, From: observed, Reduce: func(r Rec, _ Input, now time.Time) (Result, error) {
			// V0.settled-self-turn: the CLI of a settled thread lives on for
			// its background work and can start a turn by itself; that run
			// is work again, so the thread is not settled any more.
			if r.T.Settle.At != nil {
				r.T.Settle.At = nil
				dropSettleDone(r.T)
			}
			RunStarted(r.T, now)
			return Result{Events: []Event{{"run.started", map[string]any{"by": "agent"}}}}, nil
		}},
		{Name: KindRunEnded, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[RunEndedArgs](in)
			if err != nil {
				return Result{}, err
			}
			data := a.Data
			if a.Failure != nil {
				f := *a.Failure
				f.At = now
				a.Failure = &f
				if _, ok := data["failure"]; ok {
					data = maps.Clone(data)
					data["failure"] = a.Failure
				}
			}
			RunEnded(r.T, a.Failure, now)
			if stopped, _ := data["interrupted"].(bool); stopped && a.Failure == nil && r.V != nil {
				// V0.thread-claims: the owner stopped the run (Stop, or a
				// settle), so its end is not news to them: Finished, seen.
				r.V.SeenAt = now
			}
			r.T.Branch = a.Git.Branch
			if a.Summary != nil {
				r.T.Summary = *a.Summary
			}
			// V0.59 (3), #35: the git observer reads the folder and each
			// change's workspace into host records.
			git := a.Git
			return Result{Events: []Event{{"run.ended", data}}, Effects: []Effect{{Kind: EffectGit, Git: &git}}}, nil
		}},
		{Name: KindProcessLost, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			msg, _ := in.Args.(string)
			wasFail := r.T.Failure
			var ran []Task
			for _, k := range r.T.Tasks {
				if k.Status == TaskRunning {
					ran = append(ran, k)
				}
			}
			ProcessLost(r.T, "process", msg, now)
			// V0.idle-exit-ends-tasks: the timeline says which tasks ended
			// with the process, also when no run did (an idle process killed
			// while it held background work), so the stop is never silent.
			var evs []Event
			for _, k := range ran {
				evs = append(evs, Event{"task", map[string]any{"kind": "ended", "id": k.ID, "description": k.Description, "status": TaskStopped, "summary": "ended with the agent process"}})
			}
			if r.T.Failure != nil && r.T.Failure != wasFail {
				evs = append(evs, Event{"run.ended", map[string]any{"failure": map[string]any{"class": "process", "message": msg}}})
			}
			return Result{Events: evs}, nil
		}},
		{Name: KindUnresponsive, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[UnresponsiveArgs](in)
			if err != nil {
				return Result{}, err
			}
			r.T.Unresponsive = &Unresponsive{Since: a.Since, At: now, Reason: a.Reason}
			r.T.ActivityAt = now
			return Result{Events: []Event{{"agent.unresponsive", map[string]any{"since": a.Since, "reason": a.Reason}}}}, nil
		}},
		{Name: KindResponsive, From: observed, Reduce: func(r Rec, _ Input, _ time.Time) (Result, error) {
			if r.T.Unresponsive == nil {
				return Result{}, nil
			}
			r.T.Unresponsive = nil
			return Result{Events: []Event{{"agent.responsive", map[string]any{}}}}, nil
		}},
		{Name: KindMachineWait, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[MachineWaitArgs](in)
			if err != nil {
				return Result{}, err
			}
			if a.Why == "" {
				if r.T.MachineWait == nil {
					return Result{}, nil
				}
				r.T.MachineWait = nil
				return Result{Events: []Event{{"agent.machineWait", map[string]any{"done": true}}}}, nil
			}
			if r.T.Asleep() {
				return Result{}, nil
			}
			if r.T.MachineWait != nil && r.T.MachineWait.Why == a.Why {
				return Result{}, nil
			}
			r.T.MachineWait = &MachineWait{Since: now, Why: a.Why}
			return Result{Events: []Event{{"agent.machineWait", map[string]any{"why": a.Why}}}}, nil
		}},
		{Name: KindGateHold, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[GateHoldArgs](in)
			if err != nil {
				return Result{}, err
			}
			if a.Why == "" {
				if r.T.GateHold == nil {
					return Result{}, nil
				}
				r.T.GateHold = nil
				return Result{Events: []Event{{"agent.gateHold", map[string]any{"done": true}}}}, nil
			}
			if r.T.Asleep() || (r.T.GateHold != nil && r.T.GateHold.Why == a.Why) {
				return Result{}, nil
			}
			r.T.GateHold = &GateHold{Since: now, Why: a.Why}
			return Result{Events: []Event{{"agent.gateHold", map[string]any{"why": a.Why}}}}, nil
		}},
		{Name: KindFrozen, From: observed, Reduce: reduceFrozen},
		{Name: KindRestarted, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[RestartedArgs](in)
			if err != nil {
				return Result{}, err
			}
			Restarted(r.T, now)
			d := map[string]any{"why": a.Why}
			if len(a.Tasks) > 0 {
				d["tasks"] = a.Tasks
			}
			return Result{Events: []Event{{"agent.restarted", d}}}, nil
		}},
		{Name: KindTask, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[TaskArgs](in)
			if err != nil {
				return Result{}, err
			}
			TaskObserved(r.T, a.Task, now)
			return Result{Events: a.Log}, nil
		}},
		{Name: KindTaskQuiet, From: observed, Untouched: true, Reduce: reduceTaskQuiet},
		{Name: KindSession, From: observed, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			a, err := argsOf[SessionArgs](in)
			if err != nil {
				return Result{}, err
			}
			if a.SessionID != "" {
				r.T.SessionID = a.SessionID
			}
			if a.Model != "" && r.T.Model == "" {
				r.T.Model = a.Model
			}
			return Result{}, nil
		}},
		{Name: KindDigest, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[DigestArgs](in)
			if err != nil {
				return Result{}, err
			}
			t := r.T
			if a.Activity != "" && t.Running {
				t.Activity = a.Activity
			}
			if a.Summary != "" {
				t.Summary = a.Summary
			}
			t.Subagents += a.Subagents
			prs := a.PRs
			if len(prs) == 0 && a.PR != nil {
				prs = []PR{*a.PR}
			}
			for _, pr := range prs {
				if p := t.MainPR(); p == nil || p.URL != pr.URL {
					PRObserved(t, pr, now)
				}
			}
			for _, u := range a.Issues { // V0.thread-issues
				LinkIssue(t, Issue{URL: u, State: "OPEN", Via: IssueViaCreated}, now)
			}
			return Result{Events: a.Log}, nil
		}},
		{Name: KindDecisionOpened, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			d, err := argsOf[Decision](in)
			if err != nil {
				return Result{}, err
			}
			d.CreatedAt = now
			DecisionOpened(r.T, d)
			return Result{Events: []Event{{"decision.opened", map[string]any{"decision": d}}}}, nil
		}},
		{Name: KindDecisionGone, From: observed, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			id, _ := in.Args.(string)
			DecisionClosed(r.T, id)
			return Result{Events: []Event{{"decision.closed", map[string]any{"id": id, "answer": "withdrawn"}}}}, nil
		}},
		{Name: KindHandoverFailed, From: observed, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			msg, _ := in.Args.(string)
			r.T.Handover = nil
			return Result{Events: []Event{{"effect.error", map[string]any{"kind": "handover", "error": msg}}}}, nil
		}},
		{Name: KindHandoverDone, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[HandoverDoneArgs](in)
			if err != nil {
				return Result{}, err
			}
			t := r.T
			t.Profile, t.Model, t.SessionID, t.Handover = a.Profile, a.Model, a.SessionID, nil
			if a.Effort != nil {
				t.Effort = *a.Effort
			}
			t.Failure = nil
			resetAutoResumes(t)
			if a.FirstMessage && !t.Running && t.Starting == nil {
				t.Starting = &now
			}
			if a.HostNotice != "" {
				t.Feed.Enqueue(Notice{About: "thread", Key: "handover.host", Urgency: Next, What: a.HostNotice, At: now})
			}
			return Result{Events: []Event{{"handover", a.Data}}}, nil
		}},
		{Name: KindThreadHandedOver, From: ownerOnly, Touch: true, Reduce: reduceThreadHandedOver},
		{Name: KindThreadRename, From: agentOnly, Reduce: reduceRename},
	} {
		RegisterKind(k)
	}
}

// reduceThreadHandedOver marks the source thread handed over to another host
// and settles it (decision 0039, V0.trans-host-handover).
func reduceThreadHandedOver(r Rec, in Input, now time.Time) (Result, error) {
	a := mapArgs(in)
	toHostID := a.Str("toHostId")
	toThreadID := a.Str("toThreadId")
	if toHostID == "" || toThreadID == "" {
		return Result{}, reject(RejectBadArgs, "toHostId and toThreadId are required")
	}
	t := r.T
	t.HandedOverTo = &HandoverLink{
		HostID:   toHostID,
		HostName: a.Str("toHostName"),
		ThreadID: toThreadID,
	}
	t.Handover = nil
	t.Failure = nil
	resetAutoResumes(t)
	settleNow(t, now)
	r.V.SeenAt = now
	Unsnooze(r.V)
	data := map[string]any{
		"from":       t.Profile,
		"to":         a.Str("toProfile"),
		"fromHost":   a.Str("fromHostName"),
		"fromHostId": a.Str("fromHostId"),
		"toHost":     a.Str("toHostName"),
		"toHostId":   toHostID,
		"toThread":   toThreadID,
		"rung":       a.Str("rung"),
	}
	if m := a.Str("toModel"); m != "" {
		data["model"] = m
	}
	if c := a.Str("toCwd"); c != "" {
		data["toCwd"] = c
	}
	if n := a.Str("note"); n != "" {
		data["note"] = n
	}
	return Result{Events: []Event{{"handover", data}}, Effects: []Effect{{Kind: EffectStop}}}, nil
}

// MaxTitle is the longest thread title in characters (V0.69).
const MaxTitle = 80

// reduceRename is the agent's thread_rename (V0.69): one trimmed line of at
// most MaxTitle characters; a name the owner gave is kept.
func reduceRename(r Rec, in Input, _ time.Time) (Result, error) {
	a, _ := in.Args.(map[string]any)
	title, _ := a["title"].(string)
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return Result{}, &Rejection{Code: RejectBadArgs, Reason: "the title is empty"}
	}
	if utf8.RuneCountInString(title) > MaxTitle {
		return Result{}, &Rejection{Code: RejectBadArgs, Reason: fmt.Sprintf("a title has at most %d characters", MaxTitle)}
	}
	if err := CanWrite(FieldThreadTitle, ByAgent, r.T.TitleBy, "this thread"); err != nil {
		return Result{}, err
	}
	if r.T.Title == title && r.T.TitleBy == ByAgent {
		return Result{}, nil
	}
	// V0.auto-title (1): the agent's rename to the title Fyr made still
	// makes it the agent's, so Fyr's goal title keeps it.
	r.T.Title, r.T.TitleBy = title, ByAgent
	return Result{Events: []Event{{"thread.renamed", map[string]any{"title": title, "by": ByAgent}}}}, nil
}

// RestartArgs are the args of host.restarted.
type RestartArgs struct {
	// Kept: the launcher's runner still holds the thread's process (V0.90),
	// so its run, its queue and its questions go on.
	Kept bool
}

// reduceRestarted repairs what the last run of the host left behind (SPEC
// 0.5 V0.6): the agent's queue (V0.33), a handover (V0.45) and live work,
// which becomes failed with class restart. A thread whose process the
// runner kept loses only its handover (V0.90). A wait for the machine ends
// either way: the start that waited lived in the last host
// (V0.machine-wait-restart).
func reduceRestarted(r Rec, in Input, now time.Time) (Result, error) {
	t := r.T
	var res Result
	a, _ := in.Args.(RestartArgs)
	// A record saved by an older build may hold PR conditions on a PR that
	// is no longer open (decision 0018); drop them so the load check is clean.
	for i := range t.Changes {
		if c := &t.Changes[i]; c.PR != nil && c.PR.State != "OPEN" {
			dropConditions(c, CondChecks, CondReview, CondConflict, CondMerge)
		}
	}
	t.GateHold = nil // the hold lived in the last host
	t.Frozen = nil   // V0.freeze-visible: so did the freeze
	if t.MachineWait != nil {
		t.MachineWait = nil
		res.Events = append(res.Events, Event{"agent.machineWait", map[string]any{"done": true}})
	}
	// V0.starting-moves: the start lived in the last host. Without the
	// process the message it was to deliver is lost, so the thread fails
	// (ProcessLost below ends the start).
	starting := t.Starting != nil
	if a.Kept {
		t.Starting = nil
		if t.Handover != nil {
			t.Handover = nil
			res.Events = append(res.Events, Event{"effect.error", map[string]any{"kind": "handover", "error": "Fyr restarted during the handover"}})
		}
		return res, nil
	}
	for _, m := range t.Outbox {
		res.Events = append(res.Events, Event{"message.state", map[string]any{"id": m.ID, "state": "lost"}})
	}
	t.Outbox = nil
	if t.Handover != nil {
		t.Handover = nil
		res.Events = append(res.Events, Event{"effect.error", map[string]any{"kind": "handover", "error": "Fyr restarted during the handover"}})
	}
	var keep []Decision
	live := t.Running || starting
	for _, d := range t.Decisions {
		if !d.NeedsProcess() {
			keep = append(keep, d) // effect Decisions and Fyr's own questions do not need the process (5.10, V0.ask-tool)
		} else {
			live = true
		}
	}
	if !live && t.RunningTasks() == 0 {
		return res, nil
	}
	msg := "Fyr restarted during the run"
	if len(keep) != len(t.Decisions) {
		msg = "Fyr restarted while a question was open; Retry asks again"
	}
	ProcessLost(t, "restart", msg, now)
	t.Decisions = keep
	if live { // only background tasks: they stop, there was no run
		res.Events = append(res.Events, Event{"run.ended", map[string]any{"failure": t.Failure}})
	}
	return res, nil
}
