package front

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
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
//
// V0.notification-polish: every change of state takes a new number for the
// thread, and a waiting push goes out only while its number is the newest.
// So done, working, done inside the wait is one push, not two.
func (f *Front) noticeLocked(l *link, prev, next map[string]any) {
	if f.opt.Push == nil || prev == nil {
		return
	}
	from, to := attentionOf(prev), attentionOf(next)
	if from == to {
		return
	}
	id, _ := next["id"].(string)
	key := l.name() + "\x00" + id
	hostID := f.idOfLocked(l)
	if f.noticed == nil {
		f.noticed = map[string]uint64{}
	}
	f.noticed[key]++
	seq := f.noticed[key]
	switch notify.PlanFor(from, to) {
	case notify.Now:
		go f.send(l.name(), hostID, next)
	case notify.Later:
		time.AfterFunc(f.opt.NotifyDelay, func() {
			f.mu.Lock()
			cur := l.raw[id]
			newest := f.noticed[key] == seq
			f.mu.Unlock()
			if newest && cur != nil && attentionOf(cur) == to {
				f.send(l.name(), hostID, cur)
			}
		})
	}
}

// send pushes one thread's state to this front's devices. host is the
// host's name (shown, and what a device hides by); hostID its install id,
// which the tap opens the thread by (V0.client-refs).
func (f *Front) send(host, hostID string, m map[string]any) {
	d, err := dtoOf(m)
	if err != nil {
		f.opt.Logf("front: push: %v", err)
		return
	}
	if d.PushQuiet { // V0.200: an auto-merge at work is not news
		return
	}
	now := time.Now()
	if !notify.Allowed(f.presence.Watched(d.ID, now), d.QuietUntil, now) {
		return
	}
	meta, detail := AttentionDetail(d)
	// V0.140: with several hosts a push says which one the thread runs on.
	if host != "" && len(f.all()) > 1 {
		if meta == "" {
			meta = host
		} else {
			meta = host + " · " + meta
		}
	}
	pl := push.PayloadFor(d.ID, d.Title, string(d.Attention.State), meta, detail)
	pl.Host = hostID
	pl.Origin = &push.Origin{Host: host, ProjectID: d.ProjectID} // V0.207: a device may hide either
	f.opt.Push.Notify(core.Owner, pl)
}

// quotaResetLocked plans a push when a provider's quota window resets for a
// thread (V0.quota-reset-wake): after NotifyDelay, if the thread still waits
// (not working, settled or archived) and push is allowed, the front notifies
// every device that asked for quota reset pushes (push.Prefs.QuotaReset).
func (f *Front) quotaResetLocked(l *link, d map[string]any) {
	if f.opt.Push == nil {
		return
	}
	id, _ := d["threadId"].(string)
	if id == "" {
		return
	}
	window, _ := d["window"].(string)
	host := l.name()
	hostID := f.idOfLocked(l)
	time.AfterFunc(f.opt.NotifyDelay, func() {
		f.mu.Lock()
		cur := l.raw[id]
		f.mu.Unlock()
		if cur == nil {
			return
		}
		if st := attentionOf(cur); st == string(core.Working) || st == string(core.Settled) || st == string(core.Archived) {
			return
		}
		f.sendQuotaReset(host, hostID, cur, window)
	})
}

func (f *Front) sendQuotaReset(host, hostID string, m map[string]any, window string) {
	d, err := dtoOf(m)
	if err != nil {
		f.opt.Logf("front: push: %v", err)
		return
	}
	now := time.Now()
	if !notify.Allowed(f.presence.Watched(d.ID, now), d.QuietUntil, now) {
		return
	}
	meta := "Quota reset"
	if host != "" && len(f.all()) > 1 {
		meta = host + " · " + meta
	}
	detail := "Quota window reset; this thread still waits"
	if window != "" {
		detail = fmt.Sprintf("The %s window reset; this thread still waits", window)
	}
	pl := push.PayloadFor(d.ID, d.Title, "quota", meta, detail)
	pl.Host = hostID
	pl.Origin = &push.Origin{Host: host, ProjectID: d.ProjectID}
	f.opt.Push.Notify(core.Owner, pl)
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

// AttentionDetail is the meta word and the text of a push. They are the
// inbox row's bold word and line (web/src/rows.ts rowState and rowMeta,
// design/mobile rule 12, SPEC 0.5 V0.20), so the owner reads the same words
// on the lock screen and in the list (V0.notification-polish).
func AttentionDetail(d api.Thread) (meta, detail string) {
	switch d.Attention.State {
	case core.Failed:
		if d.Failure != nil {
			return "Failed", d.Failure.Message
		}
		return "Failed", d.Attention.Reason
	case core.Done:
		return "Done", d.Summary
	case core.Blocked:
		// Every cause is its own sentence ("Add login: Merge", "Run: …",
		// "Agent not responding since 08:58: …"), and the row shows it
		// without the word Blocked.
		return "", d.Attention.Reason
	}
	if x := firstAsk(d.Decisions); x != nil {
		switch {
		case x.Kind == core.DecisionAsk && len(x.Questions) > 0:
			meta = "1 question"
			if n := len(x.Questions); n > 1 {
				meta = fmt.Sprintf("%d questions", n)
			}
			return meta, x.Questions[0].Question
		case x.Kind == core.DecisionEffect && x.Effect != nil:
			return effectText(x.Effect)
		case x.Kind == core.DecisionApproval:
			tool := x.Tool
			if tool == "" {
				tool = "tool"
			}
			in, _ := x.Input.(map[string]any)
			if cmd, _ := in["command"].(string); cmd != "" {
				tool += " " + cmd
			}
			return "Approval", "Allow " + tool
		}
	}
	return "", d.Attention.Reason
}

// firstAsk is the decision a push is about: a question when there is one,
// since a thread with a question and a merge ask is in Answer
// (V0.your-move), else the oldest.
func firstAsk(ds []core.Decision) *core.Decision {
	for i := range ds {
		if ds[i].Kind == core.DecisionAsk {
			return &ds[i]
		}
	}
	if len(ds) > 0 {
		return &ds[0]
	}
	return nil
}

var prNumber = regexp.MustCompile(`/pull/(\d+)`)

// effectText is an effect ask in words, as web/src/effects.ts describeEffect
// has it: the short name, then the agent's reason, or what is asked when it
// gave none. Never the action's id ("pr.merge").
func effectText(e *core.EffectReq) (meta, detail string) {
	why := strings.TrimSpace(e.Reason)
	or := func(s string) string {
		if why != "" {
			return why
		}
		return s
	}
	switch e.Action {
	case "fyr.deploy":
		return "Update Fyr", or("Update Fyr to a new version")
	case "pr.merge":
		url, _ := e.Args["pr"].(string)
		if m := prNumber.FindStringSubmatch(url); m != nil {
			return "Merge #" + m[1], or("Merge pull request #" + m[1])
		}
		return "Merge the pull request", or("Waits for your approval")
	}
	w := strings.TrimSpace(strings.NewReplacer(".", " ", "_", " ", "-", " ").Replace(e.Action))
	if w == "" {
		return "An action", or("Waits for your approval")
	}
	return strings.ToUpper(w[:1]) + w[1:], or("Waits for your approval")
}
