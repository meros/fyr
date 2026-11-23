// The parts of undeclared work that take turns (decision 0032 slice 2), as the
// guard reads them; internal/guard decides the turns.

package engine

import "github.com/meros/fyr/internal/guard/resource"

// workUnits are the parts of thread's undeclared work, in a stable order.
func (e *Engine) workUnits(thread string) []resource.WorkUnit {
	if e.UndeclaredUnits == nil || e.UndeclaredRotate == nil {
		return nil
	}
	return e.UndeclaredUnits(thread)
}

// majorFaults is what thread has read back so far, in bytes.
func (e *Engine) majorFaults(thread string) uint64 {
	if e.UndeclaredFaults == nil {
		return 0
	}
	return e.UndeclaredFaults(thread)
}
