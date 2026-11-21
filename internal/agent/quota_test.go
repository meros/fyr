package agent

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/provider"
)

// qhHost records the Inputs the manager observes.
type qhHost struct{ in []core.Input }

func (h *qhHost) Apply(in core.Input) (any, error) {
	h.in = append(h.in, in)
	return nil, nil
}
func (h *qhHost) Thread(id string) (core.Thread, bool) { return core.Thread{ID: id, Profile: "claude"}, true }

func (h *qhHost) runEnded() *core.RunEndedArgs {
	for _, in := range h.in {
		if in.Kind == core.KindRunEnded {
			a, _ := in.Args.(core.RunEndedArgs)
			return &a
		}
	}
	return nil
}

// V0.quota-wait: claude ends the run with an error whose text is the
// provider's notice; the failure is classed quota, not api or error, so the
// thread waits for the reset instead of needing the owner.
func TestQuotaFailureFromAnErroredResult(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "claude"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true}
	m.handle(s, map[string]any{
		"type": "result", "subtype": "error", "is_error": true,
		"result": "You've hit your weekly limit · resets 23 Nov, 4am (Europe/Stockholm)",
	})
	a := h.runEnded()
	if a == nil || a.Failure == nil {
		t.Fatalf("no failure: %+v", a)
	}
	if a.Failure.Class != core.ClassQuota {
		t.Fatalf("class %q: %+v", a.Failure.Class, a.Failure)
	}
}

// V0.quota-wait: agy ends the run cleanly and writes the notice as the turn's
// last words; the same class, so the thread reads the same on both providers.
func TestQuotaFailureFromACleanResult(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "agy"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true}
	m.handle(s, map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"result": "\n⚠ Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 35m47s.\n",
	})
	a := h.runEnded()
	if a == nil || a.Failure == nil || a.Failure.Class != core.ClassQuota {
		t.Fatalf("no quota failure: %+v", a)
	}
	if !strings.HasPrefix(a.Failure.Message, "⚠ Individual quota reached.") {
		t.Fatalf("message %q", a.Failure.Message)
	}
}

// An interrupted run is the owner's Stop, never a quota stop.
func TestAnInterruptedRunIsNotAQuotaStop(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "agy"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true, interrupt: true}
	m.handle(s, map[string]any{
		"type": "result", "subtype": "interrupted", "is_error": false,
		"result": "⚠ Individual quota reached. Resets in 35m47s.",
	})
	if a := h.runEnded(); a == nil || a.Failure != nil {
		t.Fatalf("an interrupted run failed: %+v", a)
	}
}

// A clean end whose text is not the notice keeps failing nothing.
func TestACleanEndWithAnAgentReportIsNotAQuotaStop(t *testing.T) {
	h := &qhHost{}
	m := New(h, Config{Profile: func(string) provider.Profile { return provider.Profile{ID: "claude"} }})
	s := &session{thread: "t1", digest: newDigest(), live: true}
	m.handle(s, map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"result": "I made the retry show an Individual quota reached error as a wait.",
	})
	if a := h.runEnded(); a == nil || a.Failure != nil {
		t.Fatalf("an agent's report failed the run: %+v", a)
	}
}
