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
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	Cwd        string    `json:"cwd"`
	Project    string    `json:"project"`
	Owner      string    `json:"owner"`
	Visibility string    `json:"visibility"`
	Profile    string    `json:"profile"`
	Model      string    `json:"model"`
	SessionID  string    `json:"sessionId"`
	Branch     string    `json:"branch,omitempty"` // read with git after each run
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`

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

	SettledAt        *time.Time `json:"settledAt,omitempty"`
	ArchivedAt       *time.Time `json:"archivedAt,omitempty"`
	SettleWhenDone   bool       `json:"settleWhenDone,omitempty"`
	SettleWhenMerged bool       `json:"settleWhenMerged,omitempty"`

	Source string `json:"source"` // "fyr" or "t3"
	T3ID   string `json:"t3Id,omitempty"`
}

// View is the per-user state of one entity (R19): snooze and read watermark.
type View struct {
	User        string     `json:"user"`
	Entity      string     `json:"entity"`
	SeenAt      time.Time  `json:"seenAt"`
	SnoozeUntil *time.Time `json:"snoozeUntil,omitempty"`
	SnoozedAt   *time.Time `json:"snoozedAt,omitempty"`
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

// Running counts the thread's running tasks.
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
	CheckedAt *time.Time `json:"checkedAt,omitempty"`
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
