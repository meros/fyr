package core

import (
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
	KindRecord          = "record"
	KindPRLink          = "pr.link"
	KindThreadRename    = "thread.retitle"
	KindEffectRequest   = "effect.request"
	KindChangeRequest   = "change.request"
	KindSettleWhen      = "settle.when"
	KindChangeOpen      = "change.open"
	KindChangeRevise    = "change.revise"
	KindChangeStatus    = "change.status"
	KindChangeAttach    = "change.attach"
	KindChangeUnchanged = "change.unchanged"
	KindChangeEnd       = "change.end"    // the owner's command (V0.72)
	KindChangePark      = "change.park"   // the owner's Not now (V0.84)
	KindChangeUnpark    = "change.unpark" // its Undo (V0.84)
	KindActionOffer     = "action.offer"
	KindActionPress     = "action.run"
	KindViewPublish     = "view.publish"
	KindViewState       = "view.state"
	KindViewUnchanged   = "view.unchanged"
	KindPRObserved      = "pr.observed"
	KindDeployObserved  = "deploy.observed"
	KindSelfDeployed    = "deploy.self" // V0.82
	KindHostRestarted   = "host.restarted"
	KindMessageSent     = "message.sent"
	KindMessageState    = "message.state"
	KindSendFailed      = "send.failed"
	KindRunStarted      = "run.started"
	KindRunEnded        = "run.ended"
	KindProcessLost     = "process.lost"
	KindTask            = "task"
	KindSession         = "session"
	KindDigest          = "digest"
	KindDecisionOpened  = "decision.opened"
	KindDecisionGone    = "decision.withdrawn"
	KindHandoverFailed  = "handover.failed"
	KindHandoverDone    = "handover.done"
	KindSlotGranted     = "slot.granted" // V0.86: a queued slot is the thread's now
)

// The typed args of the kinds that are not a map.
type (
	// EffectRequest is the agent's effect_request: a Decision for the owner.
	EffectRequest struct {
		ID, Action, Reason string
		Args               map[string]any
	}
	// ChangeRequest is the agent's change_request.
	ChangeRequest struct {
		ID, Change, Reason string
		Ask                ChangeAsk
		Args               map[string]any
	}
	// ChangeOpenArgs is change_open; PR is its canonical PR URL or "".
	ChangeOpenArgs struct {
		Open ChangeOpen
		PR   string
	}
	ChangeReviseArgs struct {
		Change   string
		Revision Revision
	}
	ChangeStatusArgs struct {
		Change string
		Update ChangeUpdate
	}
	ChangeAttachArgs struct {
		Change   string
		Evidence []Evidence
	}
	// ChangeReply is what a change kind returns: a copy of the change, and
	// whether the change or the revision is new.
	ChangeReply struct {
		Change Change
		New    bool
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
	}
	SessionArgs struct{ SessionID, Model string }
	// DigestArgs is one stream message's digest (agent/digest.go).
	DigestArgs struct {
		Activity, Summary string
		Subagents         int
		PR                *PR
		Log               []Event
	}
	HandoverDoneArgs struct {
		Profile, Model, SessionID string
		Data                      map[string]any
	}
)

