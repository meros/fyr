// Package guard is the resource guard's owner (V0.guard-mechanism): the
// policy that admits, freezes, thaws and kills heavy work, over a Mechanism
// that does it on one host. V0.guard-tick moved the policy here: admission
// (Admission, DecideWith, EASY), the budget controller (AIMD), and the memory
// guard's tick (Tick), which makes every decision of a tick from its inputs
// and an explicit State. internal/slots keeps the queue, and the engine
// gathers the inputs and applies the decisions.
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

// Parts is what the guard reads of a thread's undeclared work on one host,
// and how its parts take turns (decision 0032 slice 2, V0.guard-finish): the
// tick's View answers from it, and Rotate applies a Rotate decision. Both of
// internal/cgroup's mechanisms implement it; without the cgroup tree there
// are no parts and no turns.
type Parts interface {
	// RootsMem is what the processes of thread's flagged work hold, false
	// when thread is not flagged (V0.guard-tidy).
	RootsMem(thread string) (uint64, bool)
	// Units are the parts of thread's flagged work, each with its command.
	Units(thread string) []resource.WorkUnit
	// Rotate lets the part with pid active run and freezes the others; false
	// when it did not take.
	Rotate(thread string, units []resource.WorkUnit, active int) bool
	// Faults is thread's read-back counter (major faults, in bytes).
	Faults(thread string) uint64
}
