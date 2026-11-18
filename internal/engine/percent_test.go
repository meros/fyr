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
	p.Host = func() slots.Signals { return slots.Signals{Provider: "cgroups", CPU: 6, Memory: 13 << 30} }
	p.Cost = func(h slots.Holder) slots.Need { return slots.Need{CPU: 2.7, Mem: 2 << 30} }
	p.Admit = func(head slots.Holder, held []slots.Holder) (bool, string, string) {
		if len(held) == 0 {
			return true, "", ""
		}
		return false, "needs ≈ 45 % CPU, 10 % free", slots.ResCPU
	}
	e.UseSlots(p)
	e.HostName = "meros-laptop"
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	out, _ := callAs(t, e, a, "slot_acquire", map[string]any{"kind": "test"})
	if !strings.Contains(out, "Admitted: your test job ≈ 45 % CPU, 15 % memory of meros-laptop; 55 % CPU and 85 % memory left.") || strings.Contains(out, "slot.") {
		t.Fatalf("held %q", out)
	}
	out, _ = callAs(t, e, b, "slot_acquire", map[string]any{"kind": "e2e"})
	if !strings.Contains(out, "Queued: your e2e job needs 45 % CPU, 15 % memory of meros-laptop; 55 % CPU and 85 % memory free (0 ahead).") || !strings.Contains(out, "Waiting: needs ≈ 45 % CPU, 10 % free.") {
		t.Fatalf("queued %q", out)
	}
	out, _ = callAs(t, e, a, "slot_release", map[string]any{})
	if !strings.Contains(out, "Released:") || !strings.Contains(out, "% memory of meros-laptop free") {
		t.Fatalf("release %q", out)
	}
	e.Slots.Acquire(a, slots.Test)
	e.Slots.Acquire(b, slots.Test)
	if r := e.HeavyGate(HeavyCall{Thread: bareThread(t, e), Class: "make test", ToolUse: "tu"}); strings.Contains(r, "slot") || !strings.Contains(r, "no room") {
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
