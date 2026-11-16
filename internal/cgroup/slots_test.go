// SPEC 0.5 V0.86: GET /api/load carries the host's slots, read fresh,
// with cgroups or without.
package cgroup

import (
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/slots"
)

func TestV086LoadShowsSlots(t *testing.T) {
	proc := t.TempDir()
	write(t, filepath.Join(proc, "loadavg"), "1.00 2.00 3.00 1/10 1\n")
	p := slots.New(1)
	s := &Sampler{Proc: proc, Cores: 4, Slots: p}
	p.Acquire("t_a", slots.Test)
	l := s.Get()
	if l.Slots == nil || l.Slots.N != 1 || len(l.Slots.Held) != 1 || l.Slots.Held[0].Owner != "t_a" {
		t.Fatalf("%+v", l.Slots)
	}
	p.Acquire("t_b", slots.Build)
	if l := s.Get(); len(l.Slots.Waiting) != 1 || l.Slots.Waiting[0].Kind != slots.Build {
		t.Fatalf("not fresh within the 2 s cache: %+v", l.Slots)
	}
	if l := (&Sampler{Proc: proc}).Get(); l.Slots != nil {
		t.Fatalf("slots without a pool: %+v", l.Slots)
	}
	if l := (&Sampler{Proc: proc, Slots: slots.New(0)}).Get(); l.Slots != nil {
		t.Fatalf("slots while off: %+v", l.Slots)
	}
}
