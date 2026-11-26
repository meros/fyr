// Undeclared heavy work: the flags the guard reads.

package engine

import "github.com/meros/fyr/internal/guard/resource"

// flags are the threads that run undeclared heavy work now.
func (e *Engine) flags() []resource.Undeclared {
	if e.Undeclared == nil {
		return nil
	}
	return e.Undeclared()
}

// HasUndeclared says thread runs heavy work Fyr did not admit now
// (V0.freeze-controller): a repeat of its command is a class (heavy.Calls).
func (e *Engine) HasUndeclared(thread string) bool {
	for _, f := range e.flags() {
		if f.Thread == thread {
			return true
		}
	}
	return false
}
