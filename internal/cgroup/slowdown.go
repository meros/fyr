package cgroup

// Decision 0029 section 0 (c) (#185): the slowdown s, how much slower a new
// job runs now (1.0 = idle). A probe times a fixed CPU burst every few
// seconds (wall ÷ CPU time), on macOS raised to what the run queue says
// (hostQueue); s is the median of the last three. Where PSI
// exists the host learns its own relation of CPU pressure to the probe, so
// s is the larger of the probe and the pressure through that fit: PSI is
// smoother and sees stalls between probes, the probe has no ceiling.

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProbeEvery is how often the probe runs.
const ProbeEvery = 5 * time.Second

// probeFresh is how old the last probe may be and still count.
const probeFresh = 20 * time.Second

// fitBins split CPU pressure in tens of percent; fitKeep is each bin's
// memory and fitMin the samples a bin needs to count.
const (
	fitBins = 10
	fitKeep = 60
	fitMin  = 10
)

// Slowdown keeps the probe's last values and the host's fit. The zero value
// is ready; a nil *Slowdown reads nothing.
type Slowdown struct {
	mu   sync.Mutex
	last []float64
	at   time.Time
	fit  [fitBins][]float64
}

func bin(p float64) int {
	b := int(p / 10)
	return max(0, min(fitBins-1, b))
}

func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	return s[len(s)/2]
}

// Add records a probe value v taken at now, with the CPU pressure at that
// moment when the host has PSI.
func (s *Slowdown) Add(now time.Time, v float64, pressure *float64) {
	if s == nil || v <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = append(s.last, v)
	if len(s.last) > 3 {
		s.last = s.last[len(s.last)-3:]
	}
	s.at = now
	if pressure != nil {
		b := bin(*pressure)
		s.fit[b] = append(s.fit[b], v)
		if len(s.fit[b]) > fitKeep {
			s.fit[b] = s.fit[b][len(s.fit[b])-fitKeep:]
		}
	}
}

// Value is s at now: the median of the last three probes, raised to what
// the fit says the pressure means; nil without a fresh probe.
func (s *Slowdown) Value(now time.Time, pressure *float64) *float64 {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.last) == 0 || now.Sub(s.at) > probeFresh {
		return nil
	}
	v := median(s.last)
	if pressure != nil {
		if f := s.fit[bin(*pressure)]; len(f) >= fitMin {
			v = max(v, median(f))
		}
	}
	v = round2(v)
	return &v
}

// RunProbe keeps the probe going until ctx ends: start's process prints one
// value per line (a child where a new job would run, so it feels what a job
// feels); without start the burst runs in this process. V0.probe-scope: a
// child that cannot start, or ends without a value, leaves the burst running
// in this process until the next try, so s is never absent for it (decision
// 0029 section 0); the first such failure is logged.
// pressure reads the CPU pressure at each value.
func (s *Slowdown) RunProbe(ctx context.Context, start func(context.Context) (*exec.Cmd, func(), error), pressure func() *float64) {
	if s == nil || !CanBurst() {
		return
	}
	if start == nil {
		s.burst(ctx, pressure, nil)
		return
	}
	logged := false
	for ctx.Err() == nil {
		cmd, done, err := start(ctx)
		n := 0
		if err == nil {
			n, err = s.read(ctx, cmd, pressure)
			done()
		}
		if ctx.Err() != nil {
			return
		}
		if n == 0 {
			if !logged {
				if err == nil {
					err = errors.New("it gave no value")
				}
				log.Printf("load: the probe child: %v; the probe runs in fyrd until it starts", err)
				logged = true
			}
			s.burst(ctx, pressure, time.After(30*time.Second))
			continue
		}
		sleep(ctx, 30*time.Second) // the child ended: start it again later
	}
}

