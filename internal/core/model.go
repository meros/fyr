// Package core holds Fyr's pure domain logic: the thread model, the
// attention function and the Action registry. Nothing in this package does
// I/O, so every rule here is covered by table tests.
package core

import "time"

// Owner is the only user in v1. The data model keeps the user dimension
// (R19) so several users per host need no migration later.
const Owner = "owner"

// Thread is one conversation with one agent session in one directory.
type Thread struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Cwd       string `json:"cwd"`
	Project   string `json:"project"`
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

	// Run state, written only by observations from the agent.
	Running      bool       `json:"running"`
	RunStartedAt *time.Time `json:"runStartedAt,omitempty"`
	Activity     string     `json:"activity,omitempty"` // last tool or text, one line
	Subagents    int        `json:"subagents,omitempty"`
	// ActivityAt moves when something happens that the user should look at:
	// a run ends, a question opens, a failure, a PR change.
	ActivityAt time.Time `json:"activityAt"`
	Summary    string    `json:"summary,omitempty"` // last assistant text, one line

	Tasks     []Task     `json:"tasks,omitempty"` // subagents and background tasks (V0.16)
	Failure   *Failure   `json:"failure,omitempty"`
	Decisions []Decision `json:"decisions,omitempty"`
	PR        *PR        `json:"pr,omitempty"`
	// Changes are the thread's change records (V0.59, decision 0018).
	Changes []Change `json:"changes,omitempty"`
	// ChangesCheckedAt is the last change tool call (V0.62).
	ChangesCheckedAt *time.Time `json:"changesCheckedAt,omitempty"`
	// Offers are the thread's and its live view's actions; OfferRuns their
	// last results (V0.60). A change holds its own.
	Offers    []Offer    `json:"offers,omitempty"`
	OfferRuns []OfferRun `json:"offerRuns,omitempty"`
	// DismissedPRs are PR URLs the owner put away (V0.36); they never attach again.
	DismissedPRs []string `json:"dismissedPRs,omitempty"`
	// Outbox holds the messages the agent has not read yet (V0.33).
	Outbox []OutMsg `json:"outbox,omitempty"`
	// LiveView is the flag: the agent keeps a pinned live view (V0.37).
	LiveView bool `json:"liveView,omitempty"`
	// Live is the view itself: page, state, times, headline (V0.37).
	Live *LiveView `json:"live,omitempty"`
	// Handover is set while the thread moves to another profile (V0.45).
	Handover *Handover `json:"handover,omitempty"`

	SettledAt        *time.Time `json:"settledAt,omitempty"`
	ArchivedAt       *time.Time `json:"archivedAt,omitempty"`
	SettleWhenDone   bool       `json:"settleWhenDone,omitempty"`
	SettleWhenMerged bool       `json:"settleWhenMerged,omitempty"`
	// SettleWhen settles when a change reaches a phase (V0.59 (7)).
	SettleWhen *SettleWhen `json:"settleWhen,omitempty"`

	Source string `json:"source"`           // "fyr" or "t3"
	Parent string `json:"parent,omitempty"` // the thread whose agent started it (V0.22)
	T3ID   string `json:"t3Id,omitempty"`
}

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

// View is the per-user state of one entity (R19): snooze and read watermark.
type View struct {
	User        string     `json:"user"`
	Entity      string     `json:"entity"`
	SeenAt      time.Time  `json:"seenAt"`
	SnoozeUntil *time.Time `json:"snoozeUntil,omitempty"`
	SnoozedAt   *time.Time `json:"snoozedAt,omitempty"`
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
	Type        string     `json:"type,omitempty"`   // local_agent, local_bash, …
	Status      string     `json:"status,omitempty"` // running, completed, failed, stopped
	Activity    string     `json:"activity,omitempty"`
	Summary     string     `json:"summary,omitempty"`
	ToolUses    int        `json:"toolUses,omitempty"`
	Tokens      int        `json:"tokens,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// RunningTasks counts the thread's running tasks.
func (t *Thread) RunningTasks() int {
	n := 0
	for _, k := range t.Tasks {
		if k.Status == "running" {
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
	ID        string     `json:"id"`
	Kind      string     `json:"kind"` // "ask", "approval" or "effect"
	Assignee  string     `json:"assignee"`
	Questions []Question `json:"questions,omitempty"`
	Tool      string     `json:"tool,omitempty"`
	Input     any        `json:"input,omitempty"`
	Agent     string     `json:"agent,omitempty"` // which subagent asked, when known
	Effect    *EffectReq `json:"effect,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// EffectReq is an effect the agent asked Fyr to run (SPEC 5.10, V0.12).
type EffectReq struct {
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

// PR is the pull request a thread serves.
type PR struct {
	URL       string     `json:"url"`
	Repo      string     `json:"repo"`
	Number    int        `json:"number"`
	Title     string     `json:"title,omitempty"`
	State     string     `json:"state"`  // OPEN, MERGED, CLOSED
	Checks    string     `json:"checks"` // pass, fail, pending, none
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

// Blocking reports why the PR needs the user, or "" when it does not.
func (p *PR) Blocking() string {
	if p == nil || p.State != "OPEN" {
		return ""
	}
	switch {
	case p.Checks == "fail":
		return "CI failed"
	case p.Conflict:
		return "Merge conflict"
	case p.Review == "CHANGES_REQUESTED":
		return "Changes requested"
	}
	return ""
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
