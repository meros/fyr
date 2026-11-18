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

func TestDarwinAvailablePagesCountHalfOfTheProgramsOwn(t *testing.T) {
	if got := availablePages(10, 1, 2, 5, 22); got != 28 { // 10+1+2+5 + (22-2)/2
		t.Fatalf("%d", got)
	}
	if got := availablePages(0, 0, 9, 0, 4); got != 9 { // purgeable above the count
		t.Fatalf("%d", got)
	}
}

func TestDarwinQueueCountsTheThreadsThatCompute(t *testing.T) {
	out := `USER  PID TT %CPU STAT PRI STIME UTIME COMMAND
me    1   ??  99.0 R    31T 0:00.05 0:00.01 /bin/hog 99.0 R
      1       98.0 R    31T 0:00.05 0:00.01          98.0 R+
      1        0.4 R    31T 0:00.05 0:00.01           0.4 R
me    2   ??  50.0 S    31T 0:00.05 0:00.01 /bin/idle 50.0 S
me    3   ??  40.0 T    31T 0:00.05 0:00.01 /bin/stopped 40.0 T
`
	if v, ok := queueSlowdown(out, 2); !ok || v != 1.5 { // two compute: (2+1) ÷ 2
		t.Fatalf("%v %v", v, ok)
	}
	if v, ok := queueSlowdown(out, 8); !ok || v != 1 { // room to spare
		t.Fatalf("%v %v", v, ok)
	}
	if _, ok := queueSlowdown("ps: failed\n", 8); ok {
		t.Fatal("output without a reading parsed")
	}
}

func TestDarwinQueueReadsThisHost(t *testing.T) {
	if v, ok := hostQueue(t.Context()); !ok || v < 1 {
		t.Fatalf("%v %v", v, ok)
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
