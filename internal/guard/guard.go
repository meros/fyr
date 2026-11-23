// Package guard is the resource guard's owner (V0.guard-mechanism): the
// policy that admits, freezes, thaws and kills heavy work, over a Mechanism
// that does it on one host. This first slice holds the Mechanism; the policy
// still lives in internal/slots, internal/admission and the engine's
// freezeGuard, and moves here in the slices V0.guard-mechanism (4) lists.
//
// The policy never names a mechanism's package: internal/cgroup implements
// Mechanism with cgroup v2 where the host has the tree, and with the process
// table (SIGSTOP and SIGCONT on each thread's process tree) on macOS and on
// Linux without cgroups. Both speak internal/guard/resource.
package guard

import "github.com/meros/fyr/internal/guard/resource"

// Mechanism is what the guard's policy asks of a host: read its signals, and
// freeze, thaw or kill a thread's work. A thread's work is its admitted job,
// or (undeclared) the heavy work it runs without one (V0.164).
type Mechanism interface {
	// Sample reads the host's signals now (V0.142).
	Sample() resource.Signals
	// Freeze stops a thread's admitted job, false when nothing was stopped;
	// Thaw resumes it (decision 0029 S5).
	Freeze(thread string) bool
	Thaw(thread string)
	// FreezeUndeclared stops the heavy work a thread runs without an
	// admission, false when there was none to stop; ThawUndeclared resumes it
	// (decision 0032).
	FreezeUndeclared(thread string) bool
	ThawUndeclared(thread string)
	// Kill kills a thread's job, or its undeclared work, and says how many
	// processes it killed (V0.rerun-classes, decision 0036 (F)).
	Kill(thread string, undeclared bool) int
}
