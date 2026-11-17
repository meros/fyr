// V0.163: slot_acquire and slot_release keep their names, but their answers, the gate's refusal and the notices
// speak a percentage of the host, not slots.
package engine

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/slots"
)

func TestV163ToolsSpeakPercent(t *testing.T) {
	e, _, _ := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	p.Cost = func(h slots.Holder) float64 { return 0.9 }
	p.Admit = func(head slots.Holder, held []slots.Holder) (bool, string) {
		if len(held) == 0 {
			return true, ""
		}
		return false, "needs ≈ 45 %, 10 % free"
	}
	e.UseSlots(p)
	e.HostName = "meros-laptop"
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	out, _ := callAs(t, e, a, "slot_acquire", map[string]any{"kind": "test"})
	if !strings.Contains(out, "Admitted: your test job ≈ 45 % of meros-laptop; 55 % left.") || strings.Contains(out, "slot.") {
		t.Fatalf("held %q", out)
	}
	out, _ = callAs(t, e, b, "slot_acquire", map[string]any{"kind": "e2e"})
	if !strings.Contains(out, "Queued: your e2e job needs ≈ 45 % of meros-laptop, 55 % free (0 ahead).") || !strings.Contains(out, "Waiting: needs ≈ 45 %, 10 % free.") {
		t.Fatalf("queued %q", out)
	}
	out, _ = callAs(t, e, a, "slot_release", map[string]any{})
	if !strings.Contains(out, "Released:") || !strings.Contains(out, "% of meros-laptop") {
		t.Fatalf("release %q", out)
	}
	if r := e.HeavyGate(bareThread(t, e), "make test"); strings.Contains(r, "slot.") || strings.Contains(r, "without a slot") || !strings.Contains(r, "not admitted") {
		t.Fatalf("gate %q", r)
	}
}

func TestV163NoticesSpeakPercent(t *testing.T) {
	e, _, _ := bare(t)
	e.UseSlots(slots.New(2))
	a := bareThread(t, e)
	running(e, a)
	e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotGranted, Thread: a, Args: "test"})
	e.Apply(core.Input{Source: core.Observer(core.ObsHost), Kind: core.KindSlotOver, Thread: a, Args: core.SlotOverArgs{Class: "fyr: make test", Live: 80, Predicted: 50}})
	th, _ := e.Thread(a)
	var all string
	for _, n := range th.Feed.Queue {
		all += n.What + "\n"
	}
	if !strings.Contains(all, "Your test job is admitted now") || !strings.Contains(all, "uses ≈ 80 % of this host; Fyr expected ≈ 50 %") || strings.Contains(all, " slot ") {
		t.Fatalf("%s", all)
	}
}
