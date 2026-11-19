// V0.164 section 12.1: undeclared heavy work is seen from a thread's own cgroup readings. The detector is fed one
// reading per thread every 5 s by a fake clock; nothing here touches a real cgroup.
package cgroup

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const gb = 1e9

// desk is the 4-core, 14 GB host of the spec: thresholds 1.0 core and 1.12 GB, window 45 s.
var desk = Capacity{CPU: 4, Memory: 14e9}

type sim struct {
	t    *testing.T
	d    *Detector
	now  time.Time
	cap  Capacity
	cfg  UndeclaredConfig
	adm  map[string]bool
	gone map[string]bool // V0.episode-frozen: frozen work whose processes ended
	ends []Episode
}

func newSim(t *testing.T) *sim {
	s := &sim{t: t, d: &Detector{}, now: time.Date(2026, 11, 17, 14, 0, 0, 0, time.UTC), cap: desk, cfg: DefaultUndeclared, adm: map[string]bool{}, gone: map[string]bool{}}
	s.d.OnEnd = func(e Episode) { s.ends = append(s.ends, e) }
	return s
}

// tick feeds one reading per 5 s for secs seconds: the thread's cores and working set.
func (s *sim) run(thread string, secs int, cpu float64, work float64) {
	s.t.Helper()
	for i := 0; i < secs/5; i++ {
		s.now = s.now.Add(5 * time.Second)
		c := cpu
		s.d.Observe(s.now, DetectInput{Capacity: &s.cap, Config: s.cfg, Reads: []Reading{{Thread: thread, CPU: &c, Work: uint64(work)}}, Admitted: s.adm,
			Gone: func(th string) bool { return s.gone[th] }})
	}
}

// seed is the first reading of a thread: no CPU yet.
func (s *sim) seed(thread string, work float64) {
	s.d.Observe(s.now, DetectInput{Capacity: &s.cap, Config: s.cfg, Reads: []Reading{{Thread: thread, Work: uint64(work)}}, Admitted: s.adm})
}

func (s *sim) flag(thread string) *Flag {
	for _, f := range s.d.Flags() {
		f := f
		if f.Thread == thread {
			return &f
		}
	}
	return nil
}

func TestD1BaselineIsNeverFlagged(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	for i := 0; i < 120; i++ {
		s.run("t_a", 5, 0.03, 0.9*gb)
		if s.flag("t_a") != nil {
			t.Fatalf("flagged at %v", s.now)
		}
	}
	if got := s.d.Idle("t_a"); got.Mem != uint64(0.9*gb) {
		t.Fatalf("idle level %+v", got)
	}
}

func TestD2TwoCoresFlagsWhenTheWindowIsFull(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 40, 2, 0.9*gb)
	if s.flag("t_a") != nil {
		t.Fatal("flagged at 40 s")
	}
	s.run("t_a", 5, 2, 0.9*gb)
	f := s.flag("t_a")
	if f == nil {
		t.Fatal("not flagged at 45 s")
	}
	if f.CPU < 1.9 || f.CPU > 2.0 || !f.OverCPU || f.OverMem {
		t.Fatalf("use %+v", f)
	}
}

func TestD3AShortSpikeNeverFlags(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 20, 4, 0.9*gb)
	for i := 0; i < 20; i++ {
		s.run("t_a", 5, 0.03, 0.9*gb)
		if s.flag("t_a") != nil {
			t.Fatalf("flagged at %v", s.now)
		}
	}
}

func TestD4OneSlowReadingInsideAPartHolds(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 20, 2, 0.9*gb)
	s.run("t_a", 5, 0, 0.9*gb)
	s.run("t_a", 20, 2, 0.9*gb)
	if s.flag("t_a") == nil {
		t.Fatal("a single 5 s dip broke the part")
	}
}

func TestD5MemoryHeldFlagsOnMemoryOnly(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 40, 0.1, 2.4*gb)
	if s.flag("t_a") != nil {
		t.Fatal("flagged at 40 s")
	}
	s.run("t_a", 5, 0.1, 2.4*gb)
	f := s.flag("t_a")
	if f == nil || !f.OverMem || f.OverCPU {
		t.Fatalf("%+v", f)
	}
	if f.Mem < uint64(1.4*gb) || f.Mem > uint64(1.6*gb) {
		t.Fatalf("use %d", f.Mem)
	}
}

func TestD6AMemoryPeakThatFallsBackNeverFlags(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 20, 0.1, 3.9*gb)
	for i := 0; i < 20; i++ {
		s.run("t_a", 5, 0.03, 0.9*gb)
		if s.flag("t_a") != nil {
			t.Fatalf("flagged at %v", s.now)
		}
	}
}

