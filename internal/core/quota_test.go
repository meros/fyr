package core

import (
	"slices"
	"strings"
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
		{"You've hit your weekly limit · resets 23 Nov, 4am (Europe/Stockholm)", time.Date(2026, 11, 23, 3, 0, 0, 0, time.UTC)}, // Stockholm is UTC+1 in November
		{"You've hit your limit · resets Nov 24 at 4am (Europe/Stockholm)", time.Date(2026, 11, 24, 3, 0, 0, 0, time.UTC)},
		{"You've hit your session limit · resets 3pm (Europe/Stockholm)", time.Date(2026, 11, 21, 14, 0, 0, 0, time.UTC)},
		{"You've hit your weekly limit · resets 4am (Europe/Stockholm)", time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)},
		{"You've hit your limit · resets 4 am", time.Date(2026, 11, 22, 4, 0, 0, 0, time.UTC)},
		{"You've hit your limit · resets Nov 24, 4:30am (Not/AZone)", time.Date(2026, 11, 24, 4, 30, 0, 0, time.UTC)},
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

// V0.quota-wait, V0.quota-auto-continue: a run the provider's quota stopped is
// Waiting in Working, not Failed in Needs you, until the reset + buffer; then
// KindClock automatically continues the thread rather than flipping to Failed.
func TestQuotaStopIsWaiting(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	reset := at.Add(2 * time.Hour)
	th, v := &Thread{ID: "t_1", ActivityAt: at, Conversation: Conversation{
		Failure: &Failure{Class: ClassQuota, Message: "Individual quota reached. Resets in 2h.", At: at},
	}}, &View{SeenAt: at}
	a := Compute(th, v, at.Add(time.Minute))
	if a.State != Waiting || a.Section != WorkingSec || a.Reason != "Waiting for quota · resets 14:00" {
		t.Fatalf("at the stop: %+v", a)
	}
	// Not the owner's move, and counts as work going on.
	if NeedsOwnerAt(th, at.Add(time.Minute)) || !WorkGoingAt(th, at.Add(time.Minute)) {
		t.Fatalf("NeedsOwner=%v WorkGoing=%v", NeedsOwnerAt(th, at.Add(time.Minute)), WorkGoingAt(th, at.Add(time.Minute)))
	}
	// The clock schedules a check slightly past the exact reset second (buffer).
	c := NextCheck(th, v, at.Add(time.Minute))
	if c == nil || !c.After(reset) || c.Sub(reset) > time.Minute {
		t.Fatalf("next check %v, want shortly after %v", c, reset)
	}
	// During the buffer past the exact reset, the thread still waits (never flickers to Failed).
	if a := Compute(th, v, reset.Add(time.Second)); a.State != Waiting || a.Section != WorkingSec {
		t.Fatalf("during reset buffer: %+v", a)
	}
	// Even at or past the resume time before KindClock runs, it waits to resume rather than flipping to Failed.
	if a := Compute(th, v, c.Add(time.Second)); a.State != Waiting || a.Section != WorkingSec {
		t.Fatalf("before KindClock runs: %+v", a)
	}
	if c2 := NextCheck(th, v, c.Add(time.Second)); c2 == nil || !c2.Equal(*c) {
		t.Fatalf("next check before clock fires: %v, want %v", c2, c)
	}
	// So "settle when done" waits for the quota wait and does not settle it.
	if WorkDoneAt(th, at.Add(time.Minute)) || WorkDoneAt(th, c.Add(time.Second)) {
		t.Fatal("a thread waiting for quota auto-continue is done")
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

// V0.quota-wait-midturn, V0.quota-auto-continue: a quota wait and its
// automatic continue break neither a timed snooze nor a "snooze until work is
// done", and preserve "Settle when done" across the reset until work finishes
// or loop protection stops auto-continuing.
func TestQuotaWaitSnoozeAndSettleWhenDone(t *testing.T) {
	start := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	stoppedAt := start.Add(5 * time.Minute)
	resetAt := stoppedAt.Add(time.Hour)
	th := &Thread{ID: "t_1", ActivityAt: stoppedAt, Conversation: Conversation{
		Failure: &Failure{Class: ClassQuota, Message: "Individual quota reached. Resets in 1h.", At: stoppedAt},
	}}

	// Timed snooze started before the quota stop stays snoozed across the wait and auto-continue.
	vTimed := snoozed(View{SeenAt: start}, start.Add(4*time.Hour), start)
	if a := Compute(th, &vTimed, stoppedAt.Add(10*time.Minute)); a.State != Snoozed {
		t.Fatalf("quota wait broke timed snooze: %+v", a)
	}
	thTimed := CloneThread(th)
	resumeAt := NextCheck(thTimed, &vTimed, stoppedAt.Add(10*time.Minute))
	if resumeAt == nil {
		t.Fatal("no next check for quota auto-continue")
	}
	step(t, thTimed, &vTimed, clockIn(), *resumeAt)
	if a := Compute(thTimed, &vTimed, *resumeAt); a.State != Snoozed {
		t.Fatalf("auto-continue broke timed snooze: %+v", a)
	}

	// "Snooze until work is done" stays snoozed while waiting AND through auto-continue,
	// then wakes when the resumed run finishes cleanly.
	thDone := CloneThread(th)
	vDone := &View{SeenAt: start}
	SnoozeWhenDone(vDone, start)
	if a := Compute(thDone, vDone, stoppedAt.Add(10*time.Minute)); a.State != Snoozed {
		t.Fatalf("quota wait broke snooze-when-done: %+v", a)
	}
	step(t, thDone, vDone, clockIn(), *resumeAt)
	if a := Compute(thDone, vDone, *resumeAt); a.State != Snoozed {
		t.Fatalf("auto-continue broke snooze-when-done: %+v", a)
	}
	step(t, thDone, vDone, Input{Source: Observer(ObsProcess), Kind: KindFeedTake, Args: FeedTake{At: AtIdle}}, resumeAt.Add(time.Second))
	step(t, thDone, vDone, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, resumeAt.Add(2*time.Second))
	step(t, thDone, vDone, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}, resumeAt.Add(time.Minute))
	if a := Compute(thDone, vDone, resumeAt.Add(time.Minute)); a.State == Snoozed {
		t.Fatalf("snooze-when-done stayed snoozed after resumed run finished: %+v", a)
	}

	// "Settle when done" stays available during quota wait and survives auto-continue,
	// settling once the resumed run finishes cleanly.
	thSettle := CloneThread(th)
	vSettle := &View{SeenAt: stoppedAt}
	if why := Registry["thread.settleWhenDone"].Available(thSettle, vSettle, stoppedAt.Add(10*time.Minute)); why != "" {
		t.Fatalf("settleWhenDone disabled during quota wait: %q", why)
	}
	step(t, thSettle, vSettle, Input{Source: FromOwner, Kind: "thread.settleWhenDone"}, stoppedAt.Add(10*time.Minute))
	step(t, thSettle, vSettle, clockIn(), *resumeAt)
	if thSettle.Settle.At != nil {
		t.Fatalf("settleWhenDone fired prematurely on auto-continue")
	}
	if !SettlesWhenDone(thSettle) {
		t.Fatalf("settleWhenDone was dropped on auto-continue")
	}
	step(t, thSettle, vSettle, Input{Source: Observer(ObsProcess), Kind: KindFeedTake, Args: FeedTake{At: AtIdle}}, resumeAt.Add(time.Second))
	step(t, thSettle, vSettle, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, resumeAt.Add(2*time.Second))
	step(t, thSettle, vSettle, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{}}, resumeAt.Add(time.Minute))
	if thSettle.Settle.At == nil {
		t.Fatalf("settleWhenDone did not settle after resumed run finished")
	}
	_ = resetAt
}

