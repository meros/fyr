package core

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"
)

// OldViewTyped is the mark a message typed on the View tab carried before
// V0.128 (V0.49). A send with it is an ordinary message, and a stored one
// reads as the owner's own.
const OldViewTyped = "view-typed"

// Args are the arguments of one command, as decoded from JSON.
type Args map[string]any

func (a Args) Str(k string) string { s, _ := a[k].(string); return s }

// Effect is work an Action asks the host to do after its state change is
// stored. Apply never performs effects itself, so it stays pure.
type Effect struct {
	// ID names the effect for its runner and its result (effect:<id>); the
	// engine sets it when it stores the reduce that returned it.
	ID     string     `json:"id,omitempty"`
	Kind   EffectKind `json:"kind"`
	Text   string     `json:"text,omitempty"`
	Images []Image    `json:"images,omitempty"`
	// From is the thread whose agent sent this message (V0.22).
	From string `json:"from,omitempty"`
	// Attachments are ids of stored files (SPEC 0.5 V0.18).
	Attachments []string          `json:"attachments,omitempty"`
	DecisionID  string            `json:"decisionId,omitempty"`
	Answers     map[string]string `json:"answers,omitempty"`
	Allow       bool              `json:"allow,omitempty"`
	Model       string            `json:"model,omitempty"`
	// Effort is the effort level of a setModel (V0.50); nil leaves it.
	Effort *string `json:"effort,omitempty"`
	// Profile is the target of a handover (V0.45).
	Profile string `json:"profile,omitempty"`
	// Request is the approved effect for Kind EffectRun.
	Request *EffectReq `json:"request,omitempty"`
	// ByOwner marks an effect the owner started (V0.43): its result is
	// recorded in the thread and not sent to the agent.
	ByOwner bool `json:"byOwner,omitempty"`
	// MessageID names the message of a send, or the one to cancel (V0.33).
	MessageID string `json:"messageId,omitempty"`
	// Script is what EffectScript runs; Cwd is where EffectThread starts.
	Script *Script `json:"script,omitempty"`
	Cwd    string  `json:"cwd,omitempty"`
	// Project files the thread EffectThread starts.
	Project string `json:"project,omitempty"`
	// Press is the owner's press this effect runs, when it is one: its
	// result ends the action's run.
	Press *Press `json:"press,omitempty"`
	// Git is the folder's facts of an EffectGit, read by the agent manager
	// at the run's end.
	Git *GitFacts `json:"git,omitempty"`
	// Run is the run count an EffectTextAsk judges (V0.210).
	Run int `json:"run,omitempty"`
	// Asked is the question an EffectAnswered answers (V0.ask-tool).
	Asked *Decision `json:"asked,omitempty"`
}

// Script is an exact command: argv in dir, within a time limit.
type Script struct {
	Argv       []string `json:"argv"`
	Dir        string   `json:"dir,omitempty"`
	TimeoutSec int      `json:"timeoutSec,omitempty"`
	// Root runs argv as root through pkexec (V0.149).
	Root bool `json:"root,omitempty"`
}

// Press is an action the owner pressed: where, and what it was.
type Press struct {
	On     string     `json:"on"`
	Title  string     `json:"title"`
	Action ActionSpec `json:"action"`
}

// Image is a pasted image, base64 encoded, sent to the agent as a content block.
type Image struct {
	MediaType string `json:"mediaType"`
	Data      string `json:"data"`
}

// Action is one entry in the registry. Every surface (keys, buttons,
// palette, bulk selection) calls Actions through one command path.
type Action struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Key   string `json:"key,omitempty"`
	Bulk  bool   `json:"bulk,omitempty"`
	// Available returns "" when the action can run, else the reason it cannot.
	Available func(t *Thread, v *View, now time.Time) string `json:"-"`
	// Apply changes t and v in place and returns the effects to run.
	Apply func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) `json:"-"`
	// Agent: another thread's agent may send it too (thread_send,
	// thread_new), as an agent-source Input that names its thread in from.
	Agent bool `json:"-"`
	// Fields are the fields an argument writes, checked against Rights for
	// the sender (5.2).
	Fields map[string]Field `json:"-"`
}

var ErrBadArgs = errors.New("bad arguments")

// ErrReportLater tells the engine that an effect reports its own result
// later (a deploy reports from the new daemon, SPEC 0.5 V0.13).
var ErrReportLater = errors.New("the effect reports its result later")