func TestD7PageCacheIsNotTheWorkingSet(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	agents := filepath.Join(o.FS, unit, "agents")
	td := threadOf(tr, "t_a")
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:   20000000 kB\n")
	write(t, filepath.Join(td, "memory.current"), fmt.Sprint(uint64(4.9*gb)))
	write(t, filepath.Join(td, "memory.stat"), "anon 900000000\ninactive_file 3800000000\n")
	write(t, filepath.Join(agents, "memory.current"), fmt.Sprint(uint64(4.9*gb)))
	l := (&Sampler{Tree: tr, Proc: proc, Cores: 4}).get()
	for _, th := range l.Threads {
		if th.ID == "t_a" {
			if th.Memory != uint64(4.9*gb) || th.Working != uint64(1.1*gb) {
				t.Fatalf("raw %d working %d", th.Memory, th.Working)
			}
			return
		}
	}
	t.Fatal("no thread")
}

func TestD8ThresholdsAreSharesOfTheHost(t *testing.T) {
	s := newSim(t)
	s.cap = Capacity{CPU: 16, Memory: 64e9}
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 90, 2, 0.9*gb)
	if s.flag("t_a") != nil {
		t.Fatal("2 cores flagged a 16-core host (threshold 4)")
	}
	s.run("t_a", 50, 5, 0.9*gb)
	if s.flag("t_a") == nil {
		t.Fatal("5 cores not flagged")
	}
}

func TestD9Settings(t *testing.T) {
	s := newSim(t)
	s.cfg.CPU = 0.5 // 2 cores
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 90, 1.5, 0.9*gb)
	if s.flag("t_a") != nil {
		t.Fatal("1.5 cores flagged at threshold 2")
	}
	s = newSim(t)
	s.cfg.After = 30 * time.Second
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 30, 2, 0.9*gb)
	if s.flag("t_a") == nil {
		t.Fatal("undeclaredAfter 30 did not flag at 30 s")
	}
	s = newSim(t)
	s.cfg.Off = true
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 120, 4, 6*gb)
	if s.flag("t_a") != nil || len(s.d.Flags()) != 0 {
		t.Fatal("off still flags")
	}
}

func TestD10AnAdmittedThreadIsNeverFlaggedNorLearnedFrom(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.adm["t_a"] = true
	s.run("t_a", 300, 3, 3*gb)
	if s.flag("t_a") != nil {
		t.Fatal("an admitted thread was flagged")
	}
	if got := s.d.Idle("t_a"); got.Mem != uint64(0.9*gb) {
		t.Fatalf("a running job entered the idle level: %+v", got)
	}
}

func TestD11ReleasedButStillRunningFlagsAWindowLater(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.adm["t_a"] = true
	s.run("t_a", 120, 2, 0.9*gb)
	delete(s.adm, "t_a")
	s.run("t_a", 40, 2, 0.9*gb)
	if s.flag("t_a") != nil {
		t.Fatal("flagged before the window after the release")
	}
	s.run("t_a", 5, 2, 0.9*gb)
	if s.flag("t_a") == nil {
		t.Fatal("not flagged W after the release")
	}
}

func TestD12HysteresisKeepsTheEpisode(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 60, 2, 0.9*gb)
	since := s.flag("t_a").Since
	s.run("t_a", 120, 0.6, 0.9*gb)
	if f := s.flag("t_a"); f == nil || !f.Since.Equal(since) {
		t.Fatalf("the episode did not hold at 0.6 cores: %+v", f)
	}
	s.run("t_a", 40, 0.2, 0.9*gb)
	if s.flag("t_a") == nil {
		t.Fatal("ended before W of calm")
	}
	s.run("t_a", 10, 0.2, 0.9*gb)
	if s.flag("t_a") != nil {
		t.Fatal("not ended after W under half the threshold")
	}
	if len(s.ends) != 1 || !s.ends[0].Start.Equal(since) {
		t.Fatalf("episode record %+v", s.ends)
	}
}

func TestD13ALongJobIsOneEpisodeAndLeavesTheIdleLevelAlone(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 30*60, 2, 1.5*gb)
	if len(s.ends) != 0 || s.flag("t_a") == nil {
		t.Fatalf("episodes %d", len(s.ends))
	}
	s.run("t_a", 120, 0.03, 0.9*gb)
	if got := s.d.Idle("t_a"); got.Mem != uint64(0.9*gb) || got.CPU > 0.1 {
		t.Fatalf("idle level %+v", got)
	}
	if len(s.ends) != 1 {
		t.Fatalf("episodes %d", len(s.ends))
	}
}

func TestD14AGrowingCLIMovesTheIdleLevel(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.8*gb)
	w := 0.8 * gb
	for i := 0; i < 5; i++ {
		w += 0.2 * gb
		s.run("t_a", 300, 0.03, w)
		if s.flag("t_a") != nil {
			t.Fatalf("flagged at %.1f GB", w/gb)
		}
	}
	if got := s.d.Idle("t_a"); float64(got.Mem) < 1.5*gb {
		t.Fatalf("idle level %.2f GB did not follow", float64(got.Mem)/gb)
	}
}

