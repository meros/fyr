package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
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
	KindMachineWait    = "agent.machineWait"
	KindTask           = "task"
	KindTaskQuiet      = "task.quiet" // V0.195: a background command waits while the thread uses no CPU
	KindSession        = "session"
	KindDigest         = "digest"
	KindDecisionOpened = "decision.opened"
	KindDecisionGone   = "decision.withdrawn"
	KindHandoverFailed   = "handover.failed"
	KindHandoverDone     = "handover.done"
	KindThreadHandedOver = "thread.handedOver" // decision 0039, V0.trans-host-handover
	KindSlotGranted      = "slot.granted"      // V0.86: a queued slot is the thread's now
	KindSlotQueued     = "slot.queued"  // V0.185: the thread joined or left the slot queue
	KindSlotMemory     = "slot.memory"  // decision 0029 S2: memory is short; this job grew most
	KindSlotOver       = "slot.over"    // decision 0029 S3: a running job is heavier than predicted
	KindSlotPause      = "slot.pause"   // decision 0029 S5: the memory guard froze or thawed this job
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
	}
	ViewPublishArgs struct {
		PageID string
		State  map[string]any
	}
	DeployObservedArgs struct {
		Change string
		Obs    DeployObs
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
		Log               []Event
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
			}
			return Result{}, err
		}},
		{Name: KindDeployObserved, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[DeployObservedArgs](in)
			if err == nil {
				ChangeDeployObserved(r.T, a.Change, a.Obs, now)
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
			kind, _ := in.Args.(string)
			if kind == "" {
				return Result{}, errors.New("slot.granted needs the kind")
			}
			r.T.SlotQueued = false
			return Result{Events: []Event{{EventSlot, map[string]any{"kind": kind, "granted": true}}}}, nil
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
		{Name: KindSlotPause, From: observed, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
			d := mapArgs(in)
			_, p := d["paused"].(string)
			_, k := d["killed"].(string)
			if !p && !k && d["resumed"] != true && d["waits"] != true && d["retry"] != true {
				return Result{}, errors.New("slot.pause needs paused, resumed, waits, retry or killed")
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
			ProcessLost(r.T, "process", msg, now)
			if r.T.Failure == nil || r.T.Failure == wasFail {
				return Result{}, nil
			}
			return Result{Events: []Event{{"run.ended", map[string]any{"failure": map[string]any{"class": "process", "message": msg}}}}}, nil
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
			if r.T.MachineWait != nil && r.T.MachineWait.Why == a.Why {
				return Result{}, nil
			}
			r.T.MachineWait = &MachineWait{Since: now, Why: a.Why}
			return Result{Events: []Event{{"agent.machineWait", map[string]any{"why": a.Why}}}}, nil
		}},
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
			t.QuotaResumes = 0
			t.QuotaResumedAt = nil
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
	t.QuotaResumes = 0
	t.QuotaResumedAt = nil
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
	if r.T.Title == title {
		return Result{}, nil
	}
	r.T.Title, r.T.TitleBy = title, ByAgent
	return Result{Events: []Event{{"thread.renamed", map[string]any{"title": title, "by": ByAgent}}}}, nil
}

// changeKind is a change tool's reducer: fn finds or makes the change, and
// the reduce records one `change` event (V0.59 (5)).
func changeKind(op string, fn func(t *Thread, in Input, now time.Time) (*Change, bool, error)) func(Rec, Input, time.Time) (Result, error) {
	return changeKindNotes(op, func(t *Thread, in Input, now time.Time) (*Change, bool, []string, error) {
		c, isNew, err := fn(t, in, now)
		return c, isNew, nil, err
	})
}

// changeKindNotes is changeKind for a report whose reply carries notes
// (V0.101: what Fyr reads instead of an observed value).
func changeKindNotes(op string, fn func(t *Thread, in Input, now time.Time) (*Change, bool, []string, error)) func(Rec, Input, time.Time) (Result, error) {
	return func(r Rec, in Input, now time.Time) (Result, error) {
		c, isNew, notes, err := fn(r.T, in, now)
		if err != nil {
			return Result{}, err
		}
		ev := Event{"change", map[string]any{"op": op, "change": map[string]any{"id": c.ID, "title": c.Title, "kind": c.Kind, "phase": c.Phase, "phaseBy": c.PhaseBy, "label": c.Label}}}
		return Result{Events: []Event{ev}, Reply: ChangeReply{Change: *c, New: isNew, Notes: notes}}, nil
	}
}

// reportChange is change_update: the report, then a revision and proof
// when given (V0.101).
func reportChange(t *Thread, in Input, now time.Time) (*Change, bool, []string, error) {
	a, ok := in.Args.(ChangeUpdateArgs)
	if !ok {
		return nil, false, nil, fmt.Errorf("%w: %s got %T", ErrBadArgs, in.Kind, in.Args)
	}
	c, err := findChange(t, a.Change)
	if err != nil {
		return nil, false, nil, err
	}
	if len(a.Evidence) > 0 {
		for _, ev := range a.Evidence {
			if ev.Kind == "" || (ev.URL == "" && ev.Attachment == "") {
				return nil, false, nil, errors.New("evidence needs a kind and a url or an attachment")
			}
		}
	}
	notes, err := ReportChange(t, c, a.Update, in.Source.Principal(), now)
	if err != nil {
		return nil, false, nil, err
	}
	added := false
	if a.Revision != nil {
		added = ReviseChange(c, *a.Revision, now)
	}
	if len(a.Evidence) > 0 {
		id, group := c.ID, c.Group
		AttachEvidence(c, a.Evidence, now)
		c = joinPRWork(t, c, now) // V0.87 (2)
		if c == nil {
			c = FindChange(t, id)
		}
		for _, ev := range a.Evidence {
			if ev.Kind != "pr" || ev.URL == "" {
				continue
			}
			u, ok := CanonicalPR(ev.URL)
			if !ok {
				continue
			}
			repo, n := parsePRURL(u)
			if cur := changeByPR(t, u); cur == nil {
				if pc := ChangePRObserved(t, PR{URL: u, Repo: repo, Number: n, State: "OPEN", Checks: ChecksPending}, now); pc != nil {
					if pc.Group == "" && group != "" {
						pc.Group = group
					}
					if t.MainPR() == nil {
						t.MainChange = pc.ID
					}
				}
			} else {
				armBuiltins(t, cur, now)
				if t.MainPR() == nil && !cur.Dismissed {
					t.MainChange = cur.ID
				}
			}
		}
		if updated := FindChange(t, id); updated != nil {
			c = updated
		}
	}
	return c, added, notes, nil
}

// codeOnly refuses a change_open that is not code (V0.changes-code-only
// (3)) with the line that names its replacement; no kind is code. A code
// change names its branch or its PR: Fyr reads the rest itself.
func codeOnly(a *ChangeOpenArgs) error {
	if a.Open.Kind == "" {
		a.Open.Kind = KindCode
	}
	if line, gone := goneKinds[a.Open.Kind]; gone {
		return errors.New(line)
	}
	if a.Open.Kind != KindCode {
		return fmt.Errorf("kind must be %s", strings.Join(Names(OpenKinds()), ", "))
	}
	if strings.TrimSpace(a.Open.Branch) == "" && a.PR == "" {
		return errors.New("a code change needs its branch or its pr; Fyr finds pushes and PRs of gh pr create itself")
	}
	return nil
}

// NoChange is the reason for a change ref the thread does not have.
func NoChange(ref string) error {
	return &Rejection{Code: RejectNotFound, Reason: fmt.Sprintf("no change %q in this thread; open it with change_open, or list them with change_list", ref)}
}

func findChange(t *Thread, ref string) (*Change, error) {
	if c := FindChange(t, ref); c != nil {
		return c, nil
	}
	return nil, NoChange(ref)
}

// canRequest checks an agent's request of a Fyr effect: its args, and the
// effect's guard, the same one the owner's command and press use (#31).
// A merge is bound to the head Fyr reads when the agent asks (V0.97); the
// guard comes first, so a blocked PR says why before a missing head does.
func canRequest(t *Thread, fx *FyrEffect, args map[string]any) error {
	if fx.ID == FyrMerge {
		fixHead(t, args)
	}
	if why := fx.Guard(t, args); why != "" {
		return fmt.Errorf("Fyr cannot %s now: %s", strings.ToLower(fx.Label), why)
	}
	return fx.Check(args)
}

// openRequest opens the owner's Decision for an agent's request; a merge
// ask goes through the CI gate first (V0.136).
func openRequest(t *Thread, id string, req EffectReq, now time.Time) (Result, error) {
	d := Decision{ID: id, Kind: DecisionEffect, Assignee: Owner, Effect: &req, CreatedAt: now}
	if req.Action == FyrMerge {
		return openMergeAsk(t, d)
	}
	DecisionOpened(t, d)
	return Result{Events: []Event{{"decision.opened", map[string]any{"decision": d}}}}, nil
}

// NotFyrRepoText is why a thread that is not on Fyr's own repo cannot ask
// to deploy Fyr (V0.one-tool-vocabulary).
const NotFyrRepoText = "Only a thread that works on Fyr's own repository may ask to deploy Fyr. For a deploy the owner must approve, call run_request with the command and the why."

func reduceChangeRequest(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[ChangeRequest](in)
	if err != nil {
		return Result{}, err
	}
	if line, gone := goneAsks[a.Ask]; gone { // V0.changes-code-only (3)
		return Result{}, errors.New(line)
	}
	if !slices.Contains(ChangeAsks(), a.Ask) {
		return Result{}, fmt.Errorf("ask must be one of %s", strings.Join(Names(ChangeAsks()), ", "))
	}
	if a.Ask == AskFyrDeploy && !a.FyrRepo {
		return Result{}, errors.New(NotFyrRepoText)
	}
	if a.Args == nil {
		a.Args = map[string]any{}
	}
	var c *Change
	var linked []Event
	if a.Change != "" || a.Ask != AskFyrDeploy { // a deploy needs no change
		if c, linked, err = requestedChange(r.T, a.Change, now); err != nil {
			return Result{}, err
		}
		if c.Phase.Ended() {
			return Result{}, fmt.Errorf("%q is %s", c.Title, c.Phase)
		}
	}
	action := "change." + string(a.Ask)
	if fx := FyrEffectFor(a.Ask); fx != nil { // merge, deploy: Fyr runs them
		action = fx.ID
		if _, ok := a.Args["pr"].(string); !ok && fx.OnPR && c != nil && c.PRURL() != "" {
			a.Args["pr"] = c.PRURL()
		}
		if pr, _ := a.Args["pr"].(string); fx.OnPR && pr == "" {
			return Result{}, errors.New(noPRText(c))
		}
		if err := canRequest(r.T, fx, a.Args); err != nil {
			return Result{}, err
		}
	}
	reply := ChangeRequestReply{ID: a.ID}
	if c != nil {
		a.Args["change"] = c.ID
		if _, ok := a.Args["title"]; !ok {
			a.Args["title"] = c.Title
		}
		cp := *c
		reply.Change = &cp
	}
	req := EffectReq{Action: action, Args: a.Args, Reason: a.Reason}
	if id := openAskLike(r.T, req); id != "" { // a retry: the ask is open already
		reply.ID, reply.Again = id, true
		return Result{Reply: reply}, nil
	}
	res, err := openRequest(r.T, a.ID, req, now)
	if err != nil {
		return Result{}, err
	}
	res.Events = append(linked, res.Events...)
	res.Reply = reply
	return res, nil
}

// requestedChange is the change a change_request names. A PR URL the thread
// does not track yet becomes its change, so Fyr watches the PR (it was
// effect_request's pr.merge, V0.136).
func requestedChange(t *Thread, ref string, now time.Time) (*Change, []Event, error) {
	if c := FindChange(t, ref); c != nil {
		return c, nil, nil
	}
	if url, ok := CanonicalPR(ref); ok {
		if repo, n := parsePRURL(url); n != 0 {
			PRObserved(t, PR{URL: url, Repo: repo, Number: n, State: "OPEN", Checks: ChecksPending}, now)
			if c := changeByPR(t, url); c != nil {
				return c, []Event{{"pr.linked", map[string]any{"url": url}}}, nil
			}
		}
	}
	return nil, nil, NoChange(ref)
}

// openAskLike is the id of the thread's open or held ask for the same effect
// with the same args, or "" (V0.one-tool-vocabulary): an agent that retries a
// call whose reply it lost gets the ask it made.
func openAskLike(t *Thread, req EffectReq) string {
	want, _ := json.Marshal(req.Args)
	for _, d := range slices.Concat(t.Decisions, t.HeldAsks) {
		if d.Kind != DecisionEffect || d.Effect == nil || d.Effect.Action != req.Action {
			continue
		}
		if got, _ := json.Marshal(d.Effect.Args); string(got) == string(want) {
			return d.ID
		}
	}
	return ""
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
		Events: []Event{{"action", pressData(on, title, a, map[string]any{"by": "owner"})}}}
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
