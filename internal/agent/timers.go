package agent

import "strings"

// TimerTools are the CLI's own timers (V0.wakeups-via-watch). They live in
// the CLI's process, which Fyr ends when it is idle (V0.7), so a wake-up
// set hours ahead died with it unseen (V0.133 lists it as not covered). A
// process with Fyr's tools runs without them and snoozes with
// mcp__fyr__watch {when: {kind: "at"}}, which Fyr keeps; a thread the
// owner ran /loop in keeps them, since /loop is built on them.
var TimerTools = []string{"CronCreate", "CronDelete", "ScheduleWakeup"}

// fyrMCPTools is the allowlist of the process that has Fyr's own MCP server:
// the adapter answers these itself, and the owner is asked about nothing
// Fyr owns (V0.agy-reattach-allowlist reattaches with the same list).
const fyrMCPTools = "mcp__fyr__*"

// fyrMCPName is the server's name in that config file.
const fyrMCPName = "fyr"

// isLoop reports that an owner's message runs /loop.
func isLoop(text string) bool {
	f := strings.Fields(text)
	return len(f) > 0 && f[0] == "/loop"
}

// loopOn reports that the owner ran /loop in the thread since fyrd started.
func (m *Manager) loopOn(thread string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loops[thread]
}

// noteLoop marks the thread as one the owner runs /loop in. A process
// without the timers ends first when it is idle, so the /loop starts on a
// process that has them; in a run it ends after the run, and this /loop
// runs on it without them.
func (m *Manager) noteLoop(thread string) {
	m.mu.Lock()
	m.loops[thread] = true
	s := m.sessions[thread]
	m.mu.Unlock()
	if s != nil && !s.timers {
		m.refreshView(thread)
	}
}
