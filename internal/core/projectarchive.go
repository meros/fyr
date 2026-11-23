package core

import (
	"fmt"
	"sort"
	"time"
)

// Archive a finished project (V0.project-archive, decision 0017). The
// project record gets ArchivedAt; each of its threads is shelved in the same
// write (ShelvedAt), which the fronts read to leave it out of the inbox. A
// shelved thread is otherwise untouched: its state, its changes and its
// worktree stay. New work in it (a message, a run, a question) brings that
// thread back and leaves the project archived. Archiving is refused while a
// thread of the project works or asks, with the reason the shelve gives.
const (
	// KindProjectArchive archives the project: args {id}. Archiving an
	// archived project changes nothing ({kept: true}), so a linked project's
	// fronts can send it to every member.
	KindProjectArchive = "project.archive"
	// KindProjectUnarchive brings the project and its shelved threads back:
	// args {id}.
	KindProjectUnarchive = "project.unarchive"
	// KindThreadShelve puts one thread away with its archived project; the
	// engine sends it for every thread of the project (ArchiveFollows).
	KindThreadShelve = "thread.shelve"
	// KindThreadUnshelve brings a shelved thread back with its project.
	KindThreadUnshelve = "thread.unshelve"
)

func init() {
	for _, k := range []*InputKind{
		{Name: KindProjectArchive, On: RecProject, From: ownerOnly, Reduce: func(r Rec, in Input, now time.Time) (Result, error) {
			p := r.P
			if p.ArchivedAt != nil {
				return Result{Reply: map[string]any{"id": p.ID, "kept": true}}, nil
			}
			p.ArchivedAt = &now
			return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID}}, nil
		}},
		{Name: KindProjectUnarchive, On: RecProject, From: ownerOnly, Reduce: func(r Rec, in Input, _ time.Time) (Result, error) {
			p := r.P
			if p.ArchivedAt == nil {
				return Result{Reply: map[string]any{"id": p.ID, "kept": true}}, nil
			}
			p.ArchivedAt = nil
			return Result{Events: []Event{{"command", map[string]any{"action": in.Kind, "commandId": in.CmdID}}}, Reply: map[string]any{"id": p.ID}}, nil
		}},
		// Untouched: putting a thread away is not news; its place in a list
		// stays where it was when it comes back.
		{Name: KindThreadShelve, From: ownerOnly, Untouched: true, Available: ShelveBlock, Reduce: func(r Rec, _ Input, now time.Time) (Result, error) {
			r.T.ShelvedAt = &now
			return Result{Events: []Event{{"thread.shelved", map[string]any{"project": r.T.ProjectID}}}}, nil
		}},
		{Name: KindThreadUnshelve, From: ownerOnly, Untouched: true, Reduce: func(r Rec, _ Input, _ time.Time) (Result, error) {
			if r.T.ShelvedAt == nil {
				return Result{}, nil
			}
			r.T.ShelvedAt = nil
			return Result{Events: []Event{{"thread.unshelved", map[string]any{"by": "project"}}}}, nil
		}},
	} {
		RegisterKind(k)
	}
}

// ShelveBlock is why thread t cannot be put away with its project, or "": a
// live run (or a message on its way to the agent) and an open question keep
// it, and so the project, from being archived.
func ShelveBlock(t *Thread, _ *View, _ time.Time) string {
	name := t.Title
	if name == "" {
		name = t.ID
	}
	if working(t) {
		return fmt.Sprintf("“%s” is working: stop its run or wait for it first", name)
	}
	if len(t.Decisions) > 0 {
		return fmt.Sprintf("“%s” has an open question: answer it first", name)
	}
	return ""
}

// working: a run is live, starting, or a message waits for the agent.
func working(t *Thread) bool {
	return t.Running || t.Starting != nil || len(t.Outbox) > 0
}

// returnShelved brings a shelved thread back once it has new work: a run, a
// message to the agent, a question. Only that thread returns; its project
// stays archived (V0.project-archive).
func returnShelved(t *Thread) []Event {
	if t.ShelvedAt == nil || (!working(t) && len(t.Decisions) == 0) {
		return nil
	}
	t.ShelvedAt = nil
	return []Event{{"thread.unshelved", map[string]any{"by": "work"}}}
}

// ArchiveFollows are the thread Inputs a project's archive or unarchive
// writes with it, by thread id: on archive every thread of the project that
// is neither archived nor shelved is shelved; on unarchive every shelved one
// comes back. A rejection of any of them (ShelveBlock) rejects the archive.
// Any other change of the project gives none.
func ArchiveFollows(src Source, before, after *Project, threads map[string]*Thread) []Input {
	if before == nil || after == nil {
		return nil
	}
	var kind string
	switch {
	case before.ArchivedAt == nil && after.ArchivedAt != nil:
		kind = KindThreadShelve
	case before.ArchivedAt != nil && after.ArchivedAt == nil:
		kind = KindThreadUnshelve
	default:
		return nil
	}
	var ids []string
	for id, t := range threads {
		if t.ProjectID != after.ID {
			continue
		}
		if kind == KindThreadShelve && (t.ArchivedAt != nil || t.ShelvedAt != nil) {
			continue
		}
		if kind == KindThreadUnshelve && t.ShelvedAt == nil {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Input, 0, len(ids))
	for _, id := range ids {
		out = append(out, Input{Source: src, Kind: kind, Thread: id})
	}
	return out
}
