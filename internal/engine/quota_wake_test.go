package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/meros/fyr/internal/api"
	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/core/coretest"
	"github.com/meros/fyr/internal/quota"
	"github.com/meros/fyr/internal/stream"
)

// V0.quota-reset-wake: agent watch {when: {kind: "quota"}} and owner
// thread.wakeOnQuota resolve against QuotaReset without e.mu, follow a moved
// reset time via SyncQuotaResets, wake at reset and emit FrameQuotaReset.
func TestWatchQuotaResolvesFollowsResetAndFires(t *testing.T) {
	e, _, now := bare(t)
	reset := now.Add(2 * time.Hour)
	var stale bool
	var tokenOnly bool
	e.QuotaReset = func(_ context.Context, provider, window string) (string, time.Time, error) {
		if stale {
			return "", time.Time{}, errors.New("quota for \"claude\" is stale; wait for a fresh reading")
		}
		if tokenOnly {
			return "", time.Time{}, errors.New("provider \"tok\" is a token account read only from running sessions; no reset time is known")
		}
		if window == "" {
			window = "5 h"
		}
		return window, reset, nil
	}
	var snap []quota.Status
	e.QuotaSnapshot = func() []quota.Status { return snap }
	guardDeps(t, e)

	a := bareThread(t, e)

	// Stale and token-account reads are refused with the reason.
	stale = true
	out, isErr := callAs(t, e, a, "watch", map[string]any{"when": map[string]any{"kind": "quota", "window": "5 h"}, "then": "notify", "note": "go"})
	if !isErr || !strings.Contains(out, "stale") {
		t.Fatalf("stale watch not refused: %v %q", isErr, out)
	}
	stale, tokenOnly = false, true
	out, isErr = callAs(t, e, a, "watch", map[string]any{"when": map[string]any{"kind": "quota", "provider": "tok"}, "then": "notify", "note": "go"})
	if !isErr || !strings.Contains(out, "token account") {
		t.Fatalf("token account not refused: %v %q", isErr, out)
	}
	tokenOnly = false

	// Agent arms a quota watch; retry returns the same armed watch.
	args := map[string]any{"when": map[string]any{"kind": "quota", "window": "5 h"}, "then": "notify", "note": "continue the task"}
	first, isErr := callAs(t, e, a, "watch", args)
	if isErr || !strings.HasPrefix(first, "Armed the watch w_") {
		t.Fatalf("arm: %v %q", isErr, first)
	}
	again, isErr := callAs(t, e, a, "watch", args)
	if isErr || !strings.HasPrefix(again, "Already armed:") {
		t.Fatalf("retry: %v %q", isErr, again)
	}

	// SyncQuotaResets updates the armed wake when ResetsAt shifts.
	moved := reset.Add(10 * time.Minute)
	e.SyncQuotaResets([]quota.Status{{
		Profile: "claude",
		Kind:    quota.KindAnthropic,
		Windows: []quota.Window{{Name: "5 h", UsedPct: 100, ResetsAt: &moved}},
	}})
	th, _ := e.Thread(a)
	if ws := core.Wakes(&th); len(ws) != 1 || !ws[0].At.Equal(moved) || ws[0].Window != "5 h" {
		t.Fatalf("after sync: %+v", ws)
	}

	// Subscribe to stream frames and advance time to the reset.
	sub := e.Hub.Subscribe()
	defer e.Hub.Unsubscribe(sub)
	*now = moved
	e.Tick()

	th, _ = e.Thread(a)
	if len(core.Wakes(&th)) != 0 {
		t.Fatalf("wake did not fire: %+v", core.Wakes(&th))
	}
	if len(th.Feed.Queue) == 0 || !strings.Contains(th.Feed.Queue[0].What, "continue the task") {
		t.Fatalf("agent not woken: %+v", th.Feed.Queue)
	}
	gotFrame := false
	for len(sub) > 0 {
		f := <-sub
		if f.Event == stream.FrameQuotaReset {
			var d map[string]any
			_ = json.Unmarshal(f.Data, &d)
			if d["threadId"] == a && d["window"] == "5 h" {
				gotFrame = true
			}
		}
	}
	if !gotFrame {
		t.Fatal("no FrameQuotaReset emitted when quota watch fired")
	}

	// Owner arms thread.wakeOnQuota on a failed thread, clearing failure.
	b := bareThread(t, e)
	coretest.Patch(e, b, func(x *core.Thread, n time.Time) []core.Event {
		x.Failure = &core.Failure{Class: "api", Message: "Quota reached", At: n}
		return nil
	})
	reset = now.Add(time.Hour)
	r := e.Command(api.Cmd{CommandID: "q1", Action: core.KindWakeOnQuota, ThreadID: b, Args: core.Args{"window": "5 h"}})
	if !r.OK {
		t.Fatalf("wakeOnQuota failed: %+v", r)
	}
	thB, _ := e.Thread(b)
	if thB.Failure != nil || len(core.Wakes(&thB)) != 1 || core.Wakes(&thB)[0].Window != "5 h" {
		t.Fatalf("owner wake state: failure=%+v wakes=%+v", thB.Failure, core.Wakes(&thB))
	}

	// Unarmed failed thread also gets FrameQuotaReset when an exhausted window passes reset.
	c := bareThread(t, e)
	coretest.Patch(e, c, func(x *core.Thread, n time.Time) []core.Event {
		x.Failure = &core.Failure{Class: "api", Message: "Quota reached", At: n}
		return nil
	})
	expReset := now.Add(30 * time.Minute)
	snap = []quota.Status{{
		Profile: "claude",
		Kind:    quota.KindAnthropic,
		Windows: []quota.Window{{Name: "5 h", UsedPct: 100, ResetsAt: &expReset}},
	}}
	e.Tick()
	*now = expReset
	e.Tick()
	gotC := false
	for len(sub) > 0 {
		f := <-sub
		if f.Event == stream.FrameQuotaReset {
			var d map[string]any
			_ = json.Unmarshal(f.Data, &d)
			if d["threadId"] == c && d["window"] == "5 h" {
				gotC = true
			}
		}
	}
	if !gotC {
		t.Fatal("no FrameQuotaReset emitted for unarmed failed thread when exhausted window reset")
	}
}