func init() {
	for _, k := range []*InputKind{
		{Name: KindRecord, From: anyone, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
			evs, err := argsOf[[]Event](in)
			return Result{Events: evs}, err
		}},
		{Name: KindPRLink, From: agentOnly, Reduce: reducePRLink},
		{Name: KindEffectRequest, From: agentOnly, Reduce: reduceEffectRequest},
		{Name: KindChangeRequest, From: agentOnly, Reduce: reduceChangeRequest},
		{Name: KindSettleWhen, From: agentOnly, Reduce: reduceSettleWhen},
		{Name: KindChangeOpen, From: agentOnly, Reduce: changeKind("open", func(t *Thread, in Input, now time.Time) (*Change, bool, error) {
			a, err := argsOf[ChangeOpenArgs](in)
			if err != nil {
				return nil, false, err
			}
			c, made, err := OpenChange(t, a.Open, ByAgent, now)
			if err != nil {
				return nil, false, err
			}
			if a.PR != "" && t.MainPR() == nil {
				// The first PR is also the thread's main PR (the PR tab, the merge gate).
				repo, n := parsePRURL(a.PR)
				PRObserved(t, PR{URL: a.PR, Repo: repo, Number: n, State: "OPEN", Checks: ChecksPending, Branch: c.Branch}, now)
			}
			return c, made, nil
		})},
		{Name: KindChangeRevise, From: agentOnly, Reduce: changeKind("revise", func(t *Thread, in Input, now time.Time) (*Change, bool, error) {
			a, err := argsOf[ChangeReviseArgs](in)
			if err != nil {
				return nil, false, err
			}
			c, err := findChange(t, a.Change)
			if err != nil {
				return nil, false, err
			}
			return c, ReviseChange(c, a.Revision, now), nil
		})},
		{Name: KindChangeStatus, From: agentOnly, Reduce: changeKind("status", func(t *Thread, in Input, now time.Time) (*Change, bool, error) {
			a, err := argsOf[ChangeStatusArgs](in)
			if err != nil {
				return nil, false, err
			}
			c, err := findChange(t, a.Change)
			if err != nil {
				return nil, false, err
			}
			return c, false, UpdateChange(t, c, a.Update, in.Source.Principal(), now)
		})},
		{Name: KindChangeAttach, From: agentOnly, Reduce: changeKind("attach", func(t *Thread, in Input, now time.Time) (*Change, bool, error) {
			a, err := argsOf[ChangeAttachArgs](in)
			if err != nil {
				return nil, false, err
			}
			c, err := findChange(t, a.Change)
			if err != nil {
				return nil, false, err
			}
			if err := AttachEvidence(c, a.Evidence, now); err != nil {
				return nil, false, err
			}
			// V0.87 (2): a PR that Fyr's own change holds folds that change into this one.
			return joinPRWork(t, c, now), false, nil
		})},
		{Name: KindChangeUnchanged, From: agentOnly, Reduce: func(r Rec, _ Input, now time.Time) (Result, error) {
			n := OpenAgentChanges(r.T)
			ChangesChecked(r.T, now)
			return Result{Reply: n}, nil
		}},
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
		{Name: KindSlotGranted, From: observed, Reduce: func(_ Rec, in Input, _ time.Time) (Result, error) {
			kind, _ := in.Args.(string)
			if kind == "" {
				return Result{}, errors.New("slot.granted needs the kind")
			}
			return Result{Events: []Event{{EventSlot, map[string]any{"kind": kind, "granted": true}}}}, nil
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
			ProcessLost(r.T, "process", msg, now)
			return Result{Events: []Event{{"run.ended", map[string]any{"failure": map[string]any{"class": "process", "message": msg}}}}}, nil
		}},
		{Name: KindTask, From: observed, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			a, err := argsOf[TaskArgs](in)
			if err != nil {
				return Result{}, err
			}
			TaskObserved(r.T, a.Task, now)
			return Result{Events: a.Log}, nil
		}},
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
			if a.Activity != "" {
				t.Activity = a.Activity
			}
			if a.Summary != "" {
				t.Summary = a.Summary
			}
			t.Subagents += a.Subagents
			if p := t.MainPR(); a.PR != nil && (p == nil || p.URL != a.PR.URL) {
				PRObserved(t, *a.PR, now)
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
		{Name: KindHandoverDone, From: observed, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			a, err := argsOf[HandoverDoneArgs](in)
			if err != nil {
				return Result{}, err
			}
			t := r.T
			t.Profile, t.Model, t.SessionID, t.Handover = a.Profile, a.Model, a.SessionID, nil
			t.Failure = nil
			return Result{Events: []Event{{"handover", a.Data}}}, nil
		}},
		{Name: KindThreadRename, From: agentOnly, Reduce: reduceRename},
	} {
		RegisterKind(k)
	}
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
// the reduce records the upkeep (V0.62) and one `change` event (V0.59 (5)).
func changeKind(op string, fn func(t *Thread, in Input, now time.Time) (*Change, bool, error)) func(Rec, Input, time.Time) (Result, error) {
	return func(r Rec, in Input, now time.Time) (Result, error) {
		c, isNew, err := fn(r.T, in, now)
		if err != nil {
			return Result{}, err
		}
		ChangesChecked(r.T, now)
		ev := Event{"change", map[string]any{"op": op, "change": map[string]any{"id": c.ID, "title": c.Title, "kind": c.Kind, "phase": c.Phase, "phaseBy": c.PhaseBy, "label": c.Label}}}
		return Result{Events: []Event{ev}, Reply: ChangeReply{Change: *c, New: isNew}}, nil
	}
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

func reducePRLink(r Rec, in Input, now time.Time) (Result, error) {
	url, _ := in.Args.(string)
	repo, n := parsePRURL(url)
	if n == 0 {
		return Result{}, errors.New("not a GitHub pull request URL")
	}
	PRObserved(r.T, PR{URL: url, Repo: repo, Number: n, State: "OPEN", Checks: ChecksPending}, now)
	return Result{Events: []Event{{"pr.linked", map[string]any{"url": url}}}}, nil
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

// openRequest opens the owner's Decision for an agent's request.
func openRequest(t *Thread, id string, req EffectReq, now time.Time) Result {
	d := Decision{ID: id, Kind: DecisionEffect, Assignee: Owner, Effect: &req, CreatedAt: now}
	DecisionOpened(t, d)
	return Result{Events: []Event{{"decision.opened", map[string]any{"decision": d}}}}
}

func reduceEffectRequest(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[EffectRequest](in)
	if err != nil {
		return Result{}, err
	}
	fx := FyrEffects[a.Action]
	if fx == nil || !fx.Request {
		return Result{}, fmt.Errorf("unknown action %q", a.Action)
	}
	if a.Args == nil {
		a.Args = map[string]any{}
	}
	if err := canRequest(r.T, fx, a.Args); err != nil {
		return Result{}, err
	}
	return openRequest(r.T, a.ID, EffectReq{Action: a.Action, Args: a.Args, Reason: a.Reason}, now), nil
}

func reduceChangeRequest(r Rec, in Input, now time.Time) (Result, error) {
	a, err := argsOf[ChangeRequest](in)
	if err != nil {
		return Result{}, err
	}
	if !slices.Contains(ChangeAsks(), a.Ask) {
		return Result{}, fmt.Errorf("ask must be one of %s", strings.Join(Names(ChangeAsks()), ", "))
	}
	if a.Args == nil {
		a.Args = map[string]any{}
	}
	c, err := findChange(r.T, a.Change)
	if err != nil {
		return Result{}, err
	}
	if c.Phase.Ended() {
		return Result{}, fmt.Errorf("%q is %s", c.Title, c.Phase)
	}
	action := "change." + string(a.Ask)
	if fx := FyrEffectFor(a.Ask); fx != nil { // merge, deploy: Fyr runs them
		action = fx.ID
		if _, ok := a.Args["pr"].(string); !ok && fx.OnPR && c.PRURL() != "" {
			a.Args["pr"] = c.PRURL()
		}
		if err := canRequest(r.T, fx, a.Args); err != nil {
			return Result{}, err
		}
	}
	a.Args["change"] = c.ID
	if _, ok := a.Args["title"]; !ok {
		a.Args["title"] = c.Title
	}
	res := openRequest(r.T, a.ID, EffectReq{Action: action, Args: a.Args, Reason: a.Reason}, now)
	ChangesChecked(r.T, now) // V0.62
	res.Reply = *c
	return res, nil
}

func reduceSettleWhen(r Rec, in Input, now time.Time) (Result, error) {
	a := mapArgs(in)
	t, cond := r.T, a.Str("condition")
	switch cond {
	case "done":
		SettleWhenDone(t, ByAgent, now)
	case "merged":
		if !SettleWhenMerged(t, ByAgent, now) {
			return Result{}, errors.New("this thread has no open PR; link one with pr_link first")
		}
	case "change":
		phase := Phase(a.trimmed("phase"))
		if phase == "" {
			phase = PhaseLanded
		}
		c, err := findChange(t, a.trimmed("change"))
		if err != nil {
			return Result{}, err
		}
		SettleWhenReached(t, c.ID, phase, ByAgent, now)
		cond = fmt.Sprintf("%q is %s", c.Title, phase)
	default:
		return Result{}, fmt.Errorf("unknown condition %q", cond)
	}
	return Result{Reply: cond}, nil
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
		ChangesChecked(r.T, now) // V0.62
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
		Events: []Event{{"action", map[string]any{"on": on, "title": title, "action": a.ID, "label": a.Label, "run": a.Run, "argv": a.Argv, "by": "owner"}}}}
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
		res.Effects = []Effect{{Kind: EffectScript, Press: press, Script: &Script{Argv: a.Argv, Dir: dir, TimeoutSec: a.TimeoutSec}}}
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
// runner kept loses only its handover (V0.90).
func reduceRestarted(r Rec, in Input, now time.Time) (Result, error) {
	t := r.T
	var res Result
	a, _ := in.Args.(RestartArgs)
	if a.Kept {
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
	live := t.Running
	for _, d := range t.Decisions {
		if d.Kind == DecisionEffect {
			keep = append(keep, d) // effect Decisions do not need the process (5.10)
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
