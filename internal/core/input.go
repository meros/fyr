package core

// One path for every change to a record (decision 0020, parts 1 to 3, #45).
//
// Every change is an Input {source, kind, args, cmdId}. One pure function,
// Reduce, applies it to a copy of the thread record (its changes included)
// and the owner's view of it, and returns the new records, the events that
// record what happened and the effects Fyr runs afterwards, or a Rejection
// with a reason. An Input kind holds its guard (5.1 available) and names
// who may send it; the fields a reducer writes are checked against one table
// (rights.go). The engine stores the result in one transaction and starts
// the effects; an effect's result comes back as an Input with source
// effect:<id> (effects.go).

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

// Source is who sent an Input: the owner, the thread's agent, an observer
// (observer:<name>) or the result of an effect Fyr ran (effect:<id>).
type Source string

const (
	FromOwner Source = "owner"
	FromAgent Source = "agent"
)

// The observers that bring facts in as Inputs.
const (
	ObsProcess = "process" // the agent process's stream
	ObsGitHub  = "github"
	ObsDeploy  = "deployments"
	ObsHost    = "host" // the host itself: a restart
	ObsGit     = "git"  // the git observer: host change records (#35)
	ObsT3      = "t3"   // T3 adoption (V0.9)
)

// Observer is the source of an observer's Inputs.
func Observer(name string) Source { return Source("observer:" + name) }

// FromEffect is the source of an effect's result.
func FromEffect(id string) Source { return Source("effect:" + id) }

// Principal is who the source writes as in the field rights: ByOwner,
// ByAgent or ByObserved. An effect's result is a fact Fyr observed of its
// own work.
func (s Source) Principal() string {
	switch {
	case s == FromOwner:
		return ByOwner
	case s == FromAgent:
		return ByAgent
	case strings.HasPrefix(string(s), "rule:"):
		return ByRule
	}
	return ByObserved
}

// Input is one change to a thread record.
type Input struct {
	Source Source `json:"source"`
	Kind   string `json:"kind"`
	Thread string `json:"thread"`
	// Args are the kind's arguments: Args (map) for the owner's commands and
	// the agent's tools, a typed value for observers and effect results.
	Args  any    `json:"args,omitempty"`
	CmdID string `json:"cmdId,omitempty"`
	// From is the thread whose agent sent an agent-source Input to another
	// thread (thread_send, thread_new).
	From string `json:"from,omitempty"`
	// Actor and Front say who gave an owner command (decision 0030 3.3):
	// "device:<id>", "local:owner" or "install:<id>", and the install whose
	// front forwarded it. The command's event records them, which is the
	// audit trail; empty for an Input that is not a forwarded command.
	Actor string `json:"actor,omitempty"`
	Front string `json:"front,omitempty"`
}

// Event is one line a reduce appends to the thread's log. The agent feed
// (feed.go) projects its notices from these.
type Event struct {
	Type string
	Data any
}

// Result is what a reduce produces besides the records.
type Result struct {
	Events []Event
	// At are the times of the first Events when they happened before the
	// reduce (an adopted thread's history); an event without one is now.
	At      []time.Time
	Effects []Effect
	// Follow are Inputs to other threads that this reduce sends (a new
	// project files the thread it was made from): the engine reduces them in
	// the same transaction.
	Follow []Input
	// Reply is what the sender gets back: a tool's text, a command's result.
	Reply any
}

// RejectCode classifies a Rejection for the sender (a command's status).
type RejectCode int

const (
	RejectConflict    RejectCode = iota // not now (409)
	RejectBadArgs                       // the args are wrong (400)
	RejectNotFound                      // no such record (404)
	RejectRights                        // the source may not write this (403)
	RejectUnavailable                   // the guard: 5.1 available (409, no receipt)
)

// Rejection is why an Input did not apply. Nothing is stored.
type Rejection struct {
	Code   RejectCode
	Reason string
}

func (r *Rejection) Error() string { return r.Reason }

func reject(code RejectCode, format string, a ...any) *Rejection {
	return &Rejection{Code: code, Reason: fmt.Sprintf(format, a...)}
}

// Rec is the records one Input reduces: the thread (with its changes and
// Rules) and the owner's view of it (with the owner's Rules), or a project.
type Rec struct {
	T *Thread
	V *View
	P *Project
}

