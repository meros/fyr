package engine

import "fmt"

// acquire takes a share of the host for th's job, as the gate does when a
// heavy command runs; the agent's slot_acquire went with
// V0.one-tool-vocabulary. It answers "off", "admitted", or "queued (n
// ahead)." with the pool's reason when the thread is next.
func acquire(e *Engine, th, kind string) string {
	if e.Slots == nil || e.Slots.Off() {
		return "off"
	}
	held, ahead := e.Slots.Acquire(th, kind)
	e.syncSlotQueue()
	if held {
		return "admitted"
	}
	out := fmt.Sprintf("queued (%d ahead).", ahead)
	if s := e.Slots.Snapshot(); ahead == 0 && s.Why != "" {
		out += " Waiting: " + s.Why + "."
	}
	return out
}
