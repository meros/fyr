package engine

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

// V0.wake-once: a frozen job's pages stay charged to the agents while they
// are in RAM, so the wake rule takes the thread's memory now out of the
// agents' before it adds the job's memory at the freeze. Seen live on 19 Nov:
// "not waking … agents=12.8 GB + its 11.6 GB >= cap=12.8 GB" while the other
// agents held 1.2 GB.
func TestWakeOnceCountsTheFrozenJobOnce(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	e, u, now, z, th := fzEngine(t)
	a := th[0] // flagFor: 2.8 GB
	e.Slots.Acquire(th[1], resource.Test)
	resident := uint64(2_800_000_000)
	e.SlotMem = func(string) uint64 { return resident }
	u.flags = []resource.Undeclared{flagFor(a, *now)}
	z.free, z.stall, z.mem = 0.04, resource.StallCritical, 10e9 // the cap
	e.Tick()
	if !z.frozen[a] {
		t.Fatal("not frozen")
	}
	// Calm, the frozen job still in RAM: the agents' 9.5 GB holds its 2.8 GB,
	// so the others use 6.7 GB and 6.7 + 2.8 < 10.
	z.free, z.stall, z.mem = 0.2, resource.StallOK, 9.5e9
	e.Tick()
	tickFor(e, now, 2*guard.ThawAfterOK)
	if z.frozen[a] {
		t.Fatalf("not woken though the others and the job fit under the cap:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "fits: agents=6.7 GB + its 2.8 GB < cap=10.0 GB") {
		t.Fatalf("no wake line with the others' memory:\n%s", out.String())
	}
}