// Record names the record an Input kind changes.
type Record string

const (
	RecThread  Record = ""        // a thread and the owner's view of it
	RecProject Record = "project" // a project (3.15)
	// RecHost changes no record of the state: the host's own setup (the
	// provider registry, deploy/update controls), done by the kind's effects.
	RecHost Record = "host"
)

// InputKind is one kind of Input: who may send it, its guard and its
// reducer. A new feature is a kind, never a new path.
type InputKind struct {
	Name string
	// From are the principals that may send it (ByOwner, ByAgent,
	// ByObserved); an effect's result sends as ByObserved.
	From []string
	// Available is the guard (5.1): why it cannot apply now, or "".
	Available func(t *Thread, v *View, now time.Time) string
	// Reduce changes the records it is given (copies) and returns what
	// happened. It never does work: work is an Effect.
	Reduce func(r Rec, in Input, now time.Time) (Result, error)
	// Touch marks the thread updated even when only the view changed (the
	// owner's commands, V0.5).
	Touch bool
	// Untouched keeps the thread's UpdatedAt even when it changes: a
	// repair, not news.
	Untouched bool
	// On is the record it changes.
	On Record
	// Makes: it makes its record (a new thread or project).
	Makes bool
	// Await: the sender's reply waits for the kind's effects, which the
	// engine runs before it answers and before it stores the receipt (a
	// message cancel needs the agent's answer; the provider commands report
	// what they did).
	Await bool
}

// Kinds are every Input kind by name: the owner's commands (Registry), the
// agent's tools, the observers' facts and the effects' results.
var Kinds = map[string]*InputKind{}

// RegisterKind adds a kind; a second kind with one name is a bug.
func RegisterKind(k *InputKind) {
	if _, dup := Kinds[k.Name]; dup {
		panic("core: input kind registered twice: " + k.Name)
	}
	Kinds[k.Name] = k
}

var (
	anyone    = []string{ByOwner, ByAgent, ByObserved}
	ownerOnly = []string{ByOwner}
	agentOnly = []string{ByAgent}
	observed  = []string{ByObserved}
)

// Reduce applies one Input to copies of the records: t may be a new, empty
// thread for a kind that makes one. It is pure: the records passed in are
// not changed.
func Reduce(t *Thread, v *View, in Input, now time.Time) (*Thread, *View, Result, error) {
	k, err := kindOf(in, RecThread)
	if err != nil {
		return nil, nil, Result{}, err
	}
	before := t
	if before == nil {
		before = &Thread{}
	}
	t, v = CloneThread(t), cloneView(v)
	if v == nil {
		v = &View{User: Owner, Entity: t.ID}
	}
	v.Entity = t.ID
	if k.Available != nil {
		if why := k.Available(t, v, now); why != "" {
			return nil, nil, Result{}, &Rejection{Code: RejectUnavailable, Reason: why}
		}
	}
	res, err := k.Reduce(Rec{T: t, V: v}, in, now)
	if err != nil {
		return nil, nil, Result{}, asRejection(err)
	}
	// 0024 §5: an ask whose change is done or whose PR closed is void.
	res.Events = append(res.Events, voidAsks(t)...)
	// V0.84 (3): a change that moved since the owner said Not now is back.
	unparkStale(t)
	// The scheduler (#46): the Rules this Input made true fire now, in the
	// same records and the same transaction.
	more := EvalRules(Rec{T: t, V: v}, now)
	res.Events = append(res.Events, more.Events...)
	res.Effects = append(res.Effects, more.Effects...)
	// V0.quota-auto-continue: when a quota wait reaches its reset + buffer on
	// KindClock, clear the failure and wake the agent to continue.
	res.Events = append(res.Events, quotaContinueAfter(t, in, now)...)
	// V0.98: touchedAt and done, after the rules (a deploy watch's end).
	markDone(before, t, now)
	// V0.101: once more after the done marks (an ask on a change this
	// reduce finished is void); the 5-minute rule of Stopped on a failure is
	// read at each reduce (the clock's included).
	res.Events = append(res.Events, voidAsks(t)...)
	// V0.136: merge asks open on green CI and close on red.
	res.Events = append(res.Events, gateMergeAsks(t, now)...)
	t.Stopped = stoppedNow(t, now)
	// The agent feed (#47): the notice rules project this reduce.
	res.Effects = append(res.Effects, project(Reduced{Before: before, After: t, In: in, Events: res.Events}, now)...)
	// V0.185: the snooze until work is done reads all of the above.
	woke := wakeWhenDone(Rec{T: t, V: v}, in, now)
	res.Events = append(res.Events, woke.Events...)
	res.Effects = append(res.Effects, woke.Effects...)
	// V0.203: so does "settle when done".
	done := settleWhenWorkDone(Rec{T: t, V: v}, in, now)
	res.Events = append(res.Events, done.Events...)
	res.Effects = append(res.Effects, done.Effects...)
	// V0.210: a question in text reads the end state too.
	asked := textAskAfter(before, t, in, now)
	res.Events = append(res.Events, asked.Events...)
	res.Effects = append(res.Effects, asked.Effects...)
	// V0.settle-merge-fixes (1): so does a merge asked in text.
	res.Effects = append(res.Effects, mergeTextAfter(before, t, in).Effects...)
	return t, v, res, nil
}

