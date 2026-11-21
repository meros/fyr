package core

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// V0.quota-wait: a run the provider's quota stopped is a wait, not a
// failure the owner fixes and not a finished turn. The thread is Waiting in
// Working until the reset the notice names, and then it is the owner's again
// (Retry, a message, or the wake thread.wakeOnQuota arms, V0.quota-reset-wake).
//
// The two CLIs say it differently: claude ends the run with an error whose
// text is the notice ("You've hit your weekly limit · resets …"), agy ends it
// cleanly and writes the notice as the turn's last words ("⚠ Individual quota
// reached … Resets in 35m47s"). Both are read here, so one thread reads the
// same on both providers.

// ClassQuota is the failure class of a run the provider's quota stopped. It
// is the one class that is a wait rather than the owner's move (attention.go,
// snoozedone.go).
const ClassQuota = "quota"

// V0.quota-auto-continue: automatic continuation parameters and loop bounds.
const (
	// QuotaResumeBuffer waits briefly past the notice's exact reset second so
	// the provider's quota clock has rolled over before Fyr resumes the thread.
	QuotaResumeBuffer = 15 * time.Second
	// MaxQuotaAutoResumes bounds consecutive automatic continues without a
	// non-quota turn completion or an owner move.
	MaxQuotaAutoResumes = 3
	// QuotaQuickFail is how soon after an automatic continue another quota
	// stop counts as an immediate rejection and incurs backoff.
	QuotaQuickFail = 2 * time.Minute
)

// quotaQuickBackoff is the minimum wait before the next automatic continue
// when the provider immediately rejected the previous one with quota again.
var quotaQuickBackoff = []time.Duration{time.Minute, 5 * time.Minute}

// quotaWords are the openings of the providers' own out-of-quota notices.
var quotaWords = []string{
	"individual quota reached", // agy, on a Gemini account
	"you've hit your",          // claude: "You've hit your weekly limit · resets 23 Nov, 4am"
	"you have hit your",
	"usage limit reached",
	"quota exceeded",
	"out of quota",
}

// QuotaNotice reports the provider's out-of-quota notice in text, as its
// line, or "" when text is not one. Only the first line that has words is
// read, and it must open with the notice: an agent's report that mentions
// the words (a log it read, a file it wrote) is not a notice.
func QuotaNotice(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		l := strings.ToLower(strings.NewReplacer("’", "'", "⚠️", "", "⚠", "").Replace(line))
		l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "warning:"))
		for _, w := range quotaWords {
			if strings.HasPrefix(l, w) {
				return TruncHead(line, 300)
			}
		}
		return ""
	}
	return ""
}

// QuotaStopped reports a thread whose last run the provider's quota stopped.
func QuotaStopped(t *Thread) bool {
	return t.Failure != nil && t.Failure.Class == ClassQuota
}

// QuotaResumeAt is when Fyr should automatically continue a quota-stopped
// thread (QuotaReset + QuotaResumeBuffer, with backoff on immediate repeated
// quota rejections), or nil when auto-continue does not apply (not quota-
// stopped, no reset time in the notice, or loop protection bound reached).
func QuotaResumeAt(t *Thread) *time.Time {
	if !QuotaStopped(t) || t.QuotaResumes >= MaxQuotaAutoResumes {
		return nil
	}
	reset := QuotaReset(t.Failure)
	if reset == nil {
		return nil
	}
	at := reset.Add(QuotaResumeBuffer)
	if t.QuotaResumes > 0 && t.QuotaResumedAt != nil && !t.Failure.At.IsZero() &&
		t.Failure.At.Sub(*t.QuotaResumedAt) < QuotaQuickFail {
		backoff := quotaQuickBackoff[min(t.QuotaResumes-1, len(quotaQuickBackoff)-1)]
		if floor := t.Failure.At.Add(backoff); floor.After(at) {
			at = floor
		}
	}
	return &at
}

// QuotaCanAutoContinue reports whether t is eligible to auto-continue at
// QuotaResumeAt (V0.quota-auto-continue): quota-stopped with a resume time,
// awake, idle, and not blocked on a decision, handover, or unread message.
func QuotaCanAutoContinue(t *Thread) bool {
	return !t.Asleep() && !t.Running && t.Starting == nil && t.Handover == nil &&
		len(t.Decisions) == 0 && len(t.Outbox) == 0 && QuotaResumeAt(t) != nil
}

// QuotaWaitText is the Waiting reason of a thread whose quota is out, or "":
// "Waiting for quota · resets 18:41", the notice's own time (QuotaReset) in
// the host's local time, as V0.143's. While auto-continue is armed, the thread
// stays Waiting across the reset buffer until KindClock resumes it
// (V0.quota-auto-continue); once loop protection stops auto-continuing, it
// returns "" so the thread surfaces as Failed in Needs you.
func QuotaWaitText(t *Thread, now time.Time) string {
	if !QuotaStopped(t) || t.QuotaResumes >= MaxQuotaAutoResumes {
		return ""
	}
	if reset := QuotaReset(t.Failure); reset != nil {
		if resume := QuotaResumeAt(t); resume != nil && QuotaCanAutoContinue(t) {
			if !now.Before(*resume) {
				return "Waiting for quota · resuming"
			}
			if resume.After(reset.Add(QuotaResumeBuffer)) {
				return "Waiting for quota · retries " + wakeClock(*resume, now)
			}
			return "Waiting for quota · resets " + wakeClock(*reset, now)
		}
		if !reset.After(now) {
			return ""
		}
		return "Waiting for quota · resets " + wakeClock(*reset, now)
	}
	return "Waiting for quota"
}

