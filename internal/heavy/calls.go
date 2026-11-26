package heavy

import (
	"sync"
	"time"
)

// V0.freeze-controller: what an agent ran, kept briefly, so work nobody
// admitted can be named from it. The detector sees a process command line
// (`node …/worktrees/<name>/.yarn/releases/yarn-3.5.1.cjs run lint:typecheck`),
// which the gate, matching the agent's Bash text (`cd web && yarn lint`),
// never meets; and each worktree made a new pattern, so none reached
// LearnAfter. The text the agent wrote is what the gate sees next time.

const (
	callsKept   = 8
	callsMaxLen = 2000
	// CallsBefore and CallsAfter bound how far from an episode's start a
	// command may be to be taken as its cause: the detector flags work after
	// its window, so the command ran before the flag.
	CallsBefore = 150 * time.Second
	CallsAfter  = 15 * time.Second
)

// Call is one Bash command of a thread.
type Call struct {
	Command string
	At      time.Time
}

// Calls keeps each thread's latest Bash commands.
type Calls struct {
	mu sync.Mutex
	by map[string][]Call
}

// Note records a command the thread ran at at.
func (c *Calls) Note(thread, command string, at time.Time) {
	if thread == "" || command == "" {
		return
	}
	if len(command) > callsMaxLen {
		command = command[:callsMaxLen]
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.by == nil {
		c.by = map[string][]Call{}
	}
	l := append(c.by[thread], Call{Command: command, At: at})
	if len(l) > callsKept {
		l = l[len(l)-callsKept:]
	}
	c.by[thread] = l
}

// Cause is the newest command of thread run within CallsBefore before start
// (or CallsAfter after it) that names work of its own: one Suggest would
// teach. "" when there is none, then the caller keeps the process command.
func (c *Calls) Cause(thread string, start time.Time) string {
	c.mu.Lock()
	l := append([]Call(nil), c.by[thread]...)
	c.mu.Unlock()
	for i := len(l) - 1; i >= 0; i-- {
		d := start.Sub(l[i].At)
		if d > CallsBefore || d < -CallsAfter {
			continue
		}
		if p, _ := Suggest(l[i].Command); p != "" {
			return l[i].Command
		}
	}
	return ""
}

// Forget drops a thread's commands (it ended).
func (c *Calls) Forget(thread string) {
	c.mu.Lock()
	delete(c.by, thread)
	c.mu.Unlock()
}