// V0.quota-auto-continue: reaching QuotaReset + buffer on KindClock clears the
// quota failure, sets Starting, queues a waking continue notice, and emits EffectFeed.
func TestQuotaAutoContinueAtReset(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	reset := at.Add(2*time.Hour + 9*time.Minute + 56*time.Second)
	th, v := &Thread{ID: "t_1", Profile: "antigravity", ActivityAt: at, Conversation: Conversation{
		Failure: &Failure{Class: ClassQuota, Message: "⚠ Individual quota reached. Resets in 2h9m56s.", At: at},
	}}, &View{SeenAt: at}

	resumeAt := NextCheck(th, v, at.Add(time.Minute))
	if resumeAt == nil || !resumeAt.After(reset) {
		t.Fatalf("NextCheck=%v want > %v", resumeAt, reset)
	}
	// Clock firing before resumeAt (even right at the exact reset second) does not resume yet.
	resEarly := step(t, th, v, clockIn(), reset)
	if th.Failure == nil || len(th.Feed.Queue) != 0 || slices.ContainsFunc(resEarly.Effects, func(e Effect) bool { return e.Kind == EffectFeed }) {
		t.Fatalf("resumed too early at exact reset: failure=%+v queue=%+v", th.Failure, th.Feed.Queue)
	}
	// At resumeAt, KindClock auto-continues the thread via the feed.
	res := step(t, th, v, clockIn(), *resumeAt)
	if th.Failure != nil {
		t.Fatalf("failure not cleared: %+v", th.Failure)
	}
	if th.Starting == nil {
		t.Fatal("Starting not set on auto-continue")
	}
	if a := Compute(th, v, *resumeAt); a.State != Working || a.Section != WorkingSec {
		t.Fatalf("attention after auto-continue: %+v", a)
	}
	if !slices.ContainsFunc(res.Effects, func(e Effect) bool { return e.Kind == EffectFeed }) {
		t.Fatalf("missing EffectFeed: %+v", res.Effects)
	}
	if len(th.Feed.Queue) == 0 || th.Feed.Queue[0].Urgency != Wake || !strings.Contains(th.Feed.Queue[0].What, "quota has reset") {
		t.Fatalf("missing wake notice: %+v", th.Feed.Queue)
	}
	if !slices.ContainsFunc(res.Events, func(e Event) bool { return e.Type == "quota.continued" }) {
		t.Fatalf("missing quota.continued event: %+v", res.Events)
	}
	if vs := CheckThread(th, v, *resumeAt, nil); len(vs) != 0 {
		t.Fatalf("invariant violations: %+v", vs)
	}
}