// FailureReason formats t.Failure for owner-facing views (Attention and
// fyr_state). For a ClassQuota failure that surfaces as Failed, it avoids
// leaking a stale relative "Resets in …" countdown (V0.quota-auto-continue).
func FailureReason(t *Thread, now time.Time) string {
	if t.Failure == nil {
		return ""
	}
	if t.Failure.Class != ClassQuota {
		return t.Failure.Message
	}
	reset := QuotaReset(t.Failure)
	if t.QuotaResumes >= MaxQuotaAutoResumes && t.QuotaResumedAt != nil {
		if reset != nil && reset.After(now) {
			return fmt.Sprintf("Quota still exhausted after %d automatic retries · resets %s", MaxQuotaAutoResumes, wakeClock(*reset, now))
		}
		return fmt.Sprintf("Quota still exhausted after %d automatic retries", MaxQuotaAutoResumes)
	}
	if reset != nil {
		if !reset.After(now) {
			return "Quota reset passed (" + wakeClock(*reset, now) + ") — ready to retry"
		}
		return "Quota limit reached · resets " + wakeClock(*reset, now)
	}
	return t.Failure.Message
}

// quotaContinueAfter runs inside Reduce on KindClock: when a quota-stopped
// thread reaches QuotaResumeAt, it clears the quota failure, marks the thread
// Starting, records the auto-resume attempt, and emits quota.continued so the
// agent feed wakes the agent (V0.quota-auto-continue).
func quotaContinueAfter(t *Thread, in Input, now time.Time) []Event {
	if in.Kind != KindClock || !QuotaCanAutoContinue(t) {
		return nil
	}
	resume := QuotaResumeAt(t)
	if resume == nil || now.Before(*resume) {
		return nil
	}
	prev := *t.Failure
	reset := QuotaReset(&prev)
	t.Failure = nil
	t.QuotaResumes++
	at := now
	t.QuotaResumedAt = &at
	t.Starting = &at
	t.Stopped = false
	data := map[string]any{
		"attempt": t.QuotaResumes,
		"message": prev.Message,
	}
	if reset != nil {
		data["resetAt"] = reset.UTC()
	}
	return []Event{{Type: "quota.continued", Data: data}}
}

// QuotaReset is when the provider's quota comes back, as the notice says, or
// nil: its "Resets in 1h56m30s" from the failure's own time, or its "resets
// 23 Nov, 4am" in the zone the host runs in (the zone the CLI wrote it in).
// Fyr never invents a time; a notice without one leaves the thread Waiting
// until the owner moves it.
func QuotaReset(f *Failure) *time.Time {
	if f == nil {
		return nil
	}
	if d, ok := resetsIn(f.Message); ok {
		at := f.At.Add(d)
		return &at
	}
	if at, ok := resetsAt(f.Message, f.At); ok {
		return &at
	}
	return nil
}

// resetsIn reads "resets in 35m47s" (or "resets in 1d2h30m") as a duration.
func resetsIn(msg string) (time.Duration, bool) {
	i := strings.Index(strings.ToLower(msg), "resets in ")
	if i < 0 {
		return 0, false
	}
	var b strings.Builder
	for _, r := range msg[i+len("resets in "):] {
		if r == ' ' || r == '\t' {
			continue // "1h 56m 30s" is one duration
		}
		if !(r >= '0' && r <= '9') && r != 'd' && r != 'h' && r != 'm' && r != 's' && r != '.' {
			break
		}
		b.WriteRune(r)
	}
	s := strings.TrimRight(b.String(), ".")
	var total time.Duration
	if before, after, ok := strings.Cut(s, "d"); ok {
		days, err := strconv.Atoi(before)
		if err != nil || days < 0 {
			return 0, false
		}
		total = time.Duration(days) * 24 * time.Hour
		s = after
	}
	if s != "" {
		d, err := time.ParseDuration(s)
		if err != nil || d < 0 {
			return 0, false
		}
		total += d
	}
	if total <= 0 {
		return 0, false
	}
	return total, true
}

// resetsAt reads "resets 23 Nov, 4am" (a zone in parentheses after it is the
// host's own, so it is dropped) as a time after at.
func resetsAt(msg string, at time.Time) (time.Time, bool) {
	i := strings.Index(strings.ToLower(msg), "resets ")
	if i < 0 {
		return time.Time{}, false
	}
	s := msg[i+len("resets "):]
	if j := strings.IndexByte(s, '('); j >= 0 {
		s = s[:j]
	}
	s = strings.TrimSpace(strings.TrimRight(s, ".,; "))
	// A time alone is on the day the notice came; a date is this year's, and
	// next year's when that day is behind us. claude writes "23 Nov, 4am";
	// the month-first forms are here for other providers.
	for _, layout := range []string{"2 Jan, 3pm", "2 Jan, 3:04pm", "2 Jan 15:04", "2 Jan 3pm", "Jan 2, 3pm", "Jan 2, 3:04pm", "Jan 2, 15:04", "Jan 2 15:04"} {
		t, err := time.ParseInLocation(layout, s, at.Location())
		if err != nil {
			continue
		}
		t = time.Date(at.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, at.Location())
		if !t.After(at) {
			t = t.AddDate(1, 0, 0)
		}
		return t, true
	}
	for _, layout := range []string{"3pm", "3:04pm", "15:04"} {
		t, err := time.ParseInLocation(layout, s, at.Location())
		if err != nil {
			continue
		}
		t = time.Date(at.Year(), at.Month(), at.Day(), t.Hour(), t.Minute(), 0, 0, at.Location())
		if !t.After(at) {
			t = t.AddDate(0, 0, 1)
		}
		return t, true
	}
	return time.Time{}, false
}