// burst runs the probe in this process until ctx ends or until.
func (s *Slowdown) burst(ctx context.Context, pressure func() *float64, until <-chan time.Time) {
	for {
		v, ok := Burst()
		if q, qok := hostQueue(ctx); qok {
			v, ok = max(v, q), true
		}
		if ok {
			s.Add(time.Now(), v, pressure())
		}
		select {
		case <-ctx.Done():
			return
		case <-until:
			return
		case <-time.After(ProbeEvery):
		}
	}
}

// read takes cmd's values until it ends; n is how many.
func (s *Slowdown) read(ctx context.Context, cmd *exec.Cmd, pressure func() *float64) (n int, err error) {
	out, err := cmd.StdoutPipe()
	if err != nil {
		return 0, err
	}
	var stderr strings.Builder // why systemd-run refused, for the log
	if cmd.Stderr == nil {
		cmd.Stderr = &stderr
	}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	go func() { <-ctx.Done(); cmd.Process.Kill() }()
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		if v, err := strconv.ParseFloat(strings.TrimSpace(sc.Text()), 64); err == nil {
			s.Add(time.Now(), v, pressure())
			n++
		}
	}
	if err := cmd.Wait(); err != nil {
		if e := strings.TrimSpace(stderr.String()); e != "" {
			return n, fmt.Errorf("%v: %.200s", err, e)
		}
		return n, err
	}
	return n, nil
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// ProbeCommand starts bin's `loadprobe` (one value per line) where a new job
// would run, at the agents' weight: agents/probe/ under t, and in the scope
// layout a scope of its own under the agents' slice, started like a thread's
// (V0.probe-scope: the slice is root's, so fyrd cannot mkdir in it). nil
// without a tree, so the probe runs in fyrd itself.
func ProbeCommand(t *Tree, bin string) func(context.Context) (*exec.Cmd, func(), error) {
	if t == nil || bin == "" {
		return nil
	}
	return func(ctx context.Context) (*exec.Cmd, func(), error) {
		if t.layout == LayoutScope {
			// fyr-agent-probe-…: a name the polkit rule allows and
			// scopeThread skips; a new one each start, as the last may
			// still be collected.
			t.mu.Lock()
			w := t.weightOf("probe", false)
			t.mu.Unlock()
			unit := fmt.Sprintf("%s%s%d-%d%s", scopePrefix, probePrefix, os.Getpid(), t.probeSeq.Add(1), scopeSuffix)
			p := append(t.sc.prefix(unit, w), bin, "loadprobe")
			cmd := exec.Command(p[0], p[1:]...)
			dieWithParent(cmd)
			return cmd, func() {}, nil
		}
		t.mu.Lock()
		dir, err := t.make("probe")
		t.mu.Unlock()
		if err != nil {
			return nil, nil, err
		}
		cmd := exec.Command(bin, "loadprobe")
		f, err := os.Open(dir)
		if err != nil {
			return nil, nil, err
		}
		intoCgroup(cmd, f)
		dieWithParent(cmd)
		return cmd, func() { f.Close() }, nil
	}
}

// CPUPressureNow is CPU pressure some avg10 now: agents/cpu.pressure with a
// tree, else /proc/pressure/cpu; nil without PSI.
func (s *Sampler) CPUPressureNow() *float64 {
	proc := s.Proc
	if proc == "" {
		proc = "/proc"
	}
	path := filepath.Join(proc, "pressure", "cpu")
	if s.Tree != nil {
		path = filepath.Join(s.Tree.agentsDir(), "cpu.pressure")
	}
	if p, ok := readPSI(path); ok {
		v := p.some
		return &v
	}
	return nil
}

// LoadProbe is `fyrd loadprobe`: a burst every ProbeEvery, its value on a
// line, until the output is closed.
func LoadProbe(w interface{ Write([]byte) (int, error) }) {
	for {
		if v, ok := Burst(); ok {
			if _, err := w.Write([]byte(strconv.FormatFloat(v, 'f', 3, 64) + "\n")); err != nil {
				return
			}
		}
		time.Sleep(ProbeEvery)
	}
}
