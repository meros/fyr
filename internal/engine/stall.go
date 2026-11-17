package engine

import (
	"sync"
	"time"

	"github.com/meros/fyr/internal/lockwatch"
	"github.com/meros/fyr/internal/stream"
)

// SPEC 0.5 V0.147: the newest stall the watchdog saw, for the state and
// the stream. It has its own lock and never takes e.mu: the stuck lock may
// be e.mu, and the owner must still hear of it.

// StallShown is how long a stall shows after it.
const StallShown = 24 * time.Hour

var stallMu sync.Mutex

// SetStall stores the stall and sends it on the stream.
func (e *Engine) SetStall(s lockwatch.Stall) {
	stallMu.Lock()
	e.stall = &s
	stallMu.Unlock()
	e.Hub.Send(stream.FrameStall, s)
}

// StallState is the stall to show at now, or nil.
func (e *Engine) StallState(now time.Time) any {
	stallMu.Lock()
	defer stallMu.Unlock()
	if e.stall == nil || now.Sub(e.stall.At) > StallShown {
		return nil
	}
	return *e.stall
}
