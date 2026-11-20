package cgroup

import (
	"encoding/binary"
	"testing"
	"time"
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

// V0.mac-swap-stall: the swap traffic is the Mac's memory stall.
func TestDarwinSwapTrafficIsTheStall(t *testing.T) {
	const mb = 1 << 20
	for _, c := range []struct {
		in, out float64
		want    Stall
	}{
		{15 * mb, 0, StallOK},          // calm: other programs read a little back
		{1 * mb, 190 * mb, StallHigh},  // a job spills into swap
		{290 * mb, 20 * mb, StallHigh}, // read back, little pushed out
		{250 * mb, 230 * mb, StallWorst},
	} {
		if got := swapStall(c.in, c.out); got != c.want {
			t.Fatalf("in %.0f out %.0f MB/s: %s, want %s", c.in/mb, c.out/mb, got, c.want)
		}
	}
	if worseStall(StallHigh, StallOK) != StallHigh || worseStall(StallOK, StallWorst) != StallWorst {
		t.Fatal("worse")
	}
}

func TestDarwinSwapMeterReadsRates(t *testing.T) {
	var in, out uint64
	m := &swapMeter{counts: func() (uint64, uint64, uint64, bool) { return in, out, 16384, true }}
	t0 := time.Unix(1000, 0)
	if r := m.read(t0); r != (swapRates{}) {
		t.Fatalf("first read %+v", r)
	}
	in, out = 6400, 64000 // 100 MB in, 1000 MB out over 10 s
	r := m.read(t0.Add(10 * time.Second))
	if r.in != 6400*16384/10 || r.out != 64000*16384/10 || !r.outRecent {
		t.Fatalf("%+v", r)
	}
	if r2 := m.read(t0.Add(12 * time.Second)); r2 != r { // within the window: the last rates
		t.Fatalf("%+v, want %+v", r2, r)
	}
	r = m.read(t0.Add(20 * time.Second)) // no traffic since
	if r.in != 0 || r.out != 0 || !r.outRecent {
		t.Fatalf("quiet %+v", r)
	}
	if r = m.read(t0.Add(75 * time.Second)); r.outRecent {
		t.Fatalf("a minute later %+v", r)
	}
	in = 0 // a counter that went back starts again
	if r = m.read(t0.Add(80 * time.Second)); r.in != 0 || r.out != 0 {
		t.Fatalf("reset %+v", r)
	}
}

func TestDarwinSwapMeterReadsThisHost(t *testing.T) {
	if _, _, _, ok := hostSwap.counts(); !ok {
		t.Fatal("no swap counters")
	}
}
