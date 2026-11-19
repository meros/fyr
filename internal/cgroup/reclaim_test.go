// V0.guard-swap: a full agents swap cap reads as full, and a freeze moves the
// frozen work's anonymous memory to swap within the room the cap leaves.
package cgroup

import (
	"path/filepath"
	"testing"

	"github.com/meros/fyr/internal/slots"
)

func TestGuardSwapFullAtNinetyPercentOfTheCap(t *testing.T) {
	d := t.TempDir()
	for _, c := range []struct {
		max, cur string
		full     bool
	}{
		{"6144M", "6143M", true},  // 19 Nov 14:12-14:37
		{"6144M", "5530M", true},  // 90 %
		{"6144M", "3000M", false}, // room left
		{"max", "20G", false},     // no cap: the device's own fullness is not the agents'
		{"0", "0", false},         // no swap allowed: nothing to fill
	} {
		write(t, filepath.Join(d, "memory.swap.max"), c.max)
		write(t, filepath.Join(d, "memory.swap.current"), c.cur)
		if got := swapFull(d); got != c.full {
			t.Errorf("max=%s current=%s: full=%v, want %v", c.max, c.cur, got, c.full)
		}
	}
}

func TestGuardSwapFullDoublesTheReserve(t *testing.T) {
	p := slots.Policy{MemoryReserve: 0.10}
	h := 0.12
	s := slots.Signals{Provider: "cgroups", Headroom: &h, CPU: 6, Memory: 12 << 30}
	if ok, _, _ := slots.DecideWith(p, s, slots.Factors{CPU: 1, Mem: 1}, 2, nil, nil, "", slots.Need{}); !ok {
		t.Fatal("12 % free with swap room: a job is admitted")
	}
	s.SwapFull = true
	ok, why, _ := slots.DecideWith(p, s, slots.Factors{CPU: 1, Mem: 1}, 2, nil, nil, "", slots.Need{})
	if ok || why != "memory: 12 % free (swap full)" {
		t.Fatalf("12 %% free with the swap full: ok=%v why=%q, want a wait that names the swap", ok, why)
	}
}

func TestGuardSwapReclaimMovesFrozenWorkWithinTheSwapRoom(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	agents := filepath.Dir(td)
	write(t, filepath.Join(agents, "memory.swap.max"), "10240M")
	write(t, filepath.Join(agents, "memory.swap.current"), "7168M") // room: 9216M - 7168M = 2048M
	ud := filepath.Join(td, undeclaredDir)
	u1, u2, u3 := filepath.Join(ud, "u100"), filepath.Join(ud, "u200"), filepath.Join(ud, "u300")
	for _, u := range []string{u1, u2, u3} {
		write(t, filepath.Join(u, "memory.stat"), "anon 3221225472\nfile 104857600\n") // 3 GiB anon
		write(t, filepath.Join(u, "memory.reclaim"), "")
	}
	write(t, filepath.Join(ud, "cgroup.freeze"), "0")
	write(t, filepath.Join(u1, "cgroup.freeze"), "0") // its turn: it runs
	write(t, filepath.Join(u2, "cgroup.freeze"), "1")
	write(t, filepath.Join(u3, "cgroup.freeze"), "1")
	g := &JobGroups{Tree: tr, Proc: t.TempDir()}

	g.reclaimFrozen("t_a")
	if got := read(t, filepath.Join(u1, "memory.reclaim")); got != "" {
		t.Fatalf("the running unit was reclaimed: %q", got)
	}
	if got := read(t, filepath.Join(u2, "memory.reclaim")); got != "2147483648 swappiness=200" {
		t.Fatalf("frozen unit: reclaim %q, want its anon within the 2 GiB of room, anon only", got)
	}
	// The fake cgroup's swap does not grow, so the room is the same for the
	// next unit; on a real one it shrinks with each reclaim.
	write(t, filepath.Join(agents, "memory.swap.current"), "9216M")
	write(t, filepath.Join(u3, "memory.reclaim"), "")
	g.reclaimFrozen("t_a")
	if got := read(t, filepath.Join(u3, "memory.reclaim")); got != "" {
		t.Fatalf("no swap room: nothing may be reclaimed, got %q", got)
	}
}

func TestGuardSwapReclaimNeedsTheKernelsFile(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	td := threadOf(tr, "t_a")
	ud := filepath.Join(td, undeclaredDir)
	write(t, filepath.Join(ud, "memory.stat"), "anon 3221225472\n")
	write(t, filepath.Join(ud, "cgroup.freeze"), "1")
	g := &JobGroups{Tree: tr, Proc: t.TempDir()}
	g.reclaimFrozen("t_a")
	if exists(filepath.Join(ud, "memory.reclaim")) {
		t.Fatal("a reclaim created memory.reclaim on a cgroupfs that has none")
	}
}
