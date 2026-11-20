package cgroup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"
)

// V0.guard-wake (decision 0036 E5): the agents' cgroup is agents/ or the
// agents' slice (V0.agents-unit). The guard ticks every 10 s and reads
// avg10, and oomd adds its own 20 s on top. A PSI trigger on the agents'
// memory.pressure tells fyrd within one window that the agents stall, so the
// guard acts at once; the 10 s tick stays as the fallback.

// PressureTrigger is the trigger fyrd writes: 200 ms of full stall in a 2 s
// window, 10 %, the critical level (StallCritical), so a trigger and
// "critical" mean the same (V0.freeze-fast-class-name; it was 150 ms). An
// unprivileged trigger needs a window that is a multiple of 2 s.
const PressureTrigger = "full 200000 2000000"

// PressureRetry is how often fyrd tries the trigger again after the open was
// refused (V0.psi-trigger-slice): the slice's memory.pressure is root's until
// the module hands it to the service user, and the slice may appear after
// fyrd. One open a minute costs nothing.
var PressureRetry = time.Minute

// watchFile is watchPressure; a test replaces it.
var watchFile = watchPressure

// PressureRetrying reports whether WatchAgentsPressure keeps trying after
// err: a file that is not the user's to write, or not there yet.
func PressureRetrying(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, fs.ErrNotExist)
}

// WatchAgentsPressure calls wake each time the agents' memory stall crosses
// PressureTrigger, until ctx ends. It returns at once with an error where
// there is no tree or the kernel takes no trigger (not Linux, no PSI, an old
// kernel); the caller then relies on the tick. V0.psi-trigger-slice: where
// the open is refused or the file is missing (PressureRetrying), it also
// tries again every PressureRetry in the background and calls armed once the
// trigger is set.
func WatchAgentsPressure(ctx context.Context, t *Tree, wake, armed func()) error {
	if t == nil {
		return errNoTree
	}
	file := filepath.Join(t.agentsDir(), "memory.pressure")
	err := watchFile(ctx, file, PressureTrigger, wake)
	if err == nil {
		return nil
	}
	err = fmt.Errorf("%s: %w", file, err)
	if PressureRetrying(err) {
		go retryPressure(ctx, file, wake, armed)
	}
	return err
}

func retryPressure(ctx context.Context, file string, wake, armed func()) {
	tk := time.NewTicker(PressureRetry)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
		}
		err := watchFile(ctx, file, PressureTrigger, wake)
		if err == nil {
			if armed != nil {
				armed()
			}
			return
		}
		if !PressureRetrying(err) {
			return
		}
	}
}

// V0.psi-stale-fire: on 20 Nov each of fyrd's 9 starts saw the new trigger
// fire 0 to 9 s after it was set, with the agents at 59-85 % headroom, and
// each made memory short and cut the memory budget ("kept across a restart:
// memory is short"). A kernel trigger starts its window from the cgroup's
// poll total, which no one updates while no trigger is set, so the first
// window counts the stall of the whole gap since the last fyrd's trigger.
// The cgroup's memory.pressure total is always current: a fire the stall did
// not cause (less than the trigger's stall since the trigger was set or since
// its last fire) is dropped. A real fire never is: the kernel fires at most
// once a window, so the growth since the last fire covers its window.

// stallGate checks a trigger's fires against the full stall total of file.
type stallGate struct {
	file string
	need uint64 // µs of full stall a fire stands for
	last uint64
	ok   bool // a total was read; without one every fire counts
}

// newStallGate reads file's full total now; trigger is "full <µs> <µs>".
func newStallGate(file, trigger string) *stallGate {
	g := &stallGate{file: file}
	var kind string
	var win uint64
	if _, err := fmt.Sscanf(trigger, "%s %d %d", &kind, &g.need, &win); err != nil || kind != "full" {
		return g
	}
	if p, ok := readPSI(file); ok && p.hasFull {
		g.last, g.ok = p.fullTotal, true
	}
	return g
}

// real says whether a fire now is a stall of at least the trigger's, and
// how much stall (µs) there was since the last check.
func (g *stallGate) real() (bool, uint64) {
	if !g.ok {
		return true, 0
	}
	p, ok := readPSI(g.file)
	if !ok || !p.hasFull || p.fullTotal < g.last {
		return true, 0
	}
	grew := p.fullTotal - g.last
	g.last = p.fullTotal
	return grew >= g.need, grew
}
