package deploy

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// #151: one record per update attempt, kept in kv `update_log` (the newest
// 50), so "what runs here and how did it get here" is one list: the build,
// the commit, why it ran, each phase with its time, the result and the logs
// kept with the build. The daemon that switched writes the start; the new
// daemon (or the rollback report) writes the end.

// Update is one attempt.
type Update struct {
	Build     string    `json:"build"`
	Commit    string    `json:"commit,omitempty"`
	Trigger   string    `json:"trigger"` // deployable, agent, owner
	Local     bool      `json:"local,omitempty"`
	Branch    string    `json:"branch,omitempty"`
	Thread    string    `json:"thread,omitempty"`
	Result    string    `json:"result"` // inProgress, installed, failed, rolledBack
	Reason    string    `json:"reason,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt,omitzero"`
	Phases    []Phase   `json:"phases,omitempty"`
	Logs      []string  `json:"logs,omitempty"`
	// V0.update-changelog: what this update brings over what ran before it: the commits since (newest first, at
	// most changesKeep) and how many there are in all; From is the commit it was counted from.
	From       string   `json:"from,omitempty"`
	Changes    []Change `json:"changes,omitempty"`
	ChangesAll int      `json:"changesAll,omitempty"`
}

// Change is one commit of an update's changelog: its sha and the first line of its message.
type Change struct {
	Commit string `json:"commit"`
	Title  string `json:"title"`
}

// changesKeep bounds the commits kept per record (the record lives in one kv value of 50 records).
const changesKeep = 40

// Phase is one step of an update and when it began.
type Phase struct {
	Name string    `json:"name"`
	At   time.Time `json:"at"`
}

// The results of an update.
const (
	UpdateRunning    = "inProgress"
	UpdateLive       = "installed"
	UpdateFailed     = "failed"
	UpdateRolledBack = "rolledBack"
	UpdateReplaced   = "replaced"
)

const (
	logKey  = "update_log"
	logKeep = 50
)

// LogNames are the files of a build the record links to.
var LogNames = []string{"build.log", "test.log", "smoke.log"}

var recMu sync.Mutex

// Updates is the record, newest first; an empty record is an empty list, never null (a host that never updated
// itself, such as a NixOS host, sent "updates": null and the Automation page threw on it).
func (d *Deployer) Updates() []Update {
	recMu.Lock()
	defer recMu.Unlock()
	if u := d.readLog(); u != nil {
		return u
	}
	return []Update{}
}

func (d *Deployer) readLog() []Update {
	raw, _ := d.DB.GetKV(logKey)
	var out []Update
	json.Unmarshal([]byte(raw), &out)
	return out
}

// edit changes the record of build (made with mk when missing) and stores it.
func (d *Deployer) edit(build string, mk func() Update, fn func(*Update)) {
	if build == "" {
		return
	}
	if d.editLocked(build, mk, fn) {
		d.changed() // V0.host-updates: the last update moved
	}
}

func (d *Deployer) editLocked(build string, mk func() Update, fn func(*Update)) bool {
	recMu.Lock()
	defer recMu.Unlock()
	list := d.readLog()
	i := -1
	for j := range list {
		if list[j].Build == build && (list[j].Result == UpdateRunning || mk == nil) {
			i = j
			break
		}
	}
	if i < 0 {
		if mk == nil {
			return false
		}
		list = append([]Update{mk()}, list...)
		i = 0
	}
	fn(&list[i])
	list[i].Logs = d.logsOf(build)
	if len(list) > logKeep {
		list = list[:logKeep]
	}
	b, _ := json.Marshal(list)
	d.DB.SetKV(logKey, string(b))
	return true
}

func (d *Deployer) logsOf(build string) []string {
	var out []string
	for _, n := range LogNames {
		if _, err := os.Stat(filepath.Join(d.Data, "builds", build, n)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// recordPhase notes a progress report in the record of its build.
func (d *Deployer) recordPhase(p Progress, start func() Update) {
	switch p.Phase {
	case "failed":
		d.edit(p.Build, start, func(u *Update) {
			u.Result, u.Reason, u.EndedAt = UpdateFailed, firstLine(p.Detail), p.At
			if u.Reason == "" {
				u.Reason = "failed"
			}
		})
	case "done":
		if p.Client { // the client-only path ends here; a restart ends in ReportPending
			d.edit(p.Build, start, func(u *Update) { u.Result, u.EndedAt = UpdateLive, p.At })
		}
	default:
		d.edit(p.Build, start, func(u *Update) {
			if n := len(u.Phases); n == 0 || u.Phases[n-1].Name != p.Phase {
				u.Phases = append(u.Phases, Phase{Name: p.Phase, At: p.At})
			}
		})
	}
}

// finish ends the record of build after the switch: live or rolled back.
func (d *Deployer) finish(build, result, reason string) {
	d.edit(build, nil, func(u *Update) {
		if u.Result == UpdateRunning {
			u.Result, u.Reason, u.EndedAt = result, reason, time.Now()
		}
	})
}

// UpdateLog reads one log of a build for the record (#151).
func (d *Deployer) UpdateLog(build, name string) ([]byte, error) {
	ok := false
	for _, n := range LogNames {
		ok = ok || n == name
	}
	if !ok || build == "" || filepath.Base(build) != build || build[0] == '.' {
		return nil, errors.New("no such log")
	}
	return os.ReadFile(filepath.Join(d.Data, "builds", build, name))
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}

// liveCommit is the commit of the build that runs now: the record of the newest installed update of the live build,
// else the live build id when it is a short sha (a release build is builds/<sha12>); "" when neither says.
func (d *Deployer) liveCommit() string {
	live := d.live()
	if live == "" {
		return ""
	}
	recMu.Lock()
	list := d.readLog()
	recMu.Unlock()
	for _, u := range list {
		if u.Build == live && u.Result == UpdateLive && u.Commit != "" {
			return u.Commit
		}
	}
	if len(live) == 12 && isHex(live) {
		return live
	}
	return ""
}

// recordChanges notes the changelog of an update in its record: the commits from what runs now up to commit, read
// by list (newest first, at most changesKeep, and the count of all). Best effort: when the list cannot be read, or
// nothing runs that it can count from, the record has none.
func (d *Deployer) recordChanges(prog *Progress, commit string, list func(from, to string) ([]Change, int, error)) {
	from := d.liveCommit()
	if from == "" || commit == "" || sameCommit(from, commit) {
		return
	}
	cs, all, err := list(from, commit)
	if err != nil {
		log.Printf("changelog %s..%s: %v", short7(from), short7(commit), err)
		return
	}
	d.edit(prog.Build, prog.start, func(u *Update) { u.From, u.Changes, u.ChangesAll = from, cs, all })
}

// parseChanges reads "sha<TAB>title" lines, oldest first when oldestFirst, into a newest-first list of at most
// changesKeep, and the count of all.
func parseChanges(out string, oldestFirst bool) ([]Change, int) {
	var cs []Change
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		sha, title, ok := strings.Cut(l, "\t")
		if !ok || !isSHA(sha) {
			continue
		}
		cs = append(cs, Change{Commit: sha, Title: title})
	}
	if oldestFirst {
		for i, j := 0, len(cs)-1; i < j; i, j = i+1, j-1 {
			cs[i], cs[j] = cs[j], cs[i]
		}
	}
	all := len(cs)
	if len(cs) > changesKeep {
		cs = cs[:changesKeep]
	}
	return cs, all
}
