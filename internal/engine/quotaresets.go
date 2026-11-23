package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/stream"
)

// resolveQuotaReset resolves a provider's quota window reset time without
// holding e.mu (V0.quota-reset-wake, V0.quota-auto-continue): first via
// e.QuotaReset (quota.Service.ResetOf), and when that is unavailable or
// returns an error and no specific window was requested, falling back to the
// thread's own fresh future core.QuotaReset(t.Failure) on the same provider.
func (e *Engine) resolveQuotaReset(ctx context.Context, threadID, provider, window string) (string, string, time.Time, error) {
	t, hasThread := e.Thread(threadID)
	prov := strings.TrimSpace(provider)
	if prov == "" && hasThread {
		prov = t.Profile
	}
	if prov == "" {
		return "", "", time.Time{}, fmt.Errorf("%w: provider is required", core.ErrBadArgs)
	}
	win := strings.TrimSpace(window)
	var qerr error
	if e.QuotaReset != nil {
		rw, at, err := e.QuotaReset(ctx, prov, win)
		if err == nil {
			return prov, rw, at, nil
		}
		qerr = err
	}
	if win == "" && hasThread && t.Profile == prov && core.QuotaStopped(&t) {
		if at := core.QuotaReset(t.Failure); at != nil && at.After(e.Now()) {
			return prov, "", at.UTC(), nil
		}
	}
	if qerr != nil {
		return "", "", time.Time{}, qerr
	}
	return "", "", time.Time{}, errors.New("quota wakes are not available on this host")
}

type quotaResetFrame struct {
	ThreadID string    `json:"threadId"`
	Provider string    `json:"provider"`
	Window   string    `json:"window,omitempty"`
	At       time.Time `json:"at"`
}

// SyncQuotaResets updates armed WhenQuota rules when a fresh provider reading
// reports a moved ResetsAt, and remembers exhausted windows for Tick's reset
// notices (V0.quota-reset-wake). Call it without e.mu.
func (e *Engine) SyncQuotaResets(statuses []quota.Status) {
	if len(statuses) == 0 {
		return
	}
	var updates []core.QuotaUpdate
	fresh := map[string]time.Time{}
	for _, st := range statuses {
		if st.Stale {
			continue
		}
		for _, w := range st.Windows {
			if w.ResetsAt == nil {
				continue
			}
			at := w.ResetsAt.UTC()
			fresh[st.Profile+"\x00"+w.Name] = at
			updates = append(updates, core.QuotaUpdate{Provider: st.Profile, Window: w.Name, ResetsAt: at})
		}
	}
	e.mu.Lock()
	now := e.Now()
	e.recordExhaustedQuotaLocked(statuses, now)
	var affected []string
	if len(fresh) > 0 {
		for id, t := range e.threads {
			if hasQuotaRuleToSync(t.Rules, fresh) {
				affected = append(affected, id)
			}
		}
	}
	e.mu.Unlock()
	for _, id := range affected {
		if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsQuota), Kind: core.KindQuotaSync, Thread: id, Args: core.QuotaSyncArgs{Updates: updates}}); err != nil {
			log.Printf("thread %s: quota.sync: %v", id, err)
		}
	}
}

func hasQuotaRuleToSync(rs []core.Rule, fresh map[string]time.Time) bool {
	for _, r := range rs {
		if r.Status == core.RuleArmed && whenHasQuotaToSync(r.When, fresh) {
			return true
		}
	}
	return false
}

func whenHasQuotaToSync(w core.When, fresh map[string]time.Time) bool {
	if w.Kind == core.WhenQuota && w.At != nil {
		if at, ok := fresh[w.Provider+"\x00"+w.Window]; ok && !at.Equal(*w.At) {
			return true
		}
	}
	for _, x := range slices.Concat(w.All, w.Any) {
		if whenHasQuotaToSync(x, fresh) {
			return true
		}
	}
	return w.Not != nil && whenHasQuotaToSync(*w.Not, fresh)
}

const (
	quotaResetsKey = "engine.quotaResets"
	// quotaResetKeep bounds persisted exhausted-window reset times and
	// already-sent deduplication entries after their reset passes.
	quotaResetKeep = 24 * time.Hour
)

