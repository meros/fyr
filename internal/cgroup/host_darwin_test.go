package cgroup

import (
	"encoding/binary"
	"testing"
)

func TestDarwinLoadavgParse(t *testing.T) {
	b := make([]byte, 24)
	for i, v := range []uint32{2048, 3072, 512} {
		binary.LittleEndian.PutUint32(b[i*4:], v)
	}
	binary.LittleEndian.PutUint64(b[16:], 2048)
	if l, ok := parseLoadavg(b); !ok || l != [3]float64{1, 1.5, 0.25} {
		t.Fatalf("%v %v", l, ok)
	}
	if _, ok := parseLoadavg(b[:12]); ok {
		t.Fatal("a short value parsed")
	}
}

func TestDarwinPressureLevel(t *testing.T) {
	for lvl, want := range map[uint32]Stall{0: StallOK, 1: StallOK, 2: StallHigh, 4: StallWorst} {
		if got := pressureLevel(lvl); got != want {
			t.Fatalf("level %d: %s, want %s", lvl, got, want)
		}
	}
}

// Without /proc the Mac's sampler reads sysctl: a provider, a capacity of
// a share of the RAM, a headroom and a stall level, and the load average.
func TestDarwinSamplerReadsSysctl(t *testing.T) {
	l := (&Sampler{Proc: t.TempDir(), Cores: 8}).Get()
	if l.Provider != "sysctl" || l.Capacity == nil || l.Capacity.CPU != 8 || l.Capacity.Memory == 0 {
		t.Fatalf("provider %q capacity %+v", l.Provider, l.Capacity)
	}
	if l.Headroom == nil || *l.Headroom <= 0 || *l.Headroom > 1 || l.MemoryStall == "" {
		t.Fatalf("headroom %v stall %q", l.Headroom, l.MemoryStall)
	}
	if l.Load[0] <= 0 && l.Load[1] <= 0 && l.Load[2] <= 0 {
		t.Fatalf("load %v", l.Load)
	}
	if !CanBurst() {
		t.Fatal("no per-thread CPU clock")
	}
	if v, ok := Burst(); !ok || v < 1 {
		t.Fatalf("burst %v %v", v, ok)
	}
}
