// Package api holds the wire shapes of the host protocol (docs/API-v0.md):
// commands and their replies, and the records as clients see them. The engine, the server, the front and the
// provider registry share them; api imports only core.
package api

import (
	"encoding/json"
	"net/http"
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
	// RootPrompt is why a root run request cannot ask for the owner's
	// password on the thread's host, or "" (V0.149 (4)); set only when the
	// thread has one.
	RootPrompt string `json:"rootPrompt,omitempty"`
}

// View is the owner's view as clients read it: the snooze is derived
// from the View's snooze Rule (#46).
type View struct {
	*core.View
	SnoozeUntil *time.Time `json:"snoozeUntil,omitempty"`
	SnoozedAt   *time.Time `json:"snoozedAt,omitempty"`
	// SnoozeWhenDone: the snooze lasts until work is done (V0.185);
	// SnoozeUntil is then its maximum time.
	SnoozeWhenDone bool `json:"snoozeWhenDone,omitempty"`
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

// State is GET /api/state (docs/API-v0.md). A host fills it with its own
// records (T api.Thread, P api.Project, F its profiles); a front with its
// hosts' records as it got them, stamped with their host, and Hosts (V0.51,
// V0.54).
type State[T, P, F any] struct {
	Now     time.Time `json:"now"`
	User    string    `json:"user"`
	BuildID string    `json:"buildId"`
	Host    string    `json:"host"`
	// HostID is the install's key id (V0.124, decision 0026): a host's own,
	// or, in a front's merged state, the front's install.
	HostID   string   `json:"hostId,omitempty"`
	Threads  []T      `json:"threads"`
	Actions  []Action `json:"actions"`
	Profiles []F      `json:"profiles"`
	Projects []P      `json:"projects"`
	// Partial: ?since=REV listed only the threads changed after REV (V0.38).
	Partial bool `json:"partial"`
	// Deploy is the deploy progress to show (V0.39), or nil.
	Deploy any `json:"deploy"`
	// Stall is the host's newest stall of the last 24 h (V0.147), or nil.
	Stall any `json:"stall,omitempty"`
	// Stalls are a front's hosts' stalls, each with its host (V0.147).
	Stalls []any `json:"stalls,omitempty"`
	// Hosts are a front's hosts (V0.51); a host sends none.
	Hosts []Host `json:"hosts,omitempty"`
	// ProjectOrder is the owner's project order, one list of ids across a
	// front's hosts (V0.121); absent until the owner sets one.
	ProjectOrder []string `json:"projectOrder,omitempty"`
	// HostOrder is the owner's host order as a host keeps it (V0.132):
	// {names, at}; nil when none was set. A front reads the newest from its
	// hosts and orders hosts[] by it.
	HostOrder any `json:"hostOrder,omitempty"`
	// Machine is what this host runs on (V0.170): laptop, desktop or server;
	// OS is its operating system ("linux", "darwin"). The fronts draw its mark.
	Machine string `json:"machine,omitempty"`
	OS      string `json:"os,omitempty"`
	// HostMarks: on a host, its copy of the owner's host marks ({marks, at},
	// V0.171), nil when none was set; on a front, the newest marks by host
	// install id ({id: {icon, color}}).
	HostMarks any `json:"hostMarks,omitempty"`
}

// Action is an owner action as clients list it.
type Action struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Key   string `json:"key,omitempty"`
	Bulk  bool   `json:"bulk,omitempty"`
}

// Host is one of a front's hosts in its state (V0.54): online or since when
// not, its profiles and its deploy.
type Host struct {
	Name   string `json:"name"`
	Online bool   `json:"online"`
	Since  string `json:"since,omitempty"`
	Error  string `json:"error,omitempty"`
	// Profiles are the host's own (decision 0015); null when the host sent none.
	Profiles []any `json:"profiles"`
	Deploy   any   `json:"deploy,omitempty"`
	// Found: discovery found it on the tailnet (V0.123); else it is configured.
	Found bool `json:"found,omitempty"`
	// ID is the host's key id (V0.124, decision 0026): the one it reports,
	// else the one the front pinned when it first saw it.
	ID string `json:"id,omitempty"`
	// IDWas: the host now reports another id than the pinned one; the front
	// keeps following it and shows both until the owner trusts the new id.
	IDWas string `json:"idWas,omitempty"`
	// URL is the address the front dials for this host, as configured or
	// as discovery found it (Settings → Hosts shows it and its route).
	URL string `json:"url,omitempty"`
	// Machine and OS as the host reports them (V0.170); none until it is seen.
	Machine string `json:"machine,omitempty"`
	OS      string `json:"os,omitempty"`
	// Link is how the front reaches the host (decision 0031 2.7, 0030 5b).
	Link *Link `json:"link,omitempty"`
}

// Link is the state of the front's link to one host.
type Link struct {
	State  string     `json:"state"`            // up | connecting | down
	Via    string     `json:"via"`              // peer | whois | pipe
	Opened string     `json:"opened,omitempty"` // here | them
	Since  string     `json:"since,omitempty"`
	Error  *LinkError `json:"error,omitempty"`
}

// LinkError says why a link is down: kind is one of acl, no-route, key-mismatch,
// refused, timeout, removed, revoked.
type LinkError struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// WriteJSON is the one JSON reply of the host and the front.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