func always(*Thread, *View, time.Time) string { return "" }

func notArchived(t *Thread, _ *View, _ time.Time) string {
	if t.ArchivedAt != nil {
		return "The thread is archived"
	}
	return ""
}

func images(a Args) []Image {
	raw, _ := a["images"].([]any)
	var out []Image
	for _, r := range raw {
		m, _ := r.(map[string]any)
		mt, _ := m["mediaType"].(string)
		d, _ := m["data"].(string)
		if mt != "" && d != "" {
			out = append(out, Image{mt, d})
		}
	}
	return out
}

// OpenMain is the main change while its PR is open, or nil.
func OpenMain(t *Thread) *Change {
	if c := t.Main(); c != nil && c.PR.State == "OPEN" {
		return c
	}
	return nil
}

// revive is what any message from the user does to a thread: it clears the
// failure, the settle and the snooze, and counts as seeing the thread.
func revive(t *Thread, v *View, now time.Time) {
	t.Failure = nil
	t.Settle.At = nil
	dropSettleDone(t) // a new run: "when done" was for the last one
	t.SettleAsk = nil // V0.settle-request: so was the agent's word
	Unsnooze(v)
	v.SeenAt = now
}

func sendAction(name, label string) *Action {
	return &Action{
		Name: name, Label: label, Agent: true,
		Fields: map[string]Field{"projectId": FieldThreadProject, "view": FieldThreadView, "title": FieldThreadTitle},
		Available: func(t *Thread, v *View, now time.Time) string {
			if r := notArchived(t, v, now); r != "" {
				return r
			}
			if len(t.Decisions) > 0 {
				return "Answer the open question first"
			}
			if t.Handover != nil {
				return "Wait for the handover to finish"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			text := strings.TrimSpace(a.Str("text"))
			imgs := images(a)
			atts := AttachmentIDs(a)
			if text == "" && len(imgs) == 0 && len(atts) == 0 {
				return nil, fmt.Errorf("%w: empty message", ErrBadArgs)
			}
			t.QuietMerge = "" // V0.200: whoever wrote wants to hear the answer
			if a.Str("from") == "" {
				clearTextAsk(t) // V0.210: the owner answered; another agent's message is no answer
			}
			if name == "thread.create" {
				t.Cwd = a.Str("cwd")
				if t.Cwd == "" {
					return nil, fmt.Errorf("%w: cwd is required", ErrBadArgs)
				}
				t.Model = a.Str("model")
				t.Effort = a.Str("effort")
				t.Profile = a.Str("profile")
				t.Title = a.Str("title")
				t.Parent = a.Str("parent")
				if on, _ := a["view"].(bool); on {
					SetView(t, true)
				}
				if t.Title == "" {
					t.Title = TitleFrom(text)
				}
				// V0.42: the engine checked that the project exists.
				if pid := a.Str("projectId"); pid != "" {
					t.ProjectID, t.ProjectManual = pid, true
				}
			}
			revive(t, v, now)
			if !t.Running && t.Starting == nil {
				// V0.starting-moves: the agent starts for this message;
				// the thread moves from the press, not from its first turn.
				t.Starting = &now
			}
			from := a.Str("from")
			if from == OldViewTyped {
				from = "" // V0.128: a stale client's View-tab message is an ordinary one
			}
			return []Effect{{Kind: EffectSend, Text: text, Images: imgs, Attachments: atts, From: from}}, nil
		},
	}
}

// Registry is the one list of Actions.
var Registry = map[string]*Action{}

// Ordered lists the registry in a stable order for clients.
func Ordered() []*Action {
	out := make([]*Action, 0, len(Registry))
	for _, a := range Registry {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// register adds an owner command: to the Registry (the list clients show)
// and as an Input kind the owner sends (decision 0020); an Agent command
// also from another thread's agent. Each argument in Fields is checked
// against Rights for the sender, and a sender that may not write the
// owner's seenAt leaves it as it was.
func register(a *Action) {
	if a.Available == nil {
		a.Available = always
	}
	Registry[a.Name] = a
	from := ownerOnly
	if a.Agent {
		from = []string{ByOwner, ByAgent}
	}
	RegisterKind(&InputKind{Name: a.Name, From: from, Available: a.Available, Touch: true,
		Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			by := in.Source.Principal()
			args := maps.Clone(mapArgs(in))
			if args == nil {
				args = Args{}
			}
			for k, f := range a.Fields {
				if _, set := args[k]; set {
					if err := CanWrite(f, by, "", "this thread"); err != nil {
						return Result{}, err
					}
				}
			}
			// Another thread's agent: the sending thread is the Input's, never
			// an argument; a thread it starts is its child.
			if by == ByAgent {
				args["from"], args["parent"] = in.From, in.From
			}
			seen := r.V.SeenAt
			effs, err := a.Apply(r.T, r.V, args, now)
			if err != nil {
				return Result{}, err
			}
			if CanWrite(FieldViewSeen, by, "", "") != nil {
				r.V.SeenAt = seen
			}
			if a.Name == "thread.send" || a.Name == "thread.create" {
				r.T.LastAsker = ""
				// Fyr's own messages go in as the owner's too, without a person's actor.
				if by == ByOwner && (strings.HasPrefix(in.Actor, "device:") || strings.HasPrefix(in.Actor, "local:")) {
					r.T.LastAsker = in.Actor
				}
			}
			if r.T.Profile == "" {
				r.T.Profile = "claude"
			}
			ev := map[string]any{"action": a.Name, "commandId": in.CmdID}
			if by != ByOwner {
				ev["by"], ev["from"] = by, in.From
			}
			if in.Actor != "" {
				ev["actor"] = in.Actor
				if in.Front != "" {
					ev["front"] = in.Front
				}
			}
			res := Result{Events: []Event{{"command", ev}}}
			for _, ef := range effs {
				// The owner's answer to an agent's request (5.10) settles here,
				// or becomes the Fyr effect it approved.
				if ef.Kind == EffectRun && ef.Request != nil && !ef.ByOwner {
					answerEffect(r.T, &res, *ef.Request, ef.Allow, now)
					continue
				}
				if ef.Kind == EffectAnswered && ef.Asked != nil { // V0.ask-tool
					answered(&res, *ef.Asked, ef.Answers)
					continue
				}
				res.Effects = append(res.Effects, ef)
			}
			return res, nil
		}})
}

func init() {
	register(sendAction("thread.create", "New thread"))
	register(sendAction("thread.send", "Send"))

	register(&Action{Name: "thread.answer", Label: "Answer",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if len(t.Decisions) == 0 {
				return "Nothing to answer"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			id := a.Str("decisionId")
			idx := -1
			for i, d := range t.Decisions {
				if d.ID == id {
					idx = i
				}
			}
			if idx < 0 {
				return nil, fmt.Errorf("%w: no open decision %q", ErrBadArgs, id)
			}
			d := t.Decisions[idx]
			e := Effect{Kind: EffectAnswer, DecisionID: id}
			if d.Kind == DecisionAsk {
				raw, _ := a["answers"].(map[string]any)
				e.Answers = map[string]string{}
				for _, q := range d.Questions {
					s, _ := raw[q.Question].(string)
					if strings.TrimSpace(s) == "" {
						return nil, fmt.Errorf("%w: question %q has no answer", ErrBadArgs, q.Question)
					}
					e.Answers[q.Question] = s
				}
				e.Allow = true
				if d.Async { // V0.ask-tool: nobody waits in a tool call
					e.Kind, e.Asked = EffectAnswered, &d
				}
			} else {
				e.Allow, _ = a["allow"].(bool)
			}
			if d.Kind == DecisionEffect {
				// V0.112: a yes whose effect must wait (a merge on pending
				// checks) is refused, and the ask stays open.
				if fx := FyrEffects[d.Effect.Action]; e.Allow && fx != nil && fx.Gate != nil {
					if why := fx.Gate(t, d.Effect.Args); why != "" {
						return nil, &Rejection{Code: RejectUnavailable, Reason: why}
					}
				}
				// SPEC 5.10: the agent is not waiting; Fyr runs the effect itself.
				e.Kind, e.Request = EffectRun, d.Effect
			}
			t.Decisions = append(t.Decisions[:idx:idx], t.Decisions[idx+1:]...)
			if len(t.Decisions) == 0 {
				t.Decisions = nil
			}
			v.SeenAt = now
			return []Effect{e}, nil
		}})

	register(&Action{Name: "thread.interrupt", Label: "Stop run", Key: "Escape",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if !t.Running {
				return "No run is going"
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			return []Effect{{Kind: EffectInterrupt}}, nil
		}})

	register(&Action{Name: "thread.retry", Label: "Retry", Key: "r",
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.Failure == nil {
				return "The last run did not fail"
			}
			return notArchived(t, v, now)
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			msg := "The last run stopped with an error (" + t.Failure.Message + "). Continue from where you stopped."
			revive(t, v, now)
			return []Effect{{Kind: EffectSend, Text: msg}}, nil
		}})

	// V0.143: the agent process does not take what Fyr writes; end it and
	// resume the session, delivering the unread messages again.
	register(&Action{Name: "thread.restart", Label: "Restart agent",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.Unresponsive == nil {
				return "The agent is responding"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			v.SeenAt = now
			return []Effect{{Kind: EffectRestart}}, nil
		}})

	register(&Action{Name: "thread.dismissPR", Label: "Dismiss PR",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			p := t.MainPR()
			if p == nil {
				return "No PR"
			}
			if p.State == "OPEN" {
				return "The PR is still open"
			}
			return ""
		},
		// V0.36, #32: the change keeps its record and is never the main
		// change again; the thread has no main PR until a new one attaches.
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			if c := t.Main(); c != nil {
				c.Dismissed = true
			}
			t.MainChange = ""
			return nil, nil
		}})

	register(&Action{Name: KindChangeEnd, Label: "End this change",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			for i := range t.Changes {
				if !t.Changes[i].Phase.Ended() {
					return ""
				}
			}
			return "No change to end"
		},
		Apply: endChange})

	register(&Action{Name: "thread.dismiss", Label: "Dismiss failure",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.Failure == nil {
				return "No failure to dismiss"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			t.Failure = nil
			v.SeenAt = now
			return nil, nil
		}})

	// V0.203: Settle settles, now, whatever the thread is doing. A settled
	// thread does not run: a live run is stopped and the messages the agent
	// has not read are withdrawn. Args{"now": true} is the bulk form, which
	// leaves pinned and running threads alone.
	register(&Action{Name: "thread.settle", Label: "Settle", Key: "e", Bulk: true,
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.Settle.At != nil {
				return "Already settled"
			}
			return notArchived(t, v, now)
		},
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			bulk, _ := a["now"].(bool)
			if bulk && t.PinnedAt != nil {
				// V0.162: a bulk settle skips a pinned thread; only the
				// thread's own Settle (or unpin) lets it go.
				return nil, reject(RejectConflict, "A pinned thread settles only by its own Settle")
			}
			if bulk && t.Running {
				return nil, reject(RejectConflict, "A running thread settles only by its own Settle")
			}
			var fx []Effect
			for _, m := range t.Outbox {
				fx = append(fx, Effect{Kind: EffectCancelMessage, MessageID: m.ID})
			}
			if t.Running {
				fx = append(fx, Effect{Kind: EffectInterrupt})
			}
			settleNow(t, now)
			t.Failure = nil
			v.SeenAt = now
			Unsnooze(v)
			return fx, nil
		}})

	// V0.203: the explicit wait. The thread settles when its work is done
	// (WorkDone). Off with a reason when there is nothing it could wait for.
	register(&Action{Name: "thread.settleWhenDone", Label: "Settle when done",
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.Settle.At != nil {
				return "Already settled"
			}
			if r := notArchived(t, v, now); r != "" {
				return r
			}
			switch {
			case settlesWhenDoneByOwner(t):
				return "Already set"
			case t.Failure != nil:
				return "The last run failed"
			case WorkDone(t):
				return SettleDoneIdle
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, _ Args, now time.Time) ([]Effect, error) {
			SettleWhenDone(t, ByOwner, now)
			return nil, nil
		}})

	register(&Action{Name: "thread.unsettle", Label: "Unsettle", Bulk: true,
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.Settle.At == nil && !Settling(t) {
				return "Not settled"
			}
			return ""
		},
		// #33: clears the settle and every settle Rule, the agent's too.
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			t.Settle = Settle{}
			t.SettleAsk = nil
			DropSettleRules(t)
			return nil, nil
		}})

	register(&Action{Name: "thread.settleWhenMerged", Label: "Settle when merged", Key: "m",
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if OpenMain(t) == nil {
				return "No open PR"
			}
			if SettlesWhenMerged(t) {
				return "Already set"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			SettleWhenMerged(t, ByOwner, now)
			v.SeenAt = now
			return nil, nil
		}})

	register(&Action{Name: "thread.snooze", Label: "Snooze", Key: "s", Bulk: true,
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.Settle.At != nil {
				return "Settled threads do not snooze"
			}
			return notArchived(t, v, now)
		},
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			until, err := time.Parse(time.RFC3339, a.Str("until"))
			if err != nil || !until.After(now) {
				return nil, fmt.Errorf("%w: until must be a future RFC 3339 time", ErrBadArgs)
			}
			Snooze(v, until, now)
			t.PinnedAt = nil // V0.104
			v.SeenAt = now
			return nil, nil
		}})

	// V0.185: until work is done. Off with a reason when no work goes on or
	// the thread needs the owner, so nothing is hidden silently.
	register(&Action{Name: "thread.snoozeWhenDone", Label: "Snooze until work is done", Bulk: true,
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.Settle.At != nil {
				return "Settled threads do not snooze"
			}
			if r := notArchived(t, v, now); r != "" {
				return r
			}
			return snoozeDoneReason(t)
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			SnoozeWhenDone(v, now)
			t.PinnedAt = nil // V0.104
			v.SeenAt = now
			return nil, nil
		}})

	register(&Action{Name: "thread.unsnooze", Label: "Unsnooze", Bulk: true,
		Available: func(_ *Thread, v *View, now time.Time) string {
			if until, _ := SnoozeOf(v); until == nil || !until.After(now) {
				return "Not snoozed"
			}
			return ""
		},
		Apply: func(_ *Thread, v *View, _ Args, _ time.Time) ([]Effect, error) {
			Unsnooze(v)
			return nil, nil
		}})

	// V0.104: the owner pins a thread above every other; agents cannot.
	register(&Action{Name: "thread.pin", Label: "Pin", Bulk: true,
		Available: func(t *Thread, v *View, now time.Time) string {
			if t.PinnedAt != nil {
				return "Already pinned"
			}
			if t.Settle.At != nil {
				return "Settled threads do not pin"
			}
			if until, _ := SnoozeOf(v); until != nil && until.After(now) {
				return "Snoozed threads do not pin"
			}
			return notArchived(t, v, now)
		},
		Apply: func(t *Thread, _ *View, _ Args, now time.Time) ([]Effect, error) {
			t.PinnedAt = &now
			return nil, nil
		}})

	register(&Action{Name: "thread.unpin", Label: "Unpin", Bulk: true,
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.PinnedAt == nil {
				return "Not pinned"
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			t.PinnedAt = nil
			DropAutoSettle(t) // V0.106: what waited for the pin is dropped, not fired
			return nil, nil
		}})

	register(&Action{Name: "thread.archive", Label: "Archive", Key: "#", Bulk: true,
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.ArchivedAt != nil {
				return "Already archived"
			}
			// L3: archived threads have no live work. V0.101: an agent's ask
			// does not hold it; archiving voids the ask.
			if t.Running || slices.ContainsFunc(t.Decisions, Decision.NeedsProcess) {
				return "Stop the run first"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			t.ArchivedAt = &now
			t.PinnedAt = nil // V0.104
			Unsnooze(v)
			return []Effect{{Kind: EffectStop}}, nil
		}})

	register(&Action{Name: "thread.unarchive", Label: "Unarchive", Bulk: true,
		Available: func(t *Thread, _ *View, _ time.Time) string {
			if t.ArchivedAt == nil {
				return "Not archived"
			}
			return ""
		},
		Apply: func(t *Thread, _ *View, _ Args, _ time.Time) ([]Effect, error) {
			t.ArchivedAt = nil
			return nil, nil
		}})

	register(&Action{Name: "thread.markSeen", Label: "Mark seen",
		// SPEC 6.4: up to the activityAt the client showed, never past now.
		Apply: func(_ *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			upTo := now
			if s := a.Str("upTo"); s != "" {
				t, err := time.Parse(time.RFC3339Nano, s)
				if err != nil {
					return nil, fmt.Errorf("%w: upTo must be RFC 3339", ErrBadArgs)
				}
				if t.Before(now) {
					upTo = t
				}
			}
			if upTo.After(v.SeenAt) {
				v.SeenAt = upTo
			}
			return nil, nil
		}})

	register(&Action{Name: "thread.markUnseen", Label: "Mark unseen", Key: "u", Bulk: true,
		Apply: func(t *Thread, v *View, _ Args, now time.Time) ([]Effect, error) {
			if t.ActivityAt.IsZero() {
				t.ActivityAt = now
			}
			v.SeenAt = t.ActivityAt.Add(-time.Millisecond)
			return nil, nil
		}})

	register(&Action{Name: "thread.setProject", Label: "Move to project", Fields: map[string]Field{"projectId": FieldThreadProject},
		// SPEC 0.5 V0.23: the engine checks that the project exists.
		// V0.30: "" unfiles the thread, still by hand, so matching leaves it.
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			if _, ok := a["projectId"].(string); !ok {
				return nil, fmt.Errorf("%w: projectId is required (\"\" for no project)", ErrBadArgs)
			}
			t.ProjectID, t.ProjectManual = a.Str("projectId"), true
			return nil, nil
		}})

	register(&Action{Name: "thread.rename", Label: "Rename",
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			s := strings.TrimSpace(a.Str("title"))
			if s == "" {
				return nil, fmt.Errorf("%w: empty title", ErrBadArgs)
			}
			t.Title, t.TitleBy = s, ByOwner
			return nil, nil
		}})

	register(&Action{Name: "thread.setView", Label: "Live view", Fields: map[string]Field{"on": FieldThreadView},
		// SPEC 0.5 V0.37: the agent's prompt and tools change with the next
		// process, so the host restarts an idle one.
		Available: notArchived,
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			on, ok := a["on"].(bool)
			if !ok {
				return nil, fmt.Errorf("%w: on must be true or false", ErrBadArgs)
			}
			if !SetView(t, on) { // V0.60: off drops the view's actions
				return nil, nil
			}
			return []Effect{{Kind: EffectRefresh}}, nil
		}})

	register(&Action{Name: "thread.handover", Label: "Hand over",
		// SPEC 0.5 V0.45, decision 0013: only the owner starts it; the host
		// moves the context by the first rung that works.
		Available: func(t *Thread, v *View, now time.Time) string {
			if r := notArchived(t, v, now); r != "" {
				return r
			}
			switch {
			case t.Handover != nil:
				return "A handover is already going"
			case t.Running:
				return "Wait for the run to end"
			case len(t.Decisions) > 0:
				return "Answer the open question first"
			case len(t.Outbox) > 0:
				return "Wait until the agent has read your message"
			}
			return ""
		},
		Apply: func(t *Thread, v *View, a Args, now time.Time) ([]Effect, error) {
			p := a.Str("profile")
			if p == "" {
				return nil, fmt.Errorf("%w: profile is required", ErrBadArgs)
			}
			if p == t.Profile {
				return nil, fmt.Errorf("%w: the thread already runs on %s", ErrBadArgs, p)
			}
			t.Handover = &Handover{To: p, Model: a.Str("model"), At: now}
			v.SeenAt = now
			e := Effect{Kind: EffectHandover, Profile: p, Model: a.Str("model")}
			// V0.model-choice: the effort the owner picked for the target; absent keeps the thread's
			if _, ok := a["effort"]; ok {
				eff := a.Str("effort")
				t.Handover.Effort, e.Effort = &eff, &eff
			}
			return []Effect{e}, nil
		}})

	register(&Action{Name: "thread.setModel", Label: "Change model",
		Available: notArchived,
		Apply: func(t *Thread, _ *View, a Args, _ time.Time) ([]Effect, error) {
			m := a.Str("model")
			_, hasEffort := a["effort"]
			if m == "" && !hasEffort {
				return nil, fmt.Errorf("%w: empty model", ErrBadArgs)
			}
			if m == "" {
				m = t.Model
			}
			t.Model = m
			e := Effect{Kind: EffectSetModel, Model: m}
			if hasEffort {
				eff := a.Str("effort")
				t.Effort, e.Effort = eff, &eff
			}
			return []Effect{e}, nil
		}})
}

// TitleFrom makes a first title from the first line of a prompt.
func TitleFrom(text string) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(text), "\n", 2)[0])
	if line == "" {
		return "Image"
	}
	r := []rune(line)
	if len(r) > 80 {
		return string(r[:79]) + "…"
	}
	return line
}

// AttachmentIDs reads args.attachments, a list of stored file ids.
func AttachmentIDs(a Args) []string {
	raw, _ := a["attachments"].([]any)
	var out []string
	for _, x := range raw {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