// V0.quota-auto-continue: loop protection backs off immediate quota rejections,
// stops after the bounded number of consecutive auto-resumes, surfaces a clean
// non-stale failure reason, and resets when a run succeeds or the owner acts.
func TestQuotaAutoContinueLoopProtection(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	th, v := &Thread{ID: "t_1", Profile: "antigravity", ActivityAt: now}, &View{SeenAt: now}
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, now)
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{
		Failure: &Failure{Class: ClassQuota, Message: "⚠ Individual quota reached. Resets in 10s."},
	}}, now)

	// Run through 3 consecutive immediate-rejection auto-resumes; verify backoff increases on immediate failures.
	var prevDelay time.Duration
	for attempt := 1; attempt <= 3; attempt++ {
		next := NextCheck(th, v, now)
		if next == nil {
			t.Fatalf("attempt %d: NextCheck=nil", attempt)
		}
		delay := next.Sub(now)
		if attempt >= 2 && delay < time.Minute {
			t.Fatalf("attempt %d: immediate re-fail had tiny delay %v, want >= 1m backoff", attempt, delay)
		}
		if attempt >= 3 && delay <= prevDelay {
			t.Fatalf("attempt %d: backoff did not grow (%v <= %v)", attempt, delay, prevDelay)
		}
		prevDelay = delay
		now = *next
		step(t, th, v, clockIn(), now)
		if th.Failure != nil || len(th.Feed.Queue) == 0 {
			t.Fatalf("attempt %d: did not auto-continue: failure=%+v queue=%+v", attempt, th.Failure, th.Feed.Queue)
		}
		step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindFeedTake, Args: FeedTake{At: AtIdle}}, now.Add(time.Second))
		step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, now.Add(2*time.Second))
		// Immediate quota rejection 5 seconds after resume.
		now = now.Add(5 * time.Second)
		step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{
			Failure: &Failure{Class: ClassQuota, Message: "⚠ Individual quota reached. Resets in 10s."},
		}}, now)
	}

	// After 3 consecutive auto-continues, the 4th quota stop halts for the owner.
	if next := NextCheck(th, v, now); next != nil {
		t.Fatalf("still scheduling auto-continue past bound: %v", next)
	}
	a := Compute(th, v, now.Add(2*time.Hour))
	if a.State != Failed || a.Section != NeedsYou || a.Group != GroupUnblock {
		t.Fatalf("expected Failed in NeedsYou after loop limit: %+v", a)
	}
	if strings.Contains(a.Reason, "Resets in") {
		t.Fatalf("stale relative countdown leaked into Failed reason: %q", a.Reason)
	}
	if why := Registry["thread.settleWhenDone"].Available(th, v, now); why != "The last run failed" {
		t.Fatalf("settleWhenDone after loop stop: %q", why)
	}
	// Owner Retry resets the counter so future quota stops auto-continue again.
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.retry"}, now.Add(time.Minute))
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, now.Add(2*time.Minute))
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{
		Failure: &Failure{Class: ClassQuota, Message: "⚠ Individual quota reached. Resets in 30m."},
	}}, now.Add(3*time.Minute))
	if next := NextCheck(th, v, now.Add(4*time.Minute)); next == nil {
		t.Fatal("owner Retry did not reset quota auto-continue counter")
	}
}