// ReduceProject applies one Input of a project kind to a copy of its
// project: fresh for a kind that makes one, else the project args.id names
// in ps. after is nil when the reduce deleted the project. It is pure.
func ReduceProject(ps map[string]*Project, fresh *Project, in Input, now time.Time) (before, after *Project, res Result, err error) {
	k, err := kindOf(in, RecProject)
	if err != nil {
		return nil, nil, Result{}, err
	}
	cur := fresh
	if !k.Makes {
		id := mapArgs(in).Str("id")
		if cur = ps[id]; cur == nil {
			return nil, nil, Result{}, reject(RejectNotFound, "no project %s", id)
		}
		before = cur
	}
	p := *cur
	p.Quiet = slices.Clone(cur.Quiet)
	p.By = maps.Clone(cur.By)
	res, err = k.Reduce(Rec{P: &p}, in, now)
	if err != nil {
		return nil, nil, Result{}, asRejection(err)
	}
	if p.ID == "" { // the reduce emptied it: deleted
		return before, nil, res, nil
	}
	return before, &p, res, nil
}

// ReduceHost applies one Input of a host kind: it checks the sender and
// returns the kind's effects. It is pure.
func ReduceHost(in Input, now time.Time) (Result, error) {
	k, err := kindOf(in, RecHost)
	if err != nil {
		return Result{}, err
	}
	res, err := k.Reduce(Rec{}, in, now)
	if err != nil {
		return Result{}, asRejection(err)
	}
	return res, nil
}

// kindOf is the kind of an Input of record on, checked against its sender.
func kindOf(in Input, on Record) (*InputKind, error) {
	k := Kinds[in.Kind]
	if k == nil || k.On != on {
		return nil, reject(RejectBadArgs, "unknown action %s", in.Kind)
	}
	if !slices.Contains(k.From, in.Source.Principal()) {
		return nil, reject(RejectRights, "%s cannot send %s", in.Source, in.Kind)
	}
	return k, nil
}

// asRejection classifies a reducer's error.
func asRejection(err error) *Rejection {
	var r *Rejection
	switch {
	case errors.As(err, &r):
	case errors.Is(err, ErrBadArgs):
		r = &Rejection{Code: RejectBadArgs, Reason: err.Error()}
	default:
		r = &Rejection{Code: RejectConflict, Reason: err.Error()}
	}
	return r
}

// CloneThread is a deep copy of a thread, as stored.
func CloneThread(t *Thread) *Thread {
	if t == nil {
		return &Thread{}
	}
	b, _ := json.Marshal(t)
	c := &Thread{}
	json.Unmarshal(b, c)
	return c
}

func cloneView(v *View) *View {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	c := &View{}
	json.Unmarshal(b, c)
	return c
}

// argsOf reads an Input's args as the kind's typed value.
func argsOf[T any](in Input) (T, error) {
	a, ok := in.Args.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%w: %s got %T", ErrBadArgs, in.Kind, in.Args)
	}
	return a, nil
}

// mapArgs reads an Input's map args (a command's or a tool's).
func mapArgs(in Input) Args {
	switch a := in.Args.(type) {
	case Args:
		return a
	case map[string]any:
		return a
	}
	return Args{}
}

// trimmed is a string argument without surrounding space.
func (a Args) trimmed(k string) string { return strings.TrimSpace(a.Str(k)) }
