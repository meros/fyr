package deploy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
}

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
