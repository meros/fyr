package engine

import (
	"slices"
	"testing"
)

// V0.forwarded-ports: a thread's ports come from the host's process table,
// outside the engine's lock; a host that cannot tell has none.
func TestThreadPortsAsksTheHostOutsideTheLock(t *testing.T) {
	e, _, _ := bare(t)
	if got := e.ThreadPorts("t_a"); got == nil || len(got) != 0 {
		t.Fatalf("without a reader: %v", got)
	}
	e.LeftoverDeps.ThreadPorts = func(thread string) []int {
		if thread == "t_a" {
			return []int{3000, 5173}
		}
		return nil
	}
	guardDeps(t, e)
	if got := e.ThreadPorts("t_a"); !slices.Equal(got, []int{3000, 5173}) {
		t.Fatalf("t_a: %v", got)
	}
	if got := e.ThreadPorts("t_b"); got == nil || len(got) != 0 {
		t.Fatalf("a thread without servers: %v", got)
	}
}
