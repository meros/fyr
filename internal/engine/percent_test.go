// V0.163: the gate's refusal and the notices speak a percentage of the host, not slots. (slot_acquire and
// slot_release, whose answers did too, went with V0.one-tool-vocabulary.)
package engine

import (
	"strings"
	"testing"

	"github.com/meros/fyr/internal/core"
	"github.com/meros/fyr/internal/guard/resource"
	"github.com/meros/fyr/internal/slots"
)

func TestV163GateSpeaksPercent(t *testing.T) {
	e, _, _ := bare(t)
	p := slots.New(2)
	p.Now = e.Now
	p.Host = func() resource.Signals { return resource.Signals{Provider: "cgroups", CPU: 6, Memory: 13 << 30} }
	p.Cost = func(h resource.Holder) resource.Need { return resource.Need{CPU: 2.7, Mem: 2 << 30} }
	p.Admit = func(head resource.Holder, held []resource.Holder) (bool, string, string) {
		if len(held) == 0 {
			return true, "", ""
		}
		return false, "needs ≈ 45 % CPU, 10 % free", slots.ResCPU
	}
	e.UseSlots(p)
	e.HostName = "home-laptop"
	a, b := bareThread(t, e), bareThread(t, e)
	running(e, a)
	running(e, b)
	e.Slots.Acquire(a, resource.Test)
	e.Slots.Acquire(b, resource.Test)
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
