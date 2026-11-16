package engine

// Done changes on the host (SPEC V0.98, decision 0024 slice 4): the owner's
// Done on a host record, the board's visit time, and the load pass that
// marks what landed before.

import (
	"strings"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/store"
)

// boardSeenKey holds the owner's boardSeenAt (V0.98 (3)).
const boardSeenKey = "owner.boardSeenAt"

// BoardVisit is a visit of the board: it stores now as the owner's
// boardSeenAt and returns the one before (zero when there was none).
func (e *Engine) BoardVisit() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	raw, _ := e.st.GetKV(boardSeenKey)
	prev, _ := time.Parse(time.RFC3339Nano, raw)
	e.st.SetKV(boardSeenKey, e.Now().UTC().Format(time.RFC3339Nano))
	return prev
}

// recordKinds are the owner's commands on a change that a host record takes
// as its own kind: Done (V0.98 (4)) and Abandon (V0.101).
var recordKinds = map[string]string{core.KindChangeDone: core.KindRecordDone, core.KindChangeEnd: core.KindRecordEnd}

// recordOwner sends the owner's change.done or change.end on a host record
// that the thread shows down the record's path, with a receipt. ok is false
// when the change is not such a record: the thread's own reduce takes it.
func (e *Engine) recordOwner(in core.Input, a core.Args, kind string) (api.Reply, bool) {
	id := strings.TrimSpace(a.Str("change"))
	e.mu.Lock()
	t := e.threads[in.Thread]
	rec := e.records[id]
	mine := t != nil && core.FindChange(t, id) != nil
	e.mu.Unlock()
	if t == nil || rec == nil || mine {
		return api.Reply{}, false
	}
	if r, done := e.replay(in.CmdID); done {
		return r, true
	}
	if _, err := e.Apply(core.Input{Source: in.Source, Kind: kind, Thread: in.Thread, Args: core.Args{"change": id, "reason": a.Str("reason")}}); err != nil {
		return rejected(err), true
	}
	reply := api.Reply{OK: true, Status: 200, ThreadID: in.Thread}
	if in.CmdID != "" {
		e.st.Write(func(tx *store.Tx) error { return tx.PutReceipt(in.CmdID, reply, e.Now()) })
	}
	return reply, true
}

// migrateDone is the load pass of V0.98 (7): in each thread a PR change and
// its open branch change fold into one, and what landed before is done; a
// host record that landed or ended is done. Call it with e.mu held, after
// the records are loaded.
func (e *Engine) migrateDone() error {
	now := e.Now()
	var dirty []*core.Thread
	for _, t := range e.threads {
		if core.MigrateDone(t, now) {
			dirty = append(dirty, t)
		}
	}
	var recs []*core.Change
	for _, c := range e.records {
		if core.MigrateRecordDone(c, now) {
			recs = append(recs, c)
		}
	}
	if len(dirty) == 0 && len(recs) == 0 {
		return nil
	}
	_, err := e.st.Write(func(tx *store.Tx) error {
		for _, c := range recs {
			if err := tx.PutChange(c); err != nil {
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
