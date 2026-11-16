package engine

// Host change records (#35, decision 0018 addendum): an observed branch is
// one record per repo and branch on the host, stored once with its own
// events. Each Input of a record goes down this path: reduce the record,
// then send the record to every thread that shows it as a change.observed
// Input through the thread's reduce, and store all of it in one
// transaction. Each thread gets its own notices from its own reduce.

import (
	"encoding/json"
	"log"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/meros/fyr/internal/agent"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// GitReader reads checkouts for the git observer. agent.Git reads with git.
type GitReader interface {
	Facts(dir string) core.GitFacts
	End(dir, branch, base, lastHead string) (gone, merged, known bool)
}

func (e *Engine) git() GitReader {
	if e.Git != nil {
		return e.Git
	}
	return agent.Git{}
}

// loadRecords reads the host records, moves the observed changes threads
// stored before #35 into them, and drops records that ended over 30 days
// ago with the threads' references to them. Call it with e.mu held, after
// the threads are loaded.
func (e *Engine) loadRecords() error {
	rs, err := e.st.Changes()
	if err != nil {
		return err
	}
	for _, c := range rs {
		e.records[c.ID] = c
	}
	before := maps.Clone(e.records)
	now := e.Now()
	var dirty []*core.Thread
	for _, t := range e.threads {
		if core.MigrateObserved(t, e.records) {
			dirty = append(dirty, t)
		}
	}
	for _, t := range e.threads {
		if dropFixture(t) && !slices.Contains(dirty, t) {
			dirty = append(dirty, t)
		}
	}
	var gone []string
	for id, c := range e.records {
		if core.RecordExpired(c, now) || e.loopRecord(c) {
			gone = append(gone, id)
			delete(e.records, id)
		}
	}
	for _, t := range e.threads {
		n := len(t.Refs)
		t.Refs = slices.DeleteFunc(t.Refs, func(r core.Ref) bool { return e.records[r.ID] == nil })
		if len(t.Refs) != n && !slices.Contains(dirty, t) {
			dirty = append(dirty, t)
		}
	}
	if len(dirty) == 0 && len(gone) == 0 && len(before) == len(e.records) {
		return nil
	}
	_, err = e.st.Write(func(tx *store.Tx) error {
		for id, c := range e.records {
			if before[id] != c {
				if err := tx.PutChange(c); err != nil {
					return err
				}
			}
		}
		for _, id := range gone {
			if err := tx.DeleteChange(id); err != nil {
				return err
			}
		}
		for _, t := range dirty {
			if err := tx.PutThread(t); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

// leakedFixture is the PR of a test fixture that reached live threads
// (V0.95 (5)).
const leakedFixture = "https://github.com/acme/app/pull/88"

// dropFixture removes the change of the leaked fixture PR from t, by that
// exact URL (V0.95 (5)). It reports whether t changed.
func dropFixture(t *core.Thread) bool {
	n := len(t.Changes)
	t.Changes = slices.DeleteFunc(t.Changes, func(c core.Change) bool {
		return (c.PR != nil && c.PR.URL == leakedFixture) || slices.ContainsFunc(c.Evidence, func(ev core.Evidence) bool { return ev.URL == leakedFixture })
	})
	if len(t.Changes) == n {
		return false
	}
	if t.MainChange != "" && core.FindChange(t, t.MainChange) == nil {
		t.MainChange = ""
	}
	return true
}

// loopRecord reports a record the loop of 16 Nov left (V0.95 (5)): a key
// git:<repo>#<branch>~N, not in flight, and no thread change follows it.
// Call it with e.mu held.
func (e *Engine) loopRecord(c *core.Change) bool {
	i := strings.LastIndex(c.Key, "~")
	if !strings.HasPrefix(c.Key, "git:") || i < 0 || c.Phase.InFlight() {
		return false
	}
	if _, err := strconv.Atoi(c.Key[i+1:]); err != nil {
		return false
	}
	for _, t := range e.threads {
		if slices.ContainsFunc(t.Changes, func(x core.Change) bool { return x.Observed == c.ID }) {
			return false
		}
	}
	return true
}

// applyRecord is the path of a host record's Input (#35): the record's
// reduce, then a change.observed Input to every thread that shows it, all
// stored in one transaction.
func (e *Engine) applyRecord(in core.Input) ([]*applied, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := e.Now()
	rs := slices.Collect(maps.Values(e.records))
	sort.Slice(rs, func(i, j int) bool { return rs[i].CreatedAt.Before(rs[j].CreatedAt) })
	before, after, evs, err := core.ReduceRecord(rs, in, now)
	if err != nil || after == nil {
		return nil, err
	}
	moved := before == nil || !sameJSON(before, after)
	var ss []*staged
	for _, id := range e.showing(after, in.Thread) {
		s, err := e.stage(e.threads[id], core.Input{Source: in.Source, Kind: core.KindChangeObserved, Thread: id, Args: core.ChangeObservedArgs{Before: before, After: *after}}, now, false)
		if err != nil {
			log.Printf("thread %s: %s: %v", id, core.KindChangeObserved, err)
			continue
		}
		if moved || !s.idle() {
			ss = append(ss, s)
		}
	}
	if !moved && len(ss) == 0 {
		return nil, nil
	}
	if _, err := e.st.Write(func(tx *store.Tx) error {
		if moved {
			if err := tx.PutChange(after); err != nil {
				return err
			}
			for _, ev := range evs {
				if err := tx.Append(after.ID, ev.Type, ev.Data, now); err != nil {
					return err
				}
			}
		}
		for _, s := range ss {
			if !s.idle() {
				if err := s.persist(tx); err != nil {
					return err
				}
			}
		}
		return nil
	}); err != nil {
		log.Printf("change %s: %s: %v", after.ID, in.Kind, err)
		return nil, err
	}
	e.records[after.ID] = after
	out := make([]*applied, len(ss))
	for i, s := range ss {
		e.commit(s, moved)
		out[i] = s.applied
	}
	return out, nil
}

// showing are the threads that show a record: those that reference it,
// and, while it is in flight, the thread that observed it and every thread
// not archived in its folder. Call it with e.mu held.
func (e *Engine) showing(rec *core.Change, observer string) []string {
	root := ""
	if rec.Workspace != nil {
		root = rec.Workspace.Path
	}
	var ids []string
	for id, t := range e.threads {
		ref := slices.ContainsFunc(t.Refs, func(r core.Ref) bool { return r.ID == rec.ID })
		here := rec.Phase.InFlight() && t.ArchivedAt == nil && (id == observer || inFolder(t.Cwd, root))
		if ref || here {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// inFolder reports dir at or under root.
func inFolder(dir, root string) bool {
	return root != "" && dir != "" && (dir == root || strings.HasPrefix(dir, strings.TrimSuffix(root, "/")+"/"))
}

// shown are the changes a thread shows (core.ShownChanges). Call it with
// e.mu held.
func (e *Engine) shown(t *core.Thread) []core.Change {
	return core.ShownChanges(t, func(id string) (*core.Change, []string) {
		c := e.records[id]
		if c == nil {
			return nil, nil
		}
		var ids []string
		for tid, x := range e.threads {
			if x.ArchivedAt == nil && slices.ContainsFunc(x.Refs, func(r core.Ref) bool { return r.ID == id }) {
				ids = append(ids, tid)
			}
		}
		sort.Strings(ids)
		return c, ids
	})
}

// Shown returns a copy of a thread with the changes it shows: its own and
// the host records it references (change_list, fyr_state).
func (e *Engine) Shown(id string) (core.Thread, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	t := e.threads[id]
	if t == nil {
		return core.Thread{}, false
	}
	c := core.CloneThread(t)
	c.Changes = e.shown(t)
	return *c, true
}

// observeGit is the git observer after a run (EffectGit, #35). It checks
// whether the branches of the in-flight records the thread references
// merged or went, then lands the facts of the thread's folder (read by the
// agent manager) and of each workspace of its in-flight changes. A folder
// that is not a checkout yields nothing.
func (e *Engine) observeGit(threadID string, folder *core.GitFacts) {
	type check struct{ id, path, branch, base, head string }
	e.mu.Lock()
	t := e.threads[threadID]
	if t == nil {
		e.mu.Unlock()
		return
	}
	cwd := t.Cwd
	var paths []string
	for _, c := range t.Changes {
		if c.Workspace != nil && c.Workspace.Path != "" && c.Phase.InFlight() {
			paths = append(paths, c.Workspace.Path)
		}
	}
	var checks []check
	merged := map[string]bool{}
	for _, ref := range t.Refs {
		rec := e.records[ref.ID]
		if ref.Ended || rec == nil || !rec.Phase.InFlight() || rec.Workspace == nil || rec.Workspace.Path == "" {
			continue
		}
		k := check{id: rec.ID, path: rec.Workspace.Path, branch: rec.Branch, base: rec.Workspace.Base}
		k.head = core.RecordHead(rec)
		checks = append(checks, k)
		merged[rec.ID] = e.prMerged(rec)
	}
	e.mu.Unlock()
	git, src := e.git(), core.Observer(core.ObsGit)
	land := func(kind string, args any) {
		if _, err := e.Apply(core.Input{Source: src, Kind: kind, Thread: threadID, Args: args}); err != nil {
			log.Printf("thread %s: %s: %v", threadID, kind, err)
		}
	}
	for _, k := range checks {
		gone, isMerged, known := git.End(k.path, k.branch, k.base, k.head)
		if !known {
			continue
		}
		if m := isMerged || merged[k.id]; m || gone {
			land(core.KindGitEnded, core.GitEnd{Change: k.id, Merged: m})
		}
	}
	seen := map[string]bool{cwd: true}
	if folder != nil && folder.Branch != "" {
		f := *folder
		if f.Path == "" {
			f.Path = cwd
		}
		seen[f.Path] = true
		land(core.KindGitObserved, f)
	}
	for _, p := range paths {
		if seen[p] {
			continue
		}
		seen[p] = true
		g := git.Facts(p)
		if g.Path == "" {
			g.Path = p
		}
		land(core.KindGitObserved, g)
	}
}

// prMerged reports a merged PR of a thread's change that follows rec,
// or is on its branch in its repo: a squash merge leaves the branch's head
// outside the base. Call it with e.mu held.
func (e *Engine) prMerged(rec *core.Change) bool {
	repo := core.RecordRepo(rec)
	for _, t := range e.threads {
		for _, c := range t.Changes {
			if !core.MergedPRCovers(c.PR, rec) {
				continue
			}
			if c.Observed == rec.ID || (c.Branch != "" && c.Branch == rec.Branch && repo == "github.com/"+c.PR.Repo) {
				return true
			}
		}
	}
	return false
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
