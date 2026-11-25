# V0.subagent-admission: a heavy job's admission reaches the subagent whose command was refused, not its parent

→ V0.209, V0.guard-wake (decision 0036 E1), #49, SPEC 6 (the agent feed: Tool call).

**Why.** The pool of heavy jobs and the agent feed know only the thread. When the gate refused a subagent's heavy command (the host was full, or memory was short), the admission or the recovery later went to the thread's own agent as "Your test job is admitted now: run the command again". It woke that agent for a turn about a job it never ran and could not place, while the subagent, told that no message would reach it, had gone on or handed back. Seen in one thread: the main session spent a turn on a test run its worker had queued.

1. **The gate remembers the actor.** The gate hook passes the subagent's `agent_id` and `agent_type` with the call. When the gate refuses a heavy command (queued, or held while memory is short), the engine notes which actor it was: a subagent, or the thread's own agent. The last refusal on a thread decides; an admission of that actor's command forgets it.
2. **A running subagent hears it itself.** When the queued job is admitted, or memory recovers, and the refused subagent still runs, the thread's agent gets no notice. The line waits for that subagent's next tool call and arrives once, as the PostToolUse context of that call: "[Fyr] Your test job is admitted now: run the command again." If the subagent runs the command again before that, the gate admits it and nothing is said.
3. **A subagent that ended is named to its parent.** When the subagent has ended without running the command (its SubagentStop came, or its process ended), the thread's agent is told and woken, with the subagent named by type and id: "The test job your subagent worker (a1) was queued for is admitted now, but that subagent has ended without running it: run the command again yourself if you still need its result." The recovery from a memory hold is worded the same way.
4. **Liveness.** The agent manager counts a subagent as running from the first hook that names it until its SubagentStop, which every process now registers (callback `substop`), or until the process ends. The engine's tick hands the waits of subagents that are gone to their threads.
5. **The subagent's refusal** says that no message can wake it, that Fyr puts the admission in the result of its next tool call while it runs, not to run the command again before that or wait for it, and to report it as queued and hand back if its work ends first, after which the main session is told, naming it.

Not changed: the thread's own refused command keeps the plain admission; the pool still holds one place per thread, and its admission covers the thread's subagents.

Tests: `internal/engine/subagent_admission_test.go`, `internal/agent/feed_test.go` (TestSubagentHooksCarryItsAdmission), `internal/engine/heavy_test.go` (the subagent's refusal).
