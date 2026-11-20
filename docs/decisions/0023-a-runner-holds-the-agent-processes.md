# 0023 A runner in the launcher holds the agent processes

Date: 2026-11-16 · Status: accepted · Takes up option 1 of 0022, which 0022 declined "for now" · Amends V0.13 (4), V0.6 and V0.83 · Spec row V0.90

**Context.** The `claude` CLI is a child of fyrd and talks to it over pipes, so every restart of fyrd ends every running turn. 0022 kept the drain for deploys (wait until no thread runs, up to 30 minutes) and added Restart now, which cuts the turns and sends "continue". A crash of fyrd cuts the turns with no way out at all. 0022 named the alternative, a small long-lived process that holds the pipes, and declined it for its cost: a protocol that must stay stable, process ownership outside fyrd, and in-flight requests across a restart. The owner asked for it on 16 Nov.

**Options.**

| Question | Options | Chosen |
|---|---|---|
| Where the holder runs | its own systemd unit; a child of the launcher; inside the launcher | **inside the launcher** |
| What the holder knows | the claude protocol (open questions, hooks); nothing but numbered lines | **nothing but numbered lines** |
| How a new fyrd finds a process | its own database; the holder's list | **the holder's list, with fyrd's note per process** |

**Decision.**

1. **The launcher holds the processes.** `fyr`, the launcher, already outlives every fyrd: a deploy and a crash restart fyrd, never the launcher, and the launcher changes only when the machine's Fyr package does. It runs the runner (`internal/runner`) on a Unix socket readable only by its user and gives fyrd the path in `FYR_RUNNER`. No new unit, no KillMode change: the processes stay in the unit's cgroup, and a stop of the unit ends them as before.
2. **The runner has no smarts besides three rules.** It starts a process, numbers each stdout line, stores it, writes what fyrd sends to stdin, and drops lines when fyrd acks them. It never parses a line. Its own rules are: (a) one live process per key (the thread id), and a second spawn for a key gets the id of the one that runs; (b) a process with no fyrd linked for an hour gets stdin closed, and a kill 10 s later; (c) an exited process that no fyrd collected within that hour is dropped.
3. **All smarts are in fyrd.** fyrd acks a line once it has handled it. A request of the CLI that waits for an answer (a permission question, a hook) stays in the ack's keep list until fyrd answers it, so the next fyrd gets it again. A Decision's id derives from the process and the request id, so the request that comes again finds the Decision it opened. fyrd's note per process (thread, MCP token, effort, flags) comes back from the runner's list.
4. **A new fyrd adopts before it does anything else.** It lists the runner's processes (retried for 10 s), tells the restart repair which threads still have one, adopts one process per thread, and only then resumes effects and serves. A send for a thread whose process it did not adopt meets rule (a) and links to that process; a second process never starts.
5. **A deploy does not drain when the runner holds the agents.** The switch is a restart of fyrd, and the turns go on. A fyrd without a runner (a run without the launcher, a runner with another protocol version) starts its processes as children, and the drain of 0022 still applies to it.

**Consequences.**

- Restart now (0022, V0.83) no longer shows with the runner, because no deploy drains. It stays for a fyrd without one.
- The protocol between fyrd and the runner must stay compatible: a new launcher reaches a machine only with a rebuild of the Fyr package and a restart of the unit. The protocol has a version; a fyrd that gets another one falls back to children and logs it.
- A line that fyrd handled but had not acked when it died comes again once; the handlers accept a repeat (a stray result is ignored, a repeated stream message adds one log line).
- An answer that the owner gives in the moment between the restart and the CLI's repeated request waits in its channel; a question that the CLI withdrew while no fyrd listened is withdrawn when the turn ends.
- The MCP tools of an adopted process keep working: fyrd listens on the same port and maps the token from the note.
- A process left without a fyrd for an hour ends. That is the only timeout; a turn that runs longer with fyrd linked is not touched.
