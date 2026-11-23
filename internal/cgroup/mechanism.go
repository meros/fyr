package cgroup

import "github.com/meros/fyr/internal/guard/resource"

// Mechanism is internal/guard's Mechanism on this host (V0.guard-mechanism):
// the job groups freeze, thaw and kill, the sampler reads the signals and
// finds the roots of a thread's undeclared work, and the detector keeps which
// threads it flagged. On a host without the cgroup tree the job groups stop
// and resume process trees (their Trees), so one Mechanism serves both.
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
