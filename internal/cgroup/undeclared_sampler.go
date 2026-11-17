package cgroup

import (
	"github.com/meros/fyr/internal/slots"
)

// detect feeds one tick's readings to the detector (V0.164) and names the
// command behind each flagged thread. The probe and the self-deploy are never
// read.
func (s *Sampler) detect(l Load) {
	if s.Detect == nil {
		return
	}
	cfg := DefaultUndeclared
	if s.DetectConfig != nil {
		cfg = s.DetectConfig()
	}
	in := DetectInput{Capacity: l.Capacity, Config: cfg, Admitted: map[string]bool{}, Queued: map[string]bool{}}
	if s.Slots != nil {
		if s.Slots.Off() {
			in.Config.Off = true // heavy jobs are off on this host
		} else {
			sn := s.Slots.Snapshot()
			for _, h := range sn.Held {
				in.Admitted[h.Owner] = true
			}
			for _, h := range sn.Waiting {
				in.Queued[h.Owner] = true
			}
		}
	}
	if s.Tree != nil {
		for _, t := range l.Threads {
			if t.ID == "probe" || t.ID == DeployDir {
				continue
			}
			in.Reads = append(in.Reads, Reading{Thread: t.ID, CPU: t.CPU, Work: t.Working})
		}
	} else if s.Trees != nil && s.CLIs != nil {
		in.Reads = s.Trees.Read(s.now(), s.CLIs())
	}
	s.Detect.Observe(s.now(), in)
	if s.Tree == nil {
		return
	}
	proc := s.Proc
	if proc == "" {
		proc = "/proc"
	}
	names := map[string]string{}
	for _, f := range s.Detect.Flags() {
		if s.Project != nil {
			names[f.Thread] = s.Project(f.Thread)
		}
		if root, ok := Heaviest(ReadRoots(s.Tree, proc, f.Thread, f.FirstOver)); ok {
			s.Detect.SetCommand(f.Thread, CommandOf(root, s.Groups.Bashes(f.Thread)))
		}
	}
	s.names.Store(&names)
}

// Undeclared lists the threads that run heavy work Fyr did not admit, for the
// pool's snapshot and the engine (V0.164); a published view, nothing is read.
func (s *Sampler) Undeclared() []slots.Undeclared {
	if s.Detect == nil {
		return nil
	}
	var out []slots.Undeclared
	sig := s.Signals()
	for _, f := range s.Detect.Flags() {
		u := slots.Undeclared{Thread: f.Thread, Name: s.nameOf(f.Thread), CPU: f.CPU, Mem: f.Mem, Since: f.Since, Command: f.Command, Dismissed: f.Dismissed,
			Paused: s.Groups.Frozen(f.Thread), Queued: f.Queued, OverCPU: f.OverCPU, OverMem: f.OverMem, Window: f.Window, MemOver: f.MemOver}
		u.CPUPct, u.MemPct = func() (float64, float64) {
			c, m := slots.Need{CPU: f.CPU, Mem: f.Mem}.Shares(sig)
			return slots.Pct(c), slots.Pct(m)
		}()
		if !f.Told.IsZero() {
			t := f.Told
			u.Told = &t
		}
		if f.Command != "" {
			u.Matched = s.Detect.Matches != nil && s.Detect.Matches(f.Command)
		}
		out = append(out, u)
	}
	return out
}

// nameOf is what a thread is called in a wait reason: its project, else its id.
func (s *Sampler) nameOf(thread string) string {
	if p := s.names.Load(); p != nil && (*p)[thread] != "" {
		return (*p)[thread]
	}
	return thread
}
