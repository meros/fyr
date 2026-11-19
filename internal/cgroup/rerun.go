package cgroup

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
)

// V0.rerun-classes (decision 0036 (F)): the command classes the owner marked
// rerunnable on this host (Settings → Automation). Under the memory guard such
// a job may be killed instead of frozen and run again. None by default.

// RerunClasses keeps the list in the store's KV. Readers never wait.
type RerunClasses struct {
	KV interface {
		GetKV(string) (string, error)
		SetKV(string, string) error
	}

	mu  sync.Mutex
	cur atomic.Pointer[[]string]
}

const rerunKey = "rerun-classes"

// MaxRerunClass is the longest entry the owner may add.
const MaxRerunClass = 200

// RerunView is the list as GET /api/load shows it.
type RerunView struct {
	Classes []string `json:"classes"`
}

// Load reads the owner's list; call it at start.
func (r *RerunClasses) Load() {
	var list []string
	if r.KV != nil {
		if raw, _ := r.KV.GetKV(rerunKey); raw != "" {
			json.Unmarshal([]byte(raw), &list)
		}
	}
	r.cur.Store(&list)
}

// List is the classes now, in the order added.
func (r *RerunClasses) List() []string {
	if r == nil {
		return nil
	}
	if p := r.cur.Load(); p != nil {
		return slices.Clone(*p)
	}
	return nil
}

// View is List for the front, never nil.
func (r *RerunClasses) View() RerunView {
	return RerunView{Classes: append([]string{}, r.List()...)}
}

// Set applies the owner's change (host.rerunClasses: add or remove one
// class) and keeps it.
func (r *RerunClasses) Set(args map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	list := r.List()
	if v, ok := args["add"].(string); ok {
		v = strings.Join(strings.Fields(v), " ")
		if v == "" || len(v) > MaxRerunClass {
			return errors.New("a class is a command of 1 to 200 characters, e.g. make test")
		}
		if !slices.Contains(list, v) {
			list = append(list, v)
		}
	}
	if v, ok := args["remove"].(string); ok {
		i := slices.Index(list, v)
		if i < 0 {
			return errors.New("no such rerunnable class")
		}
		list = slices.Delete(list, i, i+1)
	}
	if r.KV != nil {
		raw, _ := json.Marshal(list)
		if err := r.KV.SetKV(rerunKey, string(raw)); err != nil {
			return err
		}
	}
	r.cur.Store(&list)
	return nil
}

// Match is the owner's entry that marks a job rerunnable: one equal to its
// heavy class (with or without the "project: " the job records add), or one
// its command runs, the entry's words standing alone in it ("make test" in
// "cd web && make test 2>&1", not in "make testdata"). "" when none.
func (r *RerunClasses) Match(class, command string) string {
	for _, e := range r.List() {
		if class != "" && (class == e || strings.HasSuffix(class, ": "+e)) {
			return e
		}
		if command != "" && runsWords(command, e) {
			return e
		}
	}
	return ""
}

// runsWords says words appear in cmd bounded by the start, the end, a space or
// a shell separator on both sides.
func runsWords(cmd, words string) bool {
	cmd = strings.Join(strings.Fields(cmd), " ")
	bound := func(b byte) bool { return strings.IndexByte(" ;&|()'\"`", b) >= 0 }
	for i := 0; ; {
		j := strings.Index(cmd[i:], words)
		if j < 0 {
			return false
		}
		at := i + j
		end := at + len(words)
		if (at == 0 || bound(cmd[at-1])) && (end == len(cmd) || bound(cmd[end])) {
			return true
		}
		i = at + 1
	}
}