// V0.quota-auto-continue: Engine.Tick auto-continues a quota-stopped thread
// after its reset + buffer via KindClock + EffectFeed without ever flickering
// through Failed or sending a Failed attention notification.
func TestQuotaAutoContinueViaEngineTick(t *testing.T) {
	e, _, now := bare(t)
	fed := make(chan string, 8)
	e.Agents = fakeAgents(func(id string, ef core.Effect) error {
		if ef.Kind == core.EffectFeed {
			fed <- id
		}
		return nil
	})
	var states []core.State
	e.OnAttention = func(d api.Thread, _ core.State) {
		states = append(states, d.Attention.State)
	}

	id := bareThread(t, e)
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunStarted, Thread: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(core.Input{Source: core.Observer(core.ObsProcess), Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{
		Failure: &core.Failure{Class: core.ClassQuota, Message: "⚠ Individual quota reached. Resets in 1h."},
		Data:    map[string]any{},
	}}); err != nil {
		t.Fatal(err)
	}
	resetAt := now.Add(time.Hour)
	states = nil

	// At exact reset (inside buffer): still Waiting, no Failed notification.
	*now = resetAt
	e.Tick()
	th, _ := e.Thread(id)
	if th.Failure == nil {
		t.Fatal("resumed before buffer elapsed")
	}

	// Past reset + buffer: Tick auto-continues via KindClock -> EffectFeed, never hits Failed.
	*now = resetAt.Add(time.Minute)
	e.Tick()

	select {
	case got := <-fed:
		if got != id {
			t.Fatalf("fed wrong thread %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("EffectFeed not dispatched on quota reset")
	}
	th, _ = e.Thread(id)
	if th.Failure != nil {
		t.Fatalf("failure not cleared after Tick: %+v", th.Failure)
	}
	if len(th.Feed.Queue) == 0 || !strings.Contains(th.Feed.Queue[0].What, "quota has reset") {
		t.Fatalf("continue notice missing: %+v", th.Feed.Queue)
	}
	for _, st := range states {
		if st == core.Failed {
			t.Fatalf("thread flickered to Failed during quota auto-continue: %v", states)
		}
	}
}

// V0.quota-auto-continue: both thread.wakeOnQuota and watch {when: {kind: "quota"}}
// fall back to core.QuotaReset(t.Failure) on providers without quota.Service
// polling (such as antigravity) when the notice's reset time is in the future,
// and refuse expired/missing notices, mismatched providers, or unknown windows.
func TestQuotaWakeFallsBackToNoticeReset(t *testing.T) {
	e, _, now := bare(t)
	e.QuotaReset = func(_ context.Context, provider, _ string) (string, time.Time, error) {
		return "", time.Time{}, errors.New(`provider "` + provider + `" has no quota tracking`)
	}
	guardDeps(t, e)

	// 1. Agent watch {when: {kind: "quota"}} resolves from the thread's quota notice.
	a := bareThread(t, e)
	coretest.Patch(e, a, func(x *core.Thread, n time.Time) []core.Event {
		x.Profile = "antigravity"
		x.Starting = nil
		x.Failure = &core.Failure{Class: core.ClassQuota, Message: "⚠ Individual quota reached. Resets in 2h.", At: n}
		return nil
	})
	wantReset := now.Add(2 * time.Hour).UTC()
	out, isErr := callAs(t, e, a, "watch", map[string]any{"when": map[string]any{"kind": "quota"}, "then": "notify", "note": "continue review"})
	if isErr || !strings.HasPrefix(out, "Armed the watch w_") {
		t.Fatalf("watch fallback failed: %v %q", isErr, out)
	}
	thA, _ := e.Thread(a)
	if ws := core.Wakes(&thA); len(ws) != 1 || !ws[0].At.Equal(wantReset) || ws[0].Provider != "antigravity" {
		t.Fatalf("watch fallback wakes: %+v (want %v)", ws, wantReset)
	}

	// Explicit unknown window or different provider must still be refused.
	if _, isErr := callAs(t, e, a, "watch", map[string]any{"when": map[string]any{"kind": "quota", "window": "5 h"}, "then": "notify", "note": "x"}); !isErr {
		t.Fatal("watch fallback accepted an explicit unknown window")
	}
	if _, isErr := callAs(t, e, a, "watch", map[string]any{"when": map[string]any{"kind": "quota", "provider": "other"}, "then": "notify", "note": "x"}); !isErr {
		t.Fatal("watch fallback used notice for a different provider")
	}

	// 2. Owner thread.wakeOnQuota resolves from the thread's quota notice even when e.QuotaReset is nil.
	b := bareThread(t, e)
	coretest.Patch(e, b, func(x *core.Thread, n time.Time) []core.Event {
		x.Profile = "antigravity"
		x.Starting = nil
		x.Failure = &core.Failure{Class: core.ClassQuota, Message: "⚠ Individual quota reached. Resets in 90m.", At: n}
		return nil
	})
	e.QuotaReset = nil
	r := e.Command(api.Cmd{CommandID: "q-fallback-1", Action: core.KindWakeOnQuota, ThreadID: b, Args: core.Args{}})
	if !r.OK {
		t.Fatalf("wakeOnQuota fallback failed: %+v", r)
	}
	thB, _ := e.Thread(b)
	wantB := now.Add(90 * time.Minute).UTC()
	if thB.Failure != nil || len(core.Wakes(&thB)) != 1 || !core.Wakes(&thB)[0].At.Equal(wantB) {
		t.Fatalf("wakeOnQuota fallback state: failure=%+v wakes=%+v", thB.Failure, core.Wakes(&thB))
	}

	// 3. Past/expired notice is refused.
	c := bareThread(t, e)
	coretest.Patch(e, c, func(x *core.Thread, n time.Time) []core.Event {
		x.Profile = "antigravity"
		x.Starting = nil
		x.Failure = &core.Failure{Class: core.ClassQuota, Message: "⚠ Individual quota reached. Resets in 10m.", At: n.Add(-time.Hour)}
		return nil
	})
	if r := e.Command(api.Cmd{CommandID: "q-fallback-past", Action: core.KindWakeOnQuota, ThreadID: c, Args: core.Args{}}); r.OK {
		t.Fatalf("expired notice was accepted: %+v", r)
	}
}

type fakeAgents func(id string, ef core.Effect) error

func (f fakeAgents) Do(id string, ef core.Effect) error { return f(id, ef) }

// V0.quota-reset-zone: the owner's case. A first limit on a thread, written as
// "resets 4am (Europe/Stockholm)", arms a wait for that clock in that zone and
// continues the thread without the owner. The variants cover a message the
// provider refused with the limit (it stays unread in the outbox) and the
// spellings of the reset.
func TestQuotaFirstLimitArmsWaitAndContinues(t *testing.T) {
	cases := []struct {
		name   string
		msg    string
		outbox bool
		reset  time.Time // now is 2026-11-15 10:00 UTC
	}{
		{"time only", "You've hit your weekly limit · resets 4am (Europe/Stockholm)", false, time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC)},
		{"unread message", "You've hit your weekly limit · resets 4am (Europe/Stockholm)", true, time.Date(2026, 11, 16, 3, 0, 0, 0, time.UTC)},
		{"session", "You've hit your session limit · resets 3pm (Europe/Stockholm)", false, time.Date(2026, 11, 15, 14, 0, 0, 0, time.UTC)},
		{"date at", "You've hit your weekly limit · resets Nov 17 at 4am (Europe/Stockholm)", false, time.Date(2026, 11, 17, 3, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e, _, now := bare(t)
			fed := make(chan string, 8)
			e.Agents = fakeAgents(func(id string, ef core.Effect) error {
				if ef.Kind == core.EffectFeed {
					fed <- id
				}
				return nil
			})
			id := bareThread(t, e)
			if c.outbox {
				coretest.Patch(e, id, func(x *core.Thread, _ time.Time) []core.Event {
					core.SetOutbox(x, "m1", "sent")
					return nil
				})
			}
			obs := core.Observer(core.ObsProcess)
			if _, err := e.Apply(core.Input{Source: obs, Kind: core.KindRunStarted, Thread: id}); err != nil {
				t.Fatal(err)
			}
			if _, err := e.Apply(core.Input{Source: obs, Kind: core.KindRunEnded, Thread: id, Args: core.RunEndedArgs{
				Failure: &core.Failure{Class: core.ClassQuota, Message: c.msg},
				Data:    map[string]any{},
			}}); err != nil {
				t.Fatal(err)
			}
			th, _ := e.Thread(id)
			if got := core.QuotaReset(th.Failure); got == nil || !got.Equal(c.reset) {
				t.Fatalf("reset %v, want %v", got, c.reset)
			}
			if !core.QuotaCanAutoContinue(&th) {
				t.Fatalf("no auto-continue armed: outbox=%+v", th.Outbox)
			}
			*now = c.reset.Add(core.QuotaResumeBuffer - time.Second)
			e.Tick()
			if th, _ = e.Thread(id); th.Failure == nil {
				t.Fatal("continued before the reset and buffer")
			}
			*now = c.reset.Add(core.QuotaResumeBuffer + time.Second)
			e.Tick()
			select {
			case <-fed:
			case <-time.After(2 * time.Second):
				t.Fatal("not continued at the reset without the owner")
			}
			if th, _ = e.Thread(id); th.Failure != nil {
				t.Fatalf("failure not cleared: %+v", th.Failure)
			}
		})
	}
}