type savedQuotaResets struct {
	Windows map[string]time.Time `json:"windows,omitempty"`
	Sent    map[string]time.Time `json:"sent,omitempty"`
}

func (e *Engine) loadQuotaResetsLocked(now time.Time) {
	if e.st == nil {
		return
	}
	raw, _ := e.st.GetKV(quotaResetsKey)
	if raw == "" {
		return
	}
	var s savedQuotaResets
	if json.Unmarshal([]byte(raw), &s) != nil {
		return
	}
	cut := now.Add(-quotaResetKeep)
	for k, at := range s.Windows {
		if at.After(cut) {
			e.quotaWindows[k] = at
		}
	}
	for k, at := range s.Sent {
		if at.After(cut) {
			e.quotaResetSent[k] = at
		}
	}
}

func (e *Engine) saveQuotaResetsLocked(now time.Time) {
	if e.st == nil {
		return
	}
	cut := now.Add(-quotaResetKeep)
	s := savedQuotaResets{Windows: map[string]time.Time{}, Sent: map[string]time.Time{}}
	for k, at := range e.quotaWindows {
		if at.After(cut) {
			s.Windows[k] = at
		} else {
			delete(e.quotaWindows, k)
		}
	}
	for k, at := range e.quotaResetSent {
		if at.After(cut) {
			s.Sent[k] = at
		} else {
			delete(e.quotaResetSent, k)
		}
	}
	if len(s.Windows) == 0 && len(s.Sent) == 0 {
		e.st.SetKV(quotaResetsKey, "")
		return
	}
	if b, err := json.Marshal(s); err == nil {
		e.st.SetKV(quotaResetsKey, string(b))
	}
}

func (e *Engine) recordExhaustedQuotaLocked(statuses []quota.Status, now time.Time) {
	changed := false
	for _, st := range statuses {
		if st.Stale {
			continue
		}
		for _, w := range st.Windows {
			if w.UsedPct >= 100 && w.ResetsAt != nil && w.ResetsAt.After(now) {
				k := st.Profile + "\x00" + w.Name
				at := w.ResetsAt.UTC()
				if !e.quotaWindows[k].Equal(at) {
					e.quotaWindows[k] = at
					changed = true
				}
			}
		}
	}
	if changed {
		e.saveQuotaResetsLocked(now)
	}
}

func (e *Engine) dueQuotaResetsLocked(now time.Time) []quotaResetFrame {
	var out []quotaResetFrame
	changed := false
	for k, at := range e.quotaWindows {
		if at.After(now) {
			continue
		}
		delete(e.quotaWindows, k)
		changed = true
		prov, win, _ := strings.Cut(k, "\x00")
		for id, t := range e.threads {
			if t.Profile != prov || t.Failure == nil || t.Running || t.Settle.At != nil || t.ArchivedAt != nil {
				continue
			}
			sk := id + "\x00" + prov + "\x00" + win
			if e.quotaResetSent[sk].Equal(at) {
				continue
			}
			e.quotaResetSent[sk] = at
			out = append(out, quotaResetFrame{ThreadID: id, Provider: prov, Window: win, At: now})
		}
	}
	if changed {
		e.saveQuotaResetsLocked(now)
	}
	return out
}

func (e *Engine) publishQuotaResets(p *applied) {
	var frames []quotaResetFrame
	for _, ev := range p.res.Events {
		d, _ := ev.Data.(map[string]any)
		if ev.Type != "rule.fired" || d["purpose"] != core.PurposeWatch || d["outcome"] != core.OutcomeOK {
			continue
		}
		prov, _ := d["provider"].(string)
		if prov == "" {
			continue
		}
		win, _ := d["window"].(string)
		at, _ := d["at"].(time.Time)
		now := e.Now()
		e.mu.Lock()
		sk := p.threadID + "\x00" + prov + "\x00" + win
		if !at.IsZero() && e.quotaResetSent[sk].Equal(at) {
			e.mu.Unlock()
			continue
		}
		if !at.IsZero() {
			e.quotaResetSent[sk] = at
			e.saveQuotaResetsLocked(now)
		}
		e.mu.Unlock()
		frames = append(frames, quotaResetFrame{ThreadID: p.threadID, Provider: prov, Window: win, At: now})
	}
	for _, fr := range frames {
		e.Hub.Send(stream.FrameQuotaReset, fr)
	}
}