// V0.quota-auto-continue: settled, archived, running, open-decision, handover,
// threads never auto-continue; an unread outbox message does not stop one.
func TestQuotaAutoContinueRespectsLifecycle(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	late := at.Add(3 * time.Hour)
	base := func() (*Thread, *View) {
		return &Thread{ID: "t_1", Profile: "antigravity", ActivityAt: at, Conversation: Conversation{
			Failure: &Failure{Class: ClassQuota, Message: "Individual quota reached. Resets in 1h.", At: at},
		}}, &View{SeenAt: at}
	}

	cases := []struct {
		name string
		prep func(*Thread)
	}{
		{"settled", func(th *Thread) { s := at.Add(time.Minute); th.Settle.At = &s }},
		{"archived", func(th *Thread) { a := at.Add(time.Minute); th.ArchivedAt = &a }},
		{"running", func(th *Thread) { th.Running = true; th.RunStartedAt = &at }},
		{"open-decision", func(th *Thread) { th.Decisions = []Decision{{ID: "d1", Kind: DecisionAsk, Assignee: Owner}} }},
		{"handover", func(th *Thread) { th.Handover = &Handover{To: "claude", At: at} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			th, v := base()
			tc.prep(th)
			if c := NextCheck(th, v, late); c != nil {
				t.Fatalf("%s: NextCheck=%v, want nil", tc.name, c)
			}
			res := step(t, th, v, clockIn(), late)
			if len(th.Feed.Queue) != 0 || slices.ContainsFunc(res.Effects, func(e Effect) bool { return e.Kind == EffectFeed }) {
				t.Fatalf("%s: auto-continued unexpectedly: queue=%+v", tc.name, th.Feed.Queue)
			}
		})
	}

	// The message the limit refused is unread: it does not hold the wait back.
	{
		th, v := base()
		th.Outbox = []OutMsg{{ID: "m1", State: "sent"}}
		if c := NextCheck(th, v, late); c == nil {
			t.Fatal("an unread outbox message stopped the auto-continue")
		}
	}

	// Unarchiving a thread whose quota reset passed long ago does not surprise-launch a run.
	th, v := base()
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.archive"}, at.Add(5*time.Minute))
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.unarchive"}, late)
	if c := NextCheck(th, v, late); c != nil {
		t.Fatalf("unarchived expired quota thread scheduled auto-continue: %v", c)
	}
	a := Compute(th, v, late)
	if a.State != Failed || strings.Contains(a.Reason, "Resets in") {
		t.Fatalf("unarchived expired quota thread: %+v", a)
	}
}

// V0.quota-auto-continue: StateText and Attention never display a stale
// relative "Resets in …" countdown when a quota failure surfaces as Failed.
func TestQuotaStaleFailureReason(t *testing.T) {
	at := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	late := at.Add(5 * time.Hour)
	th, v := &Thread{ID: "t_1", Title: "Review", ActivityAt: at, Conversation: Conversation{
		Failure:      &Failure{Class: ClassQuota, Message: "⚠ Individual quota reached. Resets in 2h9m56s.", At: at},
		QuotaResumes: 3,
	}}, &View{SeenAt: at}
	a := Compute(th, v, late)
	if a.State != Failed || a.Section != NeedsYou {
		t.Fatalf("attention: %+v", a)
	}
	if strings.Contains(a.Reason, "Resets in") {
		t.Fatalf("Attention.Reason leaked stale countdown: %q", a.Reason)
	}
	st, err := StateText(th, "thread", late)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(st, "Resets in") {
		t.Fatalf("StateText leaked stale countdown: %q", st)
	}
}
