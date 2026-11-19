package cgroup

import (
	"context"
	"path/filepath"
)

// V0.guard-wake (decision 0036 E5): the agents' cgroup is agents/ or the
// agents' slice (V0.agents-unit). The guard ticks every 10 s and reads
// avg10, and oomd adds its own 20 s on top. A PSI trigger on the agents'
// memory.pressure tells fyrd within one window that the agents stall, so the
// guard acts at once; the 10 s tick stays as the fallback.

// PressureTrigger is the trigger fyrd writes: 150 ms of full stall in a 2 s
// window. An unprivileged trigger needs a window that is a multiple of 2 s.
const PressureTrigger = "full 150000 2000000"

// WatchAgentsPressure calls wake each time the agents' memory stall crosses
// PressureTrigger, until ctx ends. It returns at once with an error where
// there is no tree or the kernel takes no trigger (not Linux, no PSI, an old
// kernel); the caller then relies on the tick.
func WatchAgentsPressure(ctx context.Context, t *Tree, wake func()) error {
	if t == nil {
		return errNoTree
	}
	return watchPressure(ctx, filepath.Join(t.agentsDir(), "memory.pressure"), PressureTrigger, wake)
}
