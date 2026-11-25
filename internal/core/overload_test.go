package core

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// V0.overload-retry: only the CLI's own passing provider failure is read.
func TestOverloadNotice(t *testing.T) {
	yes := []string{
		"API Error: 529 Overloaded. This is a server-side issue, usually temporary — try again in a moment.",
		`API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
		"API Error: Repeated 529 Overloaded errors",
		"\n  API Error: 500 Internal server error",
		"API Error (503): upstream connect error",
		"API Error: 429 rate_limit_error",
		"API Error: Connection error.",
		"API Error: Request timed out.",
	}
	for _, s := range yes {
		if OverloadNotice(s) == "" {
			t.Errorf("not read as a passing provider failure: %q", s)
		}
	}
	no := []string{
		"",
		"API Error: 400 invalid_request_error: prompt is too long",
		"API Error: 401 authentication_error",
		"API Error: 404 model not found",
		"I fixed the retry of API Error: 529 Overloaded in the client.",
		"Done.\nAPI Error: 529 Overloaded",
		"You've hit your weekly limit · resets 23 Nov, 4am",
	}
	for _, s := range no {
		if n := OverloadNotice(s); n != "" {
			t.Errorf("read as a passing provider failure: %q", s)
		}
	}
}

func overloadEnd() Input {
	return Input{Source: Observer(ObsProcess), Kind: KindRunEnded, Args: RunEndedArgs{
		Failure: &Failure{Class: ClassOverloaded, Message: "API Error: 529 Overloaded."},
		Data:    map[string]any{"failure": nil},
	}}
}

// V0.overload-retry: the thread is Waiting in Working with the retry's time,
// breaks no snooze, and the clock retries it through the feed.
func TestOverloadStopWaitsAndRetries(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	th, v := &Thread{ID: "t_1", Profile: "claude", ActivityAt: now}, &View{SeenAt: now}
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, now)
	step(t, th, v, overloadEnd(), now)

	a := Compute(th, v, now.Add(10*time.Second))
	if a.State != Waiting || a.Section != WorkingSec || a.Reason != "Waiting for the provider · retry 1 of 7 at 12:01" {
		t.Fatalf("attention: %+v", a)
	}
	if NeedsOwnerNowAt(th, now) || !WorkGoingAt(th, now) {
		t.Fatalf("needsOwner=%v workGoing=%v, want a wait that goes on", NeedsOwnerNowAt(th, now), WorkGoingAt(th, now))
	}
	if why := Registry["thread.settleWhenDone"].Available(th, v, now); why != "" {
		t.Fatalf("settleWhenDone during the wait: %q", why)
	}
	if vs := CheckThread(th, v, now.Add(10*time.Second), nil); len(vs) != 0 {
		t.Fatalf("invariant violations: %+v", vs)
	}
	at := NextCheck(th, v, now)
	if at == nil || !at.Equal(now.Add(time.Minute)) {
		t.Fatalf("NextCheck=%v want %v", at, now.Add(time.Minute))
	}
	// Before the retry's time the clock does nothing.
	step(t, th, v, clockIn(), at.Add(-time.Second))
	if th.Failure == nil || len(th.Feed.Queue) != 0 {
		t.Fatalf("retried too early: failure=%+v queue=%+v", th.Failure, th.Feed.Queue)
	}
	res := step(t, th, v, clockIn(), *at)
	if th.Failure != nil || th.Starting == nil || th.OverloadRetries != 1 {
		t.Fatalf("not retried: failure=%+v starting=%v retries=%d", th.Failure, th.Starting, th.OverloadRetries)
	}
	if !slices.ContainsFunc(res.Effects, func(e Effect) bool { return e.Kind == EffectFeed }) ||
		!slices.ContainsFunc(res.Events, func(e Event) bool { return e.Type == "overload.retried" }) {
		t.Fatalf("missing feed effect or event: %+v %+v", res.Effects, res.Events)
	}
	if len(th.Feed.Queue) == 0 || th.Feed.Queue[0].Urgency != Wake || !strings.Contains(th.Feed.Queue[0].What, "automatic retry") {
		t.Fatalf("missing wake notice: %+v", th.Feed.Queue)
	}
	if a := Compute(th, v, *at); a.State != Working {
		t.Fatalf("attention after the retry: %+v", a)
	}
	if vs := CheckThread(th, v, *at, nil); len(vs) != 0 {
		t.Fatalf("invariant violations: %+v", vs)
	}
}

// V0.overload-retry: each retry that fails at once waits longer; when the
// schedule is used up the failure is a plain API failure, the owner's. A
// retried run that worked for a while starts the schedule over, and so does
// an owner move.
func TestOverloadRetrySchedule(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	th, v := &Thread{ID: "t_1", Profile: "claude", ActivityAt: now}, &View{SeenAt: now}
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, now)
	step(t, th, v, overloadEnd(), now)

	retry := func(ran time.Duration) Result {
		t.Helper()
		next := NextCheck(th, v, now)
		if next == nil {
			t.Fatalf("NextCheck=nil with retries=%d", th.OverloadRetries)
		}
		now = *next
		step(t, th, v, clockIn(), now)
		if th.Failure != nil {
			t.Fatalf("not retried at %v: %+v", now, th.Failure)
		}
		step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindFeedTake, Args: FeedTake{At: AtIdle}}, now.Add(time.Second))
		step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, now.Add(2*time.Second))
		now = now.Add(ran)
		return step(t, th, v, overloadEnd(), now)
	}
	var waits []time.Duration
	var last Result
	for i := 0; i < MaxOverloadRetries; i++ {
		waits = append(waits, NextCheck(th, v, now).Sub(now))
		last = retry(5 * time.Second)
	}
	if !slices.Equal(waits, overloadBackoff) {
		t.Fatalf("waits %v want %v", waits, overloadBackoff)
	}
	// The schedule is used up: Failed in Needs you, and the logged end says so.
	if th.Failure == nil || th.Failure.Class != ClassAPI || NextCheck(th, v, now) != nil {
		t.Fatalf("after the schedule: failure=%+v next=%v", th.Failure, NextCheck(th, v, now))
	}
	if a := Compute(th, v, now); a.State != Failed || a.Section != NeedsYou || a.Reason != "API Error: 529 Overloaded." {
		t.Fatalf("attention after the schedule: %+v", a)
	}
	logged := false
	for _, ev := range last.Events {
		if d, _ := ev.Data.(map[string]any); ev.Type == "run.ended" {
			f, _ := d["failure"].(*Failure)
			logged = f != nil && f.Class == ClassAPI
		}
	}
	if !logged {
		t.Fatalf("run.ended does not carry the plain failure: %+v", last.Events)
	}
	if why := Registry["thread.settleWhenDone"].Available(th, v, now); why != "The last run failed" {
		t.Fatalf("settleWhenDone after the schedule: %q", why)
	}
	// The owner's Retry starts the schedule over.
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.retry"}, now)
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, now)
	step(t, th, v, overloadEnd(), now)
	if d := NextCheck(th, v, now).Sub(now); d != overloadBackoff[0] {
		t.Fatalf("after the owner's Retry the wait is %v", d)
	}
	// A retried run that worked past OverloadProgress is a new incident.
	retry(5 * time.Second)
	if d := NextCheck(th, v, now).Sub(now); d != overloadBackoff[1] {
		t.Fatalf("second wait %v", d)
	}
	retry(OverloadProgress + time.Minute)
	if d := NextCheck(th, v, now).Sub(now); d != overloadBackoff[0] || th.OverloadRetries != 0 {
		t.Fatalf("after a run that got somewhere: wait %v retries %d", d, th.OverloadRetries)
	}
	// A run that ends well forgets the count.
	retry(5 * time.Second)
	step(t, th, v, clockIn(), *NextCheck(th, v, now))
	step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, now.Add(3*time.Minute))
	step(t, th, v, runEnd(), now.Add(4*time.Minute))
	if th.OverloadRetries != 0 || th.OverloadRetriedAt != nil || th.Failure != nil {
		t.Fatalf("a good run kept the count: %d %v %+v", th.OverloadRetries, th.OverloadRetriedAt, th.Failure)
	}
}

// V0.overload-retry: a settled or archived thread is not retried, and one
// unarchived after its retry came due shows the failure instead of starting
// a run nobody expects. A dismiss or a message ends the wait.
func TestOverloadRetryRespectsLifecycle(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.Local)
	mk := func() (*Thread, *View) {
		th, v := &Thread{ID: "t_1", Profile: "claude", ActivityAt: now}, &View{SeenAt: now}
		step(t, th, v, Input{Source: Observer(ObsProcess), Kind: KindRunStarted}, now)
		step(t, th, v, overloadEnd(), now)
		return th, v
	}
	th, v := mk()
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.archive"}, now.Add(time.Second))
	step(t, th, v, clockIn(), now.Add(time.Hour))
	if th.Failure == nil || th.Starting != nil {
		t.Fatalf("an archived thread was retried: %+v", th)
	}
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.unarchive"}, now.Add(2*time.Hour))
	if a := Compute(th, v, now.Add(2*time.Hour)); a.State != Failed || NextCheck(th, v, now.Add(2*time.Hour)) != nil {
		t.Fatalf("unarchived past its retry: %+v", a)
	}
	th, v = mk()
	step(t, th, v, Input{Source: FromOwner, Kind: "thread.dismiss"}, now.Add(time.Second))
	if th.Failure != nil || NextCheck(th, v, now.Add(time.Second)) != nil {
		t.Fatalf("a dismissed failure still retries: %+v", th.Failure)
	}
}
