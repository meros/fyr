# Coordinator ("meta conversation") agents (research 2026-11-13)

## How products do it

| Product | Sees | Can do | Gets state by | Limits |
|---|---|---|---|---|
| Claude Code agent teams | shared task list, mailbox, teammates' final answers | spawn, assign, message, approve teammate plans | push on idle/completion; hooks `TeammateIdle`, `TaskCompleted` | experimental; no nesting; lead not transferable; permission prompts go to the human |
| Claude Code agent view | rows with state and a small-model summary | human dispatches; `claude agents --json` for supervising sessions | polling JSON | one machine |
| Cross-session messaging | other sessions | `SendMessage`, `notify_when_idle` | push | a message can't approve anything; loop throttling |
| Claude Code Projects | what threads report back | start/route threads, answer, propose | push on finish | prompt-level thread limits are not hard caps |
| Codex subagents | spawned threads | spawn, route, wait, close | waits | `max_concurrent_threads_per_session` (6); children inherit sandbox |
| Devin managed Devins | child trajectories | spawn with ACU limits, message, pause | self-scheduled check-ins | caps undocumented |
| Factory Missions | validation contract, shared state | plan, decompose; validators injected by the system | artifacts and reports | orchestration ~3.7 % of tokens in one example |
| T3 Code | `t3_thread_list/read` on any thread | delegate, create threads, send, wait, interrupt, schedule, PR watch | child questions pushed into the parent | mode ceiling; cannot answer approvals; no spawn cap found |

## Patterns that work
1. Orchestrator–worker with dynamic decomposition (Anthropic, *Building effective agents*).
2. A full brief per delegation: objective, output format, boundaries (Anthropic multi-agent research system).
3. Artifacts by reference, not relayed text.
4. Effort-scaling rules: 1 agent for simple, 2–4 for comparison, 10+ only for complex work.
5. Writes single-threaded; agents add intelligence (Cognition).
6. An independent, fresh validator decides correctness (Factory).
7. Push on completion with the final answer, plus a small-model row summary.
8. The coordinator holds recent state and memory, not history.

## Failure modes → mitigations
Runaway spawning → hard caps in code · duplicate work → ownership claims on Refs · invented worker state → read the durable log, never write status · coordinator does the work itself → no edit/exec tools · context bloat → summaries and Briefs, capped transcript reads · cost blow-up → budgets per thread and per coordinator · permission laundering → mode ceilings; approvals never routable to an agent · message loops → rate limits, dedupe.

## Decision for Fyr
Coordinator is its own entity kind (owner decision D4) sharing the Conversation component with Thread; it acts within a grant the user signs, receives pushed digests, writes a plan Document, opens proposals for anything outside its grant, and can never answer approvals. Post-v1 (v1.3).
