package core

import (
	"testing"
	"time"
)

// V0.quota-wait: the providers' out-of-quota notices are read, and only the
// line that opens with one counts.
func TestQuotaNotice(t *testing.T) {
	for _, s := range []string{
		"⚠ Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 35m47s.",
		"Individual quota reached. Resets in 13m48s.",
		"You've hit your weekly limit · resets 23 Nov, 4am (Europe/Stockholm)",
		"\n\n  You have hit your 5-hour limit\n",
		"Quota exceeded: try again later",
	} {
		if QuotaNotice(s) == "" {
			t.Fatalf("not read: %q", s)
		}
	}
	// An agent that writes about the notice is not the notice.
	for _, s := range []string{
		"I fixed the retry so an Individual quota reached error is shown as a wait.",
		"The log said: Individual quota reached. Resets in 1h.",
		"",
		"Done. Everything green.",
	} {
		if got := QuotaNotice(s); got != "" {
			t.Fatalf("%q read as a notice: %q", s, got)
		}
	}
	// The line is the notice's own, cut as a failure's message is.
	if got := QuotaNotice("\n⚠ Individual quota reached. Resets in 35m47s.\n"); got != "⚠ Individual quota reached. Resets in 35m47s." {
		t.Fatalf("got %q", got)
	}
}

// V0.quota-wait: the reset time comes from the notice, never invented.
func TestQuotaReset(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		msg  string
		want time.Time
	}{
		{"Individual quota reached. Resets in 35m47s.", at.Add(35*time.Minute + 47*time.Second)},
		{"Individual quota reached. Resets in 1h56m30s.", at.Add(time.Hour + 56*time.Minute + 30*time.Second)},
		{"Individual quota reached. Resets in 1d2h30m.", at.Add(26*time.Hour + 30*time.Minute)},
		{"Individual quota reached. Resets in 2d.", at.Add(48 * time.Hour)},
		{"You've hit your weekly limit · resets Nov 23, 4am", time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC)},
		{"You've hit your weekly limit · resets 23 Nov, 4am (Europe/Stockholm)", time.Date(2026, 11, 23, 4, 0, 0, 0, time.UTC)},
		// A day of this month already past is next year's.
		{"resets Jan 2, 4am", time.Date(2027, 1, 2, 4, 0, 0, 0, time.UTC)},
		// A time alone is today's, or tomorrow's when it has gone by.
		{"resets 18:30", time.Date(2026, 11, 21, 18, 30, 0, 0, time.UTC)},
		{"resets 09:00", time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		got := QuotaReset(&Failure{Class: ClassQuota, Message: c.msg, At: at})
		if got == nil || !got.Equal(c.want) {
			t.Fatalf("%q: %v, want %v", c.msg, got, c.want)
		}
	}
	if got := QuotaReset(&Failure{Class: ClassQuota, Message: "Individual quota reached."}); got != nil {
		t.Fatalf("a notice without a time invented %v", got)
	}
}

// V0.quota-wait: a run the provider's quota stopped is Waiting in Working,
// not Failed in Needs you, until the reset the notice names; then the
// failure is the owner's again.
func TestQuotaStopIsWaiting(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	th, v := &Thread{ID: "t_1", ActivityAt: at, Conversation: Conversation{
		Failure: &Failure{Class: ClassQuota, Message: "Individual quota reached. Resets in 2h.", At: at},
	}}, &View{SeenAt: at}
	a := Compute(th, v, at.Add(time.Minute))
	if a.State != Waiting || a.Section != WorkingSec || a.Reason != "Waiting for quota · resets 14:00" {
		t.Fatalf("at the stop: %+v", a)
	}
	// Not the owner's move, so it hides nothing and is not work either.
	if NeedsOwnerAt(th, at.Add(time.Minute)) || !WorkGoingAt(th, at.Add(time.Minute)) {
		t.Fatalf("NeedsOwner=%v WorkGoing=%v", NeedsOwnerAt(th, at.Add(time.Minute)), WorkGoingAt(th, at.Add(time.Minute)))
	}
	// The clock re-reduces at the reset, and only then.
	if c := NextCheck(th, v, at.Add(time.Minute)); c == nil || !c.Equal(at.Add(2*time.Hour)) {
		t.Fatalf("next check %v", c)
	}
	if c := NextCheck(th, v, at.Add(3*time.Hour)); c != nil {
		t.Fatalf("next check past the reset: %v", c)
	}
	// Past the reset the failure is the owner's again, and it is not work.
	late := at.Add(3 * time.Hour)
	if a := Compute(th, v, late); a.State != Failed || a.Section != NeedsYou || a.Group != GroupUnblock {
		t.Fatalf("after the reset: %+v", a)
	}
	if !NeedsOwnerAt(th, late) || WorkGoingAt(th, late) {
		t.Fatalf("after the reset NeedsOwner=%v WorkGoing=%v", NeedsOwnerAt(th, late), WorkGoingAt(th, late))
	}
	// So "settle when done" waits for the reset and does not settle it then.
	if WorkDoneAt(th, at.Add(time.Minute)) || WorkDoneAt(th, late) {
		t.Fatal("a thread stopped by the quota is done")
	}
}

