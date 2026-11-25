package core

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// V0.overload-retry: a run the provider's own trouble stopped (overloaded, a
// 5xx, a rate limit, a lost connection) is a wait, not a failure the owner
// fixes. Nothing the owner says moves it, and a Retry pressed at once fails the
// same way: Fyr retries by itself on a schedule, and the thread is Waiting in
// Working meanwhile, as a thread Waiting for quota is (V0.quota-wait). When
// the schedule is used up the next such failure is classed ClassAPI, and is
// the owner's as any failure.

// ClassOverloaded is the failure class of a run the provider's own trouble
// stopped. With ClassQuota it is a wait rather than the owner's move.
const ClassOverloaded = "overloaded"

// ClassAPI is a provider failure that is the owner's: one that does not pass
// by itself, or a passing one whose retries are used up.
const ClassAPI = "api"

// overloadBackoff is the wait before each automatic retry, counted from the
// failure. The CLI has already retried for a short while before it gives up,
// so the first wait is a minute, and the schedule covers about an hour and a
// half of provider trouble.
var overloadBackoff = []time.Duration{
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute,
	15 * time.Minute, 30 * time.Minute, 30 * time.Minute,
}

// MaxOverloadRetries bounds consecutive automatic retries without a run that
// got somewhere or an owner move.
var MaxOverloadRetries = len(overloadBackoff)

// OverloadProgress is how long a retried run must have lasted for its next
// provider failure to count as a new incident (the schedule starts over)
// rather than the same one still going on.
const OverloadProgress = 5 * time.Minute

// overloadStatus reads the HTTP status the CLI writes after "API Error".
var overloadStatus = regexp.MustCompile(`^api error:?\s*(?:repeated\s+)?\(?(\d{3})\b`)

// overloadWords say a provider failure that passes by itself when the notice
// names no status.
var overloadWords = []string{
	"overloaded", "rate limit", "rate_limit", "internal server error", "service unavailable",
	"bad gateway", "gateway timeout", "connection error", "timed out", "timeout",
	"econnreset", "fetch failed", "socket hang up",
}

// OverloadNotice reports the provider's passing failure in text, as its line,
// or "" when text is not one: "API Error: 529 Overloaded. …", "API Error: 500
// …", "API Error: Connection error.". As QuotaNotice, only the first line with
// words is read and it must open with the CLI's own "API Error", so an agent
// that writes about such an error is not the error. A 4xx other than 429 is a
// request the provider refuses every time and is not one.
func OverloadNotice(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		l := strings.ToLower(line)
		if !strings.HasPrefix(l, "api error") {
			return ""
		}
		if m := overloadStatus.FindStringSubmatch(l); m != nil {
			if m[1] == "429" || m[1][0] == '5' {
				return TruncHead(line, 300)
			}
			return ""
		}
		for _, w := range overloadWords {
			if strings.Contains(l, w) {
				return TruncHead(line, 300)
			}
		}
		return ""
	}
	return ""
}

// OverloadStopped reports a thread whose last run the provider's own trouble
// stopped.
func OverloadStopped(t *Thread) bool {
	return t.Failure != nil && t.Failure.Class == ClassOverloaded
}

// OverloadRetryAt is when Fyr retries a thread a provider failure stopped: the
// failure's time plus the next wait of the schedule, or nil when it does not
// apply (not such a failure, or the schedule is used up).
func OverloadRetryAt(t *Thread) *time.Time {
	if !OverloadStopped(t) || t.OverloadRetries >= MaxOverloadRetries {
		return nil
	}
	at := t.Failure.At.Add(overloadBackoff[max(t.OverloadRetries, 0)])
	return &at
}

// OverloadCanRetry reports whether t is retried at OverloadRetryAt: awake,
// idle, and not blocked on a decision or a handover (as QuotaCanAutoContinue).
func OverloadCanRetry(t *Thread) bool {
	return !t.Asleep() && !t.Running && t.Starting == nil && t.Handover == nil &&
		len(t.Decisions) == 0 && OverloadRetryAt(t) != nil
}

// OverloadWaitText is the Waiting reason of a thread a provider failure
// stopped, or "": "Waiting for the provider · retry 2 of 7 at 18:41", in the
// host's local time. It stays Waiting until KindClock retries it.
func OverloadWaitText(t *Thread, now time.Time) string {
	at := OverloadRetryAt(t)
	if at == nil {
		return ""
	}
	if !now.Before(*at) {
		if OverloadCanRetry(t) {
			return "Waiting for the provider · retrying"
		}
		return "" // something else holds the thread: the failure is the owner's
	}
	return fmt.Sprintf("Waiting for the provider · retry %d of %d at %s", t.OverloadRetries+1, MaxOverloadRetries, wakeClock(*at, now))
}

// ProviderWaitText is the Waiting reason of a thread whose last run the
// provider stopped and that goes on by itself: out of quota until the reset
// (V0.quota-wait), or a passing failure until the next retry
// (V0.overload-retry). "" when the failure, if any, is the owner's.
func ProviderWaitText(t *Thread, now time.Time) string {
	if w := QuotaWaitText(t, now); w != "" {
		return w
	}
	return OverloadWaitText(t, now)
}

// providerWaits reports a failure Fyr still moves by itself: its automatic
// continues or retries are not used up.
func providerWaits(t *Thread, f *Failure) bool {
	if f == nil {
		return false
	}
	switch f.Class {
	case ClassQuota:
		return t.QuotaResumes < MaxQuotaAutoResumes
	case ClassOverloaded:
		return true // a used-up schedule made it ClassAPI (RunEnded)
	}
	return false
}

// resetAutoResumes forgets the automatic continues and retries: a run got
// through, or the owner moved the thread.
func resetAutoResumes(t *Thread) {
	t.QuotaResumes, t.QuotaResumedAt = 0, nil
	t.OverloadRetries, t.OverloadRetriedAt = 0, nil
}

// overloadRetryAfter runs inside Reduce on KindClock: at OverloadRetryAt it
// clears the failure, marks the thread Starting, counts the retry and emits
// overload.retried so the agent feed wakes the agent (V0.overload-retry).
func overloadRetryAfter(t *Thread, in Input, now time.Time) []Event {
	if in.Kind != KindClock || !OverloadCanRetry(t) {
		return nil
	}
	if at := OverloadRetryAt(t); at == nil || now.Before(*at) {
		return nil
	}
	prev := *t.Failure
	t.Failure = nil
	t.OverloadRetries++
	at := now
	t.OverloadRetriedAt = &at
	t.Starting = &at
	t.Stopped = false
	return []Event{{Type: "overload.retried", Data: map[string]any{
		"attempt": t.OverloadRetries,
		"of":      MaxOverloadRetries,
		"message": prev.Message,
	}}}
}
