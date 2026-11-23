// Package api holds the wire shapes of the host protocol (docs/API-v0.md):
// commands and their replies, and the records as clients see them. The engine, the server, the front and the
// provider registry share them; api imports only core.
package api

import (
	"encoding/json"
	"errors"
	"github.com/meros/fyr/internal/httpguard"
	"io"
	"log"
	"net/http"
	"sort"
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
	// PushQuiet: the attention state is only an auto-merge at work, so it
	// sends no push (V0.200).
	PushQuiet bool `json:"pushQuiet,omitempty"`
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
	// Wakes are the agent's armed wake-ups, soonest first
	// (V0.wakeups-via-watch): its watches on a time.
	Wakes []core.WakeUp `json:"wakes,omitempty"`
	// Waits are all the agent's armed watches (V0.waits-strip), soonest wake-up first.
	Waits []core.Wait `json:"waits,omitempty"`
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
	// Usage is the thread's tokens so far, from the host's usage ledger
	// (V0.usage-ledger); absent before its first counted result.
	Usage *Usage `json:"usage,omitempty"`
}

// Usage is a sum of tokens by kind (V0.usage-ledger, decision 0041).
// Thinking is part of Output; Total is a plain sum of the kinds, a count
// and not a weight.
type Usage struct {
	Input        int64 `json:"input"`
	Output       int64 `json:"output"`
	CacheRead    int64 `json:"cacheRead"`
	CacheWrite5m int64 `json:"cacheWrite5m"`
	CacheWrite1h int64 `json:"cacheWrite1h"`
	Thinking     int64 `json:"thinking"`
	Total        int64 `json:"total"`
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
	// Repo is the folder's normalised git origin, read from its .git/config;
	// the client suggests linking projects that have the same (decision 0034).
	Repo string `json:"repo,omitempty"`
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
	// More and Cursor: ?limit=N answered the newest N threads (V0.state-paging);
	// More says older ones remain, and Cursor (a string: a rev is above 2^53) is
	// the oldest rev in this page, to ask the next page with ?before=CURSOR.
	More   bool   `json:"more,omitempty"`
	Cursor string `json:"cursor,omitempty"`
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
	// AgentMark: on a host, what its own agent set for its mark and its last
	// setup run (V0.208), nil when neither; a front lays it under the owner's.
	AgentMark any `json:"agentMark,omitempty"`
	// Update: on a host, its update state (V0.host-updates): the commit it
	// runs, the release it wants, how far behind, the last update and a
	// release held back; nil on a host without a release source.
	Update any `json:"update,omitempty"`
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
	// Update is the host's update state (V0.host-updates), as it sent it.
	Update any `json:"update,omitempty"`
	// Found: not in the config; the roster brought it (a paired install,
	// V0.retire-discovery; V0.123's tailnet discovery did until then).
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

// StalledCode is the code of a host's 503 past a request's deadline (V0.181):
// the host is stuck, not gone.
const StalledCode = "host-stalled"

// LinkError says why a link is down: kind is one of acl, no-route, key-mismatch,
// refused, timeout, removed, revoked, stalled (V0.181: it answers nothing in
// time).
type LinkError struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// WriteJSON is the one JSON reply of the host and the front.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: write json: %v", err)
	}
}

// DecodeBody reads the request body as JSON into v, at most limit bytes. On a
// failure it answers {"ok":false,"error":...} (400, or 413 over the cap) and
// returns false: the one answer of the host and the front to a bad body.
func DecodeBody(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	return decode(w, r, v, limit, false)
}

// DecodeOptional is DecodeBody for a call whose fields are all optional: an
// empty body is fine and leaves v as it was. Any other failure (bad JSON, over
// the cap) is answered as DecodeBody does, so a malformed body never leaves a
// partly decoded v behind for the handler to act on.
func DecodeOptional(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	return decode(w, r, v, limit, true)
}

func decode(w http.ResponseWriter, r *http.Request, v any, limit int64, emptyOK bool) bool {
	err := httpguard.ReadJSON(w, r, v, limit)
	if err == nil || (emptyOK && errors.Is(err, io.EOF)) {
		return true
	}
	var big *http.MaxBytesError
	if errors.As(err, &big) {
		WriteJSON(w, http.StatusRequestEntityTooLarge, Reply{Error: "request body too large"})
	} else {
		WriteJSON(w, http.StatusBadRequest, Reply{Error: "bad JSON: " + err.Error()})
	}
	return false
}

// PageByRev is one page of a state's threads, newest first (V0.state-paging):
// those with a rev below before (all when before is 0), at most limit of them
// (all when limit is 0). more says older ones remain after the page; cursor is
// the oldest rev in it, the next page's before. The input is not changed.
func PageByRev[T any](items []T, rev func(T) int64, before int64, limit int) (page []T, more bool, cursor int64) {
	page = make([]T, 0, len(items))
	for _, it := range items {
		if before <= 0 || rev(it) < before {
			page = append(page, it)
		}
	}
	if limit <= 0 {
		return page, false, 0
	}
	sort.SliceStable(page, func(i, j int) bool { return rev(page[i]) > rev(page[j]) })
	if len(page) > limit {
		page, more = page[:limit], true
	}
	if len(page) > 0 {
		cursor = rev(page[len(page)-1])
	}
	return page, more, cursor
}
