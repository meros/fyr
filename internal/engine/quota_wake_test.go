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