func TestD15ARestartMidJobIsNotFlaggedFalsely(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 3.8*gb)
	s.run("t_a", 300, 0.2, 3.8*gb)
	if s.flag("t_a") != nil {
		t.Fatal("flagged")
	}
	if got := s.d.Idle("t_a"); got.Mem != uint64(3.8*gb) {
		t.Fatalf("idle %+v", got)
	}
}

func TestD16TheProbeAndTheDeployAreNeverRead(t *testing.T) {
	o, _ := fakeFS(t, "0::/"+unit+"\n")
	tr := Setup(o)
	agents := filepath.Join(o.FS, unit, "agents")
	for _, n := range []string{"probe", "self-deploy", "t_a"} {
		write(t, filepath.Join(agents, n, "cpu.stat"), "usage_usec 1\n")
		write(t, filepath.Join(agents, n, "memory.current"), "1000000000")
	}
	now := time.Date(2026, 11, 17, 14, 0, 0, 0, time.UTC)
	proc := t.TempDir()
	write(t, filepath.Join(proc, "meminfo"), "MemTotal:       32000000 kB\nMemAvailable:   20000000 kB\n")
	s := &Sampler{Tree: tr, Proc: proc, Cores: 4, Now: func() time.Time { return now }, Detect: &Detector{}}
	for i := 0; i < 40; i++ {
		now = now.Add(5 * time.Second)
		for _, n := range []string{"probe", "self-deploy", "t_a"} {
			u := uint64(i+1) * 5 * 4_000_000 // 4 cores
			os.WriteFile(filepath.Join(agents, n, "cpu.stat"), []byte(fmt.Sprintf("usage_usec %d\n", u)), 0o644)
		}
		s.Tick()
	}
	var got []string
	for _, f := range s.Detect.Flags() {
		got = append(got, f.Thread)
	}
	if len(got) != 1 || got[0] != "t_a" {
		t.Fatalf("flagged %v: probe and self-deploy are never read", got)
	}
}

func TestD17AFlatBaselineOfServersIsNeverFlagged(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 2.4*gb)
	s.run("t_a", 30*60, 0.05, 2.4*gb)
	if s.flag("t_a") != nil || len(s.ends) != 0 {
		t.Fatal("a baseline was flagged")
	}
}

func TestEpisodeOverWhenTheThreadIsAdmitted(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 60, 2, 0.9*gb)
	s.adm["t_a"] = true
	s.run("t_a", 5, 2, 0.9*gb)
	if s.flag("t_a") != nil || len(s.ends) != 1 {
		t.Fatalf("an admitted thread's episode goes on: %+v", s.ends)
	}
}

// V0.episode-frozen: frozen work uses no CPU and the guard pushes its memory
// to swap, so a frozen thread looks calm; that calm never ends its episode
// (it would thaw the work and restart the guard's count of freezes).
func TestEpisodeFrozenCalmDoesNotEnd(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 60, 2, 2.5*gb)
	since := s.flag("t_a").Since
	s.d.MarkPaused("t_a")
	s.run("t_a", 10*60, 0, 0.9*gb) // frozen, swapped out
	if f := s.flag("t_a"); f == nil || !f.Since.Equal(since) || len(s.ends) != 0 {
		t.Fatalf("a frozen episode ended by calm: flag %+v ends %+v", f, s.ends)
	}
}

// V0.episode-frozen: once the guard thaws the work, calm for W ends the
// episode, counted from the thaw.
func TestEpisodeFrozenEndsAWindowAfterTheThaw(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 60, 2, 2.5*gb)
	s.d.MarkPaused("t_a")
	s.run("t_a", 5*60, 0, 0.9*gb)
	s.d.MarkThawed("t_a")
	s.run("t_a", 40, 0.1, 0.9*gb)
	if s.flag("t_a") == nil {
		t.Fatal("ended before W of calm after the thaw")
	}
	s.run("t_a", 10, 0.1, 0.9*gb)
	if s.flag("t_a") != nil || len(s.ends) != 1 || !s.ends[0].Paused {
		t.Fatalf("not ended W after the thaw: ends %+v", s.ends)
	}
}

// V0.episode-frozen: frozen work whose processes are gone ends its episode.
func TestEpisodeFrozenEndsWhenTheWorkIsGone(t *testing.T) {
	s := newSim(t)
	s.seed("t_a", 0.9*gb)
	s.run("t_a", 60, 2, 2.5*gb)
	s.d.MarkPaused("t_a")
	s.run("t_a", 60, 0, 0.9*gb)
	if s.flag("t_a") == nil {
		t.Fatal("ended while frozen")
	}
	s.gone["t_a"] = true
	s.run("t_a", 5, 0, 0.9*gb)
	if s.flag("t_a") != nil || len(s.ends) != 1 || !s.ends[0].Paused {
		t.Fatalf("a frozen episode whose work is gone goes on: ends %+v", s.ends)
	}
}
