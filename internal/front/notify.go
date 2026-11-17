package front

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/notify"
	"github.com/meros/fyr/internal/push"
)

// Notifications (SPEC 0.5 V0.54, decision 0016): the front holds the device
// data (push subscriptions, presence) and sends the push for a thread on any
// of its hosts. It sees each host's threads on the host's stream and applies
// the same plan a host used to (V0.20): asking and failed push at once, done
// and blocked after notify.Delay if still true; never for a thread a device
// of this front is watching or in its project's quiet hours (V0.46). Hosts
// send no push.

// attentionOf is a thread DTO's attention state, "" when it has none.
func attentionOf(m map[string]any) string {
	if m == nil {
		return ""
	}
	a, _ := m["attention"].(map[string]any)
	s, _ := a["state"].(string)
	return s
}

// noticeLocked plans a push for a change of one thread's attention from prev
// to next (both DTOs as the host sent them). prev nil is a thread the front
// has not seen before: no push for old states, as on a host (V0.20).
// Called with f.mu held; the send runs outside it.
func (f *Front) noticeLocked(l *link, prev, next map[string]any) {
	if f.opt.Push == nil || prev == nil {
		return
	}
	from, to := attentionOf(prev), attentionOf(next)
	id, _ := next["id"].(string)
	switch notify.PlanFor(from, to) {
	case notify.Now:
		go f.send(l.name(), next)
	case notify.Later:
		time.AfterFunc(f.opt.NotifyDelay, func() {
			f.mu.Lock()
			cur := l.raw[id]
			f.mu.Unlock()
			if cur != nil && attentionOf(cur) == to {
				f.send(l.name(), cur)
			}
		})
	}
}

// send pushes one thread's state to this front's devices.
func (f *Front) send(host string, m map[string]any) {
	d, err := dtoOf(m)
	if err != nil {
		f.opt.Logf("front: push: %v", err)
		return
	}
	now := time.Now()
	if !notify.Allowed(f.presence.Watched(d.ID, now), d.QuietUntil, now) {
		return
	}
	meta, detail := AttentionDetail(d)
	// V0.141: with several hosts a push says which one the thread runs on.
	if host != "" && len(f.all()) > 1 {
		if meta == "" {
			meta = host
		} else {
			meta = host + " · " + meta
		}
	}
	f.opt.Push.Notify(core.Owner, push.PayloadFor(d.ID, d.Title, string(d.Attention.State), meta, detail))
}

func dtoOf(m map[string]any) (api.Thread, error) {
	var d api.Thread
	b, err := json.Marshal(m)
	if err != nil {
		return d, err
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return d, err
	}
	if d.Thread == nil {
		return d, fmt.Errorf("thread without fields")
	}
	return d, nil
}

// AttentionDetail is the meta word and the text of a push, matching the
// inbox row (design/mobile rule 12, SPEC 0.5 V0.20).
func AttentionDetail(d api.Thread) (meta, detail string) {
	switch d.Attention.State {
	case core.Failed:
		if d.Failure != nil {
			return "Failed", d.Failure.Message
		}
	case core.Done:
		return "Done", d.Summary
	case core.Blocked:
		return "Blocked", d.Attention.Reason
	}
	if len(d.Decisions) > 0 {
		x := d.Decisions[0]
		switch {
		case x.Kind == core.DecisionAsk && len(x.Questions) > 0:
			meta = "1 question"
			if n := len(x.Questions); n > 1 {
				meta = fmt.Sprintf("%d questions", n)
			}
			return meta, x.Questions[0].Question
		case x.Kind == core.DecisionEffect && x.Effect != nil:
			return "Fyr action", "approve " + x.Effect.Action
		case x.Kind == core.DecisionApproval:
			return "Approval", "allow " + x.Tool
		}
	}
	return "", d.Attention.Reason
}
