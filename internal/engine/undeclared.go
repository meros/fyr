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
