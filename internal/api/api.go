// Package api holds the wire shapes of the host protocol (docs/API-v0.md):
// commands and their replies, and the records as clients see them. The engine, the server, the front and the
// provider registry share them; api imports only core.
package api

import (
	"time"

	"github.com/meros/fyr/internal/core"
)

// Cmd is one command from any surface.
type Cmd struct {
	CommandID string    `json:"commandId"`
	Action    string    `json:"action"`
	ThreadID  string    `json:"threadId,omitempty"`
	Args      core.Args `json:"args,omitempty"`
}

type Reply struct {
	OK       bool   `json:"ok"`
	ThreadID string `json:"threadId,omitempty"`
	Error    string `json:"error,omitempty"`
	Status   int    `json:"-"`
	Result   any    `json:"result,omitempty"`
}

// Thread is a thread as clients see it (docs/API-v0.md).
type Thread struct {
	*core.Thread
	Rev         int64             `json:"rev"`
	Attention   core.Attention    `json:"attention"`
	View        *View             `json:"view"`
	Unavailable map[string]string `json:"unavailable"`
	// NewProject is what "New project from this thread" starts with (V0.30,
	// V0.55).
	NewProject *NewProject `json:"newProject,omitempty"`
	// QuietUntil is the end of the quiet hours of the thread's project, while
	// they hold (V0.46).
	QuietUntil *time.Time `json:"quietUntil,omitempty"`
	// PR is the main change's PR, for the PR chip and tab (#32); the PR
	// itself lives on its change.
	PR *core.PR `json:"pr,omitempty"`
	// Project is the name of the thread's project (#33: derived, not
	// stored).
	Project string `json:"project"`
	// LiveView is live.enabled, the flag clients read (#33).
	LiveView bool `json:"liveView,omitempty"`
	// The settle as clients read it, derived from thread.settle and the
	// settle Rules (#33, #46): settledAt, settleWhenDone (a "done" Rule),
	// settleWhenMerged (the main change reaching landed) and settleWhen (a
	// change Rule).
	SettledAt        *time.Time     `json:"settledAt,omitempty"`
	SettleWhenDone   bool           `json:"settleWhenDone,omitempty"`
	SettleWhenMerged bool           `json:"settleWhenMerged,omitempty"`
	SettleWhen       *core.SettleOn `json:"settleWhen,omitempty"`
	// Settle shadows thread.settle, which the fields above carry.
	Settle *struct{} `json:"settle,omitempty"`
	// Feed shadows thread.feed: the notices are the agent's (#47).
	Feed *struct{} `json:"feed,omitempty"`
	// Changes are the thread's own changes and the host records it shows
	// (#35), each record once with the threads that show it.
	Changes []core.Change `json:"changes,omitempty"`
}

// View is the owner's view as clients read it: the snooze is derived
// from the View's snooze Rule (#46).
type View struct {
	*core.View
	SnoozeUntil *time.Time `json:"snoozeUntil,omitempty"`
	SnoozedAt   *time.Time `json:"snoozedAt,omitempty"`
}

// NewProject is what "New project from this thread" starts with (SPEC 0.5
// V0.30, V0.55): the thread's folder and its name.
type NewProject struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

// Project is a project as clients see it (SPEC 0.5 V0.23).
type Project struct {
	core.Project
	// Rev is when this copy was built (#42): a client keeps the higher of a
	// snapshot's copy and a stream frame's.
	Rev      int64     `json:"rev"`
	Threads  int       `json:"threads"`
	LastUsed time.Time `json:"lastUsed"`
	// QuietUntil is the end of the quiet hours the project is in (V0.46).
	QuietUntil *time.Time `json:"quietUntil,omitempty"`
}
