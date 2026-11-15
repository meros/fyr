// Package core holds Fyr's pure domain logic: the thread model, the
// attention function and the Action registry. Nothing in this package does
// I/O, so every rule here is covered by table tests.
package core

import "time"

// Owner is the only user in v1. The data model keeps the user dimension
// (R19) so several users per host need no migration later.
const Owner = "owner"

// Thread is one conversation with one agent session in one directory. Its
// parts are components (#33): the Conversation (the agent's run state), the
// Live view, the Settle and the Actions. The project's name is not stored:
// clients get it from ProjectID.
type Thread struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// TitleBy is who named the thread last: the owner's name is kept
	// against the agent's (V0.69); "" for a title from the first message.
	TitleBy   string `json:"titleBy,omitempty"`
	Cwd       string `json:"cwd"`
	ProjectID string `json:"projectId,omitempty"` // V0.23
	// ProjectManual: the thread was filed by hand; folder matching never
	// moves it again (V0.55).
	ProjectManual bool      `json:"projectManual,omitempty"`
	Owner         string    `json:"owner"`
	Visibility    string    `json:"visibility"`
	Profile       string    `json:"profile"`
	Model         string    `json:"model"`
	Effort        string    `json:"effort,omitempty"` // V0.50: "" is the provider's default
	SessionID     string    `json:"sessionId"`
	Branch        string    `json:"branch,omitempty"` // read with git after each run
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`

	// Conversation is the agent's run state. Its fields stay at the top
	// level of the stored and the wire JSON.
	Conversation
	// ActivityAt moves when something happens that the user should look at:
	// a run ends, a question opens, a failure, a PR change.
	ActivityAt time.Time  `json:"activityAt"`
	Decisions  []Decision `json:"decisions,omitempty"`
	// Changes are the thread's change records (V0.59, decision 0018). A PR
	// lives on its code change (#32).
	Changes []Change `json:"changes,omitempty"`
	// MainChange is the id of the code change whose PR the PR tab shows and
	// the PR commands act on (#32); "" when the thread has no main PR.
	MainChange string `json:"mainChange,omitempty"`
	// ChangesCheckedAt is the last change tool call (V0.62).
	ChangesCheckedAt *time.Time `json:"changesCheckedAt,omitempty"`
	// Actions are the thread's own actions and their last runs (V0.60, #31).
	// A change and the live view hold their own.
	Actions
	// Live is the live view: whether the agent keeps one, its page, state,
	// upkeep times and actions (V0.37).
	Live *LiveView `json:"live,omitempty"`
	// Settle is when the thread settled (#33); what settles it are Rules.
	Settle Settle `json:"settle,omitzero"`
	// Rules are the thread's armed watches (SPEC 7, #46): settle conditions,
	// the deploy watch, the built-in actions and the agent's own.
	Rules []Rule `json:"rules,omitempty"`
	// Feed is the agent's queue of notices not delivered yet (#47).
	Feed       Feed       `json:"feed,omitzero"`
	ArchivedAt *time.Time `json:"archivedAt,omitempty"`

	Source string `json:"source"`           // "fyr" or "t3"
	Parent string `json:"parent,omitempty"` // the thread whose agent started it (V0.22)
	T3ID   string `json:"t3Id,omitempty"`
}

// Conversation is the run state of the thread's agent session (SPEC 3.3),
// written only by observations from the agent.
type Conversation struct {
	Running      bool       `json:"running"`
	RunStartedAt *time.Time `json:"runStartedAt,omitempty"`
	Activity     string     `json:"activity,omitempty"` // last tool or text, one line
	Subagents    int        `json:"subagents,omitempty"`
	Summary      string     `json:"summary,omitempty"` // last assistant text, one line
	Tasks        []Task     `json:"tasks,omitempty"`   // subagents and background tasks (V0.16)
	Failure      *Failure   `json:"failure,omitempty"`
	// RunEnds counts the runs that ended: runEnded Rules compare with it.
	RunEnds int `json:"runEnds,omitempty"`
	// Outbox holds the messages the agent has not read yet (V0.33).
	Outbox []OutMsg `json:"outbox,omitempty"`
	// Handover is set while the thread moves to another profile (V0.45).
	Handover *Handover `json:"handover,omitempty"`
}

// ViewOn reports that the agent keeps a live view (V0.37).
func (t *Thread) ViewOn() bool { return t.Live != nil && t.Live.Enabled }

// Handover is a move to another profile that is going on (SPEC 0.5 V0.45).
type Handover struct {
	To    string    `json:"to"`
	Model string    `json:"model,omitempty"`
	At    time.Time `json:"at"`
}

// OutMsg is a message sent to the agent and not read yet (SPEC 0.5 V0.33).
// State is "sent" (written, no report from the agent) or "queued" (in the
// agent's queue).
type OutMsg struct {
	ID    string `json:"id"`
	State string `json:"state"`
}

// View is the per-user state of one entity (R19): the read watermark and the
// user's Rules (a snooze, SPEC 7.1 viewer).
type View struct {
	User   string    `json:"user"`
	Entity string    `json:"entity"`
	SeenAt time.Time `json:"seenAt"`
	Rules  []Rule    `json:"rules,omitempty"`
	// ChangesLook is the owner's last look at the Changes tab and the
	// Viewed marks (V0.65). Not sent in the thread DTO.
	ChangesLook *ChangesLook `json:"changesLook,omitempty"`
}

// ChangesLook is what the Changes tab showed: the head commit and each
// file's blob id, and the files marked Viewed with the blob they had then
// (SPEC 0.5 V0.65).
type ChangesLook struct {
	Head   string            `json:"head"`
	At     time.Time         `json:"at"`
	Files  map[string]string `json:"files,omitempty"`
	Viewed map[string]string `json:"viewed,omitempty"`
}

// FileViewed reports whether the file was marked Viewed with this content;
// the mark resets by itself when the content changes.
func FileViewed(v *View, path, blob string) bool {
	if v == nil || v.ChangesLook == nil {
		return false
	}
	b, ok := v.ChangesLook.Viewed[path]
	return ok && b == blob
}

// Task is a native subagent or background task of the thread's session
// (SPEC 0.5 V0.16), as the CLI reports it.
type Task struct {
	ID          string     `json:"id"`
	Description string     `json:"description,omitempty"`
	Type        string     `json:"type,omitempty"` // local_agent, local_bash, …
	Status      TaskStatus `json:"status,omitempty"`
	Activity    string     `json:"activity,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	ToolUses    int        `json:"toolUses,omitempty"`
	Tokens      int        `json:"tokens,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// RunningTasks counts the conversation's running tasks.
func (t *Conversation) RunningTasks() int {
	n := 0
	for _, k := range t.Tasks {
		if k.Status == TaskRunning {
			n++
		}
	}
	return n
}

// Failure is the reason the last run did not finish normally.
type Failure struct {
	Class   string    `json:"class"` // api, auth, mcp, process, restart, error
	Message string    `json:"message"`
	At      time.Time `json:"at"`
}

// Decision is a question or an approval that waits for the user.
type Decision struct {
	ID        string       `json:"id"`
	Kind      DecisionKind `json:"kind"`
	Assignee  string       `json:"assignee"`
	Questions []Question   `json:"questions,omitempty"`
	Tool      string       `json:"tool,omitempty"`
	Input     any          `json:"input,omitempty"`
	Agent     string       `json:"agent,omitempty"` // which subagent asked, when known
	Effect    *EffectReq   `json:"effect,omitempty"`
	CreatedAt time.Time    `json:"createdAt"`
}

// EffectReq is an effect the agent asked Fyr to run (SPEC 5.10, V0.12).
type EffectReq struct {
	// ID is the effect that runs it, set by the runner; a result reported
	// later (a deploy) names it.
	ID     string         `json:"id,omitempty"`
	Action string         `json:"action"`
	Args   map[string]any `json:"args"`
	Reason string         `json:"reason,omitempty"`
}

type Question struct {
	Question    string   `json:"question"`
	Header      string   `json:"header,omitempty"`
	Options     []Option `json:"options"`
	MultiSelect bool     `json:"multiSelect,omitempty"`
}

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// PR is a pull request snapshot as GitHub reported it. It lives on its code
// change (Change.PR, #32).
type PR struct {
	URL       string     `json:"url"`
	Repo      string     `json:"repo"`
	Number    int        `json:"number"`
	Title     string     `json:"title,omitempty"`
	State     string     `json:"state"` // OPEN, MERGED, CLOSED
	Checks    Checks     `json:"checks"`
	Review    string     `json:"review,omitempty"`
	Conflict  bool       `json:"conflict,omitempty"`
	Branch    string     `json:"branch,omitempty"` // the head branch (V0.59)
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
	// V0.66: the head and merge commits, and the failed checks' annotations
	// (carried to the change, not stored on the PR).
	Head        string       `json:"head,omitempty"`
	MergeSHA    string       `json:"mergeSha,omitempty"`
	MergedAt    *time.Time   `json:"mergedAt,omitempty"`
	Annotations []Annotation `json:"-"`
}

// Main is the thread's main code change, the one with the PR the PR tab
// shows (#32), or nil.
func (t *Thread) Main() *Change {
	c := FindChange(t, t.MainChange)
	if c == nil || c.PR == nil || c.Dismissed {
		return nil
	}
	return c
}

// MainPR is the main change's PR snapshot, or nil.
func (t *Thread) MainPR() *PR {
	if c := t.Main(); c != nil {
		return c.PR
	}
	return nil
}

// SetOutbox records a message's delivery state: "sent" and "queued" keep it
// in the outbox, any other state (read, cancelled, lost) removes it.
func SetOutbox(t *Thread, id, state string) {
	for i, m := range t.Outbox {
		if m.ID == id {
			if state == "sent" || state == "queued" {
				t.Outbox[i].State = state
			} else {
				t.Outbox = append(t.Outbox[:i:i], t.Outbox[i+1:]...)
			}
			return
		}
	}
	if state == "sent" || state == "queued" {
		t.Outbox = append(t.Outbox, OutMsg{ID: id, State: state})
	}
}

// OutboxState is a message's state while it is unread, else "".
func OutboxState(t *Thread, id string) string {
	for _, m := range t.Outbox {
		if m.ID == id {
			return m.State
		}
	}
	return ""
}
