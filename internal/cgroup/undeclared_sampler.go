package cgroup

import "github.com/meros/fyr/internal/guard/resource"

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
	for _, t := range l.Threads {
		if t.ID == "probe" || t.ID == DeployDir {
			continue
		}
		// V0.services-not-jobs: a service's memory is reserved, not undeclared work.
		work := t.Working - min(t.Working, s.Services.InCgroup(t.ID))
		in.Reads = append(in.Reads, Reading{Thread: t.ID, CPU: t.CPU, Work: work})
	}
	if s.Groups != nil {
		in.Gone = s.Groups.UndeclaredEmpty // V0.episode-frozen
	}
	s.Detect.Observe(s.now(), in)
	names := map[string]string{}
	for _, f := range s.Detect.Flags() {
		if s.Project != nil {
			names[f.Thread] = s.Project(f.Thread)
		}
		if root, ok := Heaviest(s.Roots(f)); ok {
			s.Detect.SetCommand(f.Thread, CommandOf(root, s.Groups.Bashes(f.Thread)))
		}
	}
	if s.Project != nil { // V0.services-not-jobs: a service's wait reason names its thread too
		for _, t := range l.Threads {
			if _, ok := names[t.ID]; !ok && len(s.Services.Of(t.ID)) > 0 {
				names[t.ID] = s.Project(t.ID)
			}
		}
	}
	s.names.Store(&names)
}

// FlagNow flags the thread whose working set grew most at once (Detector.FlagNow,
// V0.freeze-fast-class-name) and names its command; "" when none is over the
// memory threshold.
func (s *Sampler) FlagNow() string {
	if s.Detect == nil {
		return ""
	}
	th := s.Detect.FlagNow(s.now())
	if th == "" {
		return ""
	}
	for _, f := range s.Detect.Flags() {
		if f.Thread != th {
			continue
		}
		if root, ok := Heaviest(s.Roots(f)); ok {
			s.Detect.SetCommand(th, CommandOf(root, s.Groups.Bashes(th)))
		}
	}
	return th
}

// Roots are the job roots of flag f's episode: from its cgroup, else from the
// process tree under its CLI (V0.process-tree-hosts). An episode over its
// memory threshold also takes the older work that holds a quarter of its
// memory or more (V0.freeze-old-work), so a long job is what is named and
// frozen, not the newest small process.
func (s *Sampler) Roots(f Flag) []Root {
	var minOld uint64
	if f.MemOver {
		minOld = max(f.Mem/4, 1)
	}
	cli := 0
	if s.CLIs != nil {
		cli = s.CLIs()[f.Thread]
	}
	if s.Tree != nil {
		el := Elders{CLI: cli, Min: minOld}
		if s.Services != nil {
			el.Service = s.Services.IsService // V0.services-not-jobs
		}
		return ReadRootsWith(s.Tree, s.Proc, f.Thread, f.FirstOver, el)
	}
	if s.Trees == nil {
		return nil
	}
	t, ok := s.Trees.Table(s.now())
	if cli <= 0 || !ok {
		return nil
	}
	return t.roots(cli, f.FirstOver.Add(-rootGrace), minOld)
}

// Undeclared lists the threads that run heavy work Fyr did not admit, for the
// pool's snapshot and the engine (V0.164); a published view, nothing is read.
func (s *Sampler) Undeclared() []resource.Undeclared {
	if s.Detect == nil {
		return nil
	}
	var out []resource.Undeclared
	sig := s.Signals()
	for _, f := range s.Detect.Flags() {
		u := resource.Undeclared{Thread: f.Thread, Name: s.nameOf(f.Thread), CPU: f.CPU, Mem: f.Mem, Since: f.Since, Command: f.Command,
			Paused: s.Groups.Frozen(f.Thread), Queued: f.Queued, OverCPU: f.OverCPU, OverMem: f.OverMem, Window: f.Window, MemOver: f.MemOver}
		u.CPUPct, u.MemPct = func() (float64, float64) {
			c, m := resource.Need{CPU: f.CPU, Mem: f.Mem}.Shares(sig)
			return resource.Pct(c), resource.Pct(m)
		}()
		out = append(out, u)
	}
	return out
}

// ServiceExtras are the threads' running services' reservations for admission
// (V0.services-not-jobs), from the last tick.
func (s *Sampler) ServiceExtras() []resource.Extra {
	return s.Services.Extras(s.nameOf)
}

// nameOf is what a thread is called in a wait reason: its project, else its id.
func (s *Sampler) nameOf(thread string) string {
	if p := s.names.Load(); p != nil && (*p)[thread] != "" {
		return (*p)[thread]
	}
	return thread
}
