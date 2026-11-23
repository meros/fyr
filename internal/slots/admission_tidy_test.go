// V0.admission-tidy: a thread that holds an admission and starts a heavier
// command is admitted again for the difference; a new job asks the queue's
// head before it backfills past it; the guard.AIMD controller reads the host's
// configured memory reserve.
package slots

import (
	"testing"
	"time"

	"github.com/meros/fyr/internal/guard"
	"github.com/meros/fyr/internal/guard/resource"
)

// V0.admission-tidy (1): the holder's own hold is not counted beside its
// heavier command; on refusal it waits as a new job does, keeps its hold
// meanwhile, and is granted in place.
func TestAdmissionTidyGrowAdmitsTheDifference(t *testing.T) {
	p := New(2)
	room := true
	var asked []string
	p.Admit = func(head resource.Holder, held []resource.Holder) (bool, string, string) {
		for _, h := range held {
			asked = append(asked, head.Owner+" beside "+h.Owner)
		}
		if head.Owner == "a" && !room {
			return false, "no room", resource.ResMem
		}
		return true, "", ""
	}
	p.Acquire("a", resource.Test)
	p.Acquire("b", resource.Test)
	asked = nil
	if held, _ := p.Grow("a", resource.E2E); !held {
		t.Fatal("a heavier command that fits waits")
	}
	for _, s := range asked {
		if s == "a beside a" {
			t.Fatalf("its own hold counted beside it: %v", asked)
		}
	}
	room = false
	held, ahead := p.Grow("a", resource.E2E)
	if held || ahead != 0 {
		t.Fatalf("a heavier command that does not fit runs: %v %d", held, ahead)
	}
	if _, ok := p.Holding("a"); !ok {
		t.Fatal("the running hold was lost")
	}
	s := p.Snapshot()
	if len(s.Waiting) != 1 || s.Waiting[0].Owner != "a" || len(s.Held) != 2 {
		t.Fatalf("%+v", s)
	}
	var told []string
	p.OnGrant = func(o, _ string) { told = append(told, o) }
	room = true
	p.Fill()
	s = p.Snapshot()
	if len(s.Waiting) != 0 || len(s.Held) != 2 || len(told) != 1 || told[0] != "a" {
		t.Fatalf("not granted in place: %+v told %v", s, told)
	}
	// A release ends both the hold and the wait.
	room = false
	p.Grow("a", resource.E2E)
	p.Release("a")
	if s := p.Snapshot(); len(s.Waiting) != 0 || len(s.Held) != 1 {
		t.Fatalf("after release %+v", s)
	}
}

// V0.admission-tidy (7): a new job behind a head that fits now does not
// backfill past it (and use one of its passes); the head goes first.
func TestAdmissionTidyAcquireAsksTheHeadFirst(t *testing.T) {
	p := New(4)
	fits := map[string]bool{"x": true, "c": true}
	p.Admit = func(head resource.Holder, _ []resource.Holder) (bool, string, string) {
		return fits[head.Owner], "", ""
	}
	p.Backfill = func(_, _ resource.Holder, _ []resource.Holder) bool { return true }
	p.Acquire("x", resource.Test)
	if held, _ := p.Acquire("head", resource.Test); held {
		t.Fatal("setup: the head was admitted")
	}
	fits["head"] = true // the host freed room; no Fill ran yet
	p.Acquire("c", resource.Test)
	if _, ok := p.Holding("head"); !ok {
		t.Fatalf("the head that fits still waits: %+v", p.Snapshot())
	}
	if p.passes != 0 {
		t.Fatalf("backfill passed a head that fits: %d passes", p.passes)
	}
}

// V0.admission-tidy (5): the controller reads the host's reserve, not the
// default: 15 % free is short where the host keeps 20 %.
func TestAdmissionTidyAIMDReadsTheConfiguredReserve(t *testing.T) {
	t0 := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	a := &guard.AIMD{N: 2, Policy: resource.Policy{SlowdownMax: 1.5, MemoryReserve: 0.2}}
	s := resource.Signals{Provider: "cgroups", Slowdown: f(1), Headroom: f(0.15), Stall: resource.StallOK}
	a.Step(t0, s, false, time.Time{}, "")
	if b, _ := a.Step(t0.Add(30*time.Second), s, false, time.Time{}, ""); b.Mem >= 1 {
		t.Fatalf("15 %% free under a 20 %% reserve did not cut: %+v", b)
	}
}

// V0.admission-tidy: two holders that both wait to grow would each count the
// other's hold and never be admitted; with every holder waiting to grow, the
// head is decided as with nothing held, and the next waits beside it.
func TestAdmissionTidyTwoGrowersDoNotBlockEachOther(t *testing.T) {
	p := New(2)
	p.Acquire("a", resource.Test) // by the count, before Admit is set
	p.Acquire("b", resource.Test)
	// A heavier job fits only with nothing else held.
	p.Admit = func(_ resource.Holder, held []resource.Holder) (bool, string, string) {
		return len(held) == 0, "needs more", resource.ResMem
	}
	if held, _ := p.Grow("a", resource.E2E); held {
		t.Fatal("setup: a grew beside b's ungrown hold")
	}
	var told []string
	p.OnGrant = func(o, _ string) { told = append(told, o) }
	if held, _ := p.Grow("b", resource.E2E); held {
		t.Fatal("b grew beside a's hold")
	}
	s := p.Snapshot()
	if len(s.Waiting) != 1 || s.Waiting[0].Owner != "b" || len(told) != 1 || told[0] != "a" {
		t.Fatalf("the oldest grower was not admitted: waiting %+v told %v", s.Waiting, told)
	}
	// The memory rules still hold: nothing is granted while memory is short.
	q := New(2)
	q.Acquire("a", resource.Test)
	q.Acquire("b", resource.Test)
	q.Admit = func(resource.Holder, []resource.Holder) (bool, string, string) {
		return false, "memory short", resource.ResHeadroom
	}
	q.Grow("a", resource.E2E)
	q.Grow("b", resource.E2E)
	q.Fill()
	if s := q.Snapshot(); len(s.Waiting) != 2 {
		t.Fatalf("granted past the memory rule: %+v", s.Waiting)
	}
}
