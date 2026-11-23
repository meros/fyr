package cgroup

import "github.com/meros/fyr/internal/guard/resource"

// Mechanism is internal/guard's Mechanism on a host with the cgroup tree
// (V0.guard-mechanism): the job groups freeze, thaw and kill, the sampler
// reads the signals and finds the roots of a thread's undeclared work, and the
// detector keeps which threads it flagged. A host without the tree has
// ProcMechanism (V0.guard-finish).
type Mechanism struct {
	Groups   *JobGroups
	Sampler  *Sampler
	Detector *Detector
}

// Sample reads the host's signals now.
func (m *Mechanism) Sample() resource.Signals { return m.Sampler.Signals() }

// Freeze stops thread's admitted job.
func (m *Mechanism) Freeze(thread string) bool { return m.Groups.Freeze(thread) }

// Thaw resumes thread's admitted job.
func (m *Mechanism) Thaw(thread string) { m.Groups.Thaw(thread) }

// FreezeUndeclared stops thread's flagged undeclared work, from the roots the
// sampler finds for its flag, and marks it paused in the detector.
func (m *Mechanism) FreezeUndeclared(thread string) bool {
	for _, f := range m.Detector.Flags() {
		if f.Thread == thread && m.Groups.FreezeUndeclared(thread, m.Sampler.Roots(f)) {
			m.Detector.MarkPaused(thread)
			return true
		}
	}
	return false
}

// ThawUndeclared resumes thread's undeclared work; its calm counts again
// (V0.episode-frozen).
func (m *Mechanism) ThawUndeclared(thread string) {
	m.Groups.ThawUndeclared(thread)
	m.Detector.MarkThawed(thread)
}

// Kill kills thread's job, or its undeclared work.
func (m *Mechanism) Kill(thread string, undeclared bool) int {
	return m.Groups.KillJob(thread, undeclared)
}

// ProcMechanism is internal/guard's Mechanism on a host without the cgroup
// tree, macOS and Linux without cgroups (V0.process-tree-hosts,
// V0.guard-finish): ProcStops stops and resumes process trees with SIGSTOP
// and SIGCONT; the sampler and the detector serve as for Mechanism.
type ProcMechanism struct {
	Stops    *ProcStops
	Sampler  *Sampler
	Detector *Detector
}

// Sample reads the host's signals now.
func (m *ProcMechanism) Sample() resource.Signals { return m.Sampler.Signals() }

// Freeze stops thread's admitted job.
func (m *ProcMechanism) Freeze(thread string) bool { return m.Stops.Freeze(thread) }

// Thaw resumes thread's admitted job.
func (m *ProcMechanism) Thaw(thread string) { m.Stops.Thaw(thread) }

// FreezeUndeclared stops thread's flagged undeclared work, from the roots the
// sampler finds for its flag, and marks it paused in the detector.
func (m *ProcMechanism) FreezeUndeclared(thread string) bool {
	for _, f := range m.Detector.Flags() {
		if f.Thread == thread && m.Stops.FreezeUndeclared(thread, m.Sampler.Roots(f)) {
			m.Detector.MarkPaused(thread)
			return true
		}
	}
	return false
}

// ThawUndeclared resumes thread's undeclared work; its calm counts again
// (V0.episode-frozen).
func (m *ProcMechanism) ThawUndeclared(thread string) {
	m.Stops.ThawUndeclared(thread)
	m.Detector.MarkThawed(thread)
}

// Kill kills what was stopped of thread's job, or of its undeclared work.
func (m *ProcMechanism) Kill(thread string, undeclared bool) int {
	return m.Stops.KillJob(thread, undeclared)
}

// flag is thread's flag of undeclared work, false when it has none.
func flagOf(d *Detector, thread string) (Flag, bool) {
	for _, f := range d.Flags() {
		if f.Thread == thread {
			return f, true
		}
	}
	return Flag{}, false
}

// rootsMem is what the roots the sampler finds for thread's flag hold.
func rootsMem(s *Sampler, d *Detector, thread string) (uint64, bool) {
	f, ok := flagOf(d, thread)
	if !ok {
		return 0, false
	}
	var m uint64
	for _, r := range s.Roots(f) {
		m += r.RSS
	}
	return m, true
}

// RootsMem is what the processes of thread's flagged work hold (V0.guard-tidy:
// against its flagged memory).
func (m *Mechanism) RootsMem(thread string) (uint64, bool) {
	return rootsMem(m.Sampler, m.Detector, thread)
}

// Units are the parts of thread's flagged work (decision 0032 slice 2), each
// named by its command.
func (m *Mechanism) Units(thread string) []resource.WorkUnit {
	f, ok := flagOf(m.Detector, thread)
	if !ok {
		return nil
	}
	var out []resource.WorkUnit
	for _, u := range m.Groups.Units(thread, m.Sampler.Roots(f)) {
		out = append(out, resource.WorkUnit{PID: u.PID, RSS: u.RSS, Command: CommandOf(Root{PID: u.PID, Cmdline: u.Cmdline}, m.Groups.Bashes(thread))})
	}
	return out
}

// Rotate lets the part with pid active run and freezes the others, and marks
// the work paused in the detector.
func (m *Mechanism) Rotate(thread string, units []resource.WorkUnit, active int) bool {
	us := make([]Unit, 0, len(units))
	for _, u := range units {
		us = append(us, Unit{PID: u.PID, RSS: u.RSS})
	}
	if !m.Groups.RotateUndeclared(thread, us, active) {
		return false
	}
	m.Detector.MarkPaused(thread)
	return true
}

// Faults is thread's read-back counter.
func (m *Mechanism) Faults(thread string) uint64 { return m.Groups.MajorFaultBytes(thread) }

// RootsMem is what the processes of thread's flagged work hold.
func (m *ProcMechanism) RootsMem(thread string) (uint64, bool) {
	return rootsMem(m.Sampler, m.Detector, thread)
}

// Units: without the cgroup tree the work is not split into parts.
func (m *ProcMechanism) Units(string) []resource.WorkUnit { return nil }

// Rotate: without the cgroup tree the parts take no turns.
func (m *ProcMechanism) Rotate(string, []resource.WorkUnit, int) bool { return false }

// Faults: the process table keeps no read-back counter.
func (m *ProcMechanism) Faults(string) uint64 { return 0 }