// A notice without a time waits with no clock to end it: the owner's move
// (Retry, a message, a quota wake) ends it, not the clock.
func TestQuotaStopWithoutATime(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	th, v := &Thread{ID: "t_1", ActivityAt: at, Conversation: Conversation{
		Failure: &Failure{Class: ClassQuota, Message: "Individual quota reached.", At: at},
	}}, &View{SeenAt: at}
	if a := Compute(th, v, at.Add(24*time.Hour)); a.State != Waiting || a.Reason != "Waiting for quota" {
		t.Fatalf("%+v", a)
	}
	if c := NextCheck(th, v, at.Add(time.Minute)); c != nil {
		t.Fatalf("a clock was armed with no time to arm it for: %v", c)
	}
}

// The owner's wake (V0.quota-reset-wake) ends a quota wait, as Retry does.
func TestOwnerMovesEndAQuotaWait(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	reset := at.Add(2 * time.Hour)
	th, v := &Thread{ID: "t_1", Profile: "claude", ActivityAt: at, Conversation: Conversation{
		Failure: &Failure{Class: ClassQuota, Message: "Individual quota reached. Resets in 2h.", At: at},
	}}, &View{SeenAt: at}
	step(t, th, v, Input{Source: FromOwner, Kind: KindWakeOnQuota, Args: Args{
		"id": "w_o1", "provider": "claude", "window": "5 h", "at": reset.UTC().Format(time.RFC3339),
	}}, at)
	if th.Failure != nil {
		t.Fatalf("failure not cleared: %+v", th.Failure)
	}
	if a := Compute(th, v, at); a.State != Waiting || a.Section != WorkingSec {
		t.Fatalf("after the wake: %+v", a)
	}

	// Retry clears it too.
	th2 := &Thread{ID: "t_2", ActivityAt: at, Conversation: Conversation{
		Failure: &Failure{Class: ClassQuota, Message: "Individual quota reached.", At: at},
	}}
	if why := Registry["thread.retry"].Available(th2, &View{}, at); why != "" {
		t.Fatalf("retry off: %s", why)
	}
}

// A failure that is not the quota's keeps the old rule: Failed in Needs you
// from the stop, and the row offers Retry.
func TestOtherFailuresStillFail(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	th, v := &Thread{ID: "t_1", ActivityAt: at, Conversation: Conversation{
		Failure: &Failure{Class: "api", Message: "529: overloaded", At: at},
	}}, &View{SeenAt: at}
	if a := Compute(th, v, at.Add(time.Minute)); a.State != Failed || a.Section != NeedsYou {
		t.Fatalf("%+v", a)
	}
	if c := NextCheck(th, v, at.Add(time.Minute)); c != nil {
		t.Fatalf("a clock was armed for a failure that is not the quota's: %v", c)
	}
}

// V0.quota-wait-midturn: a quota wait breaks neither a timed snooze nor a
// "snooze until work is done" before the reset, wakes once the reset passes,
// and lets the owner arm "Settle when done" while waiting.
func TestQuotaWaitSnoozeAndSettleWhenDone(t *testing.T) {
	start := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	stoppedAt := start.Add(5 * time.Minute)
	resetAt := stoppedAt.Add(time.Hour)
	th := &Thread{ID: "t_1", ActivityAt: stoppedAt, Conversation: Conversation{
		Failure: &Failure{Class: ClassQuota, Message: "Individual quota reached. Resets in 1h.", At: stoppedAt},
	}}

	// Timed snooze started before the quota stop stays snoozed until reset, then wakes.
	vTimed := snoozed(View{SeenAt: start}, start.Add(4*time.Hour), start)
	if a := Compute(th, &vTimed, stoppedAt.Add(10*time.Minute)); a.State != Snoozed {
		t.Fatalf("quota wait broke timed snooze: %+v", a)
	}
	if a := Compute(th, &vTimed, resetAt.Add(time.Minute)); a.State != Failed || a.Section != NeedsYou {
		t.Fatalf("after reset timed snooze did not wake to Failed: %+v", a)
	}

	// "Snooze until work is done" stays snoozed while waiting for quota, then wakes at reset.
	vDone := &View{SeenAt: start}
	SnoozeWhenDone(vDone, start)
	if a := Compute(th, vDone, stoppedAt.Add(10*time.Minute)); a.State != Snoozed {
		t.Fatalf("quota wait broke snooze-when-done: %+v", a)
	}
	if a := Compute(th, vDone, resetAt.Add(time.Minute)); a.State != Failed || a.Section != NeedsYou {
		t.Fatalf("after reset snooze-when-done did not wake to Failed: %+v", a)
	}

	// "Settle when done" is available during a quota wait and disabled once the reset passes.
	if why := Registry["thread.settleWhenDone"].Available(th, &View{SeenAt: stoppedAt}, stoppedAt.Add(10*time.Minute)); why != "" {
		t.Fatalf("settleWhenDone disabled during quota wait: %q", why)
	}
	if why := Registry["thread.settleWhenDone"].Available(th, &View{SeenAt: stoppedAt}, resetAt.Add(time.Minute)); why != "The last run failed" {
		t.Fatalf("settleWhenDone after reset: %q, want %q", why, "The last run failed")
	}
}
