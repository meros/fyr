# 0020 One state machine beside the agent, and one feed into it

Date: 2026-11-15 · Status: accepted · Builds on SPEC 3.2 (Entity), 4 (Events), 5 (Actions), 5.10 (async effects), 7 (Rules); 0018, 0019 · Replaces #34

**Context.** 
The spec has the parts in sections 3.2, 4, 5 and 7. The V0 rows built around them, so today one idea has several forms:

| Idea | Forms in the code (solid-core, 15 Nov) |
|---|---|
| Change the state | `Command` with `Registry[...].Apply` (owner); `Observe(fn)` (agent tools, pollers, the stream digest); `runAction` and `runFyr` with goroutines and `finishAction`; `projectCommand` |
| Tell the agent | `tell` (a `[Fyr]` message); the Stop hook's block reason (view and changes upkeep); the mid-turn PostToolUse reminder; tool replies; the static system prompt |
| Watch something, then act | `SettleWhen` and `CheckSettle*`; `deployWatch`; `ChangesStale` and `ViewStale`; `syncBuiltins` after a PR poll; quiet hours |
| Who may write a field | the source rank on conditions only ("facts beat claims"); everything else ad hoc |

**The foundation.** Five parts, each written once. Every feature is data for them.

1. **Records.** The state is a set of typed records, each with a kind, an id, an owner principal and a sequence number (3.2, `eseq`): Thread (its non-message state: run, decisions, tasks, outbox, settle, live view, actions), Change (with its sources: git, PR, deploy, workspace from 0019), Project, and later a Coordinator. Messages stay in the event log and the conversation; they are not state.
2. **Inputs and one reducer.** Every change to a record is an Input `{source, kind, args, cmdId}`. The source is `owner`, `agent`, `observer:<git|github|deploy|clock>` or `effect:<id>` (the result of something Fyr ran). One pure function per record kind, `Reduce(record, input, now) → (record', events, effects) | rejection(reason)`, holds the guard (5.1 `available`) and the field rights: each field names who may write it, so "facts beat claims" is a table, not code in each tool. The engine has one path: replay the receipt, load, reduce, store the record and its events in one transaction, broadcast, start the effects. `Command`, `Observe`, agent tools and pollers become thin constructors of Inputs.
3. **Effects run by Fyr (5.10).** A reducer never does work. It returns effects: `pr.merge`, `script.run`, `agent.send`, `process.stop`, `deploy`, `git.worktree`. Fyr's runner executes each one at most once (an effect id with a receipt). It reports the result as an Input with source `effect:<id>`, which the same reducer applies. "Merge" is: the owner's or agent's Input passes the guard, the reducer emits `pr.merge`, the runner merges, the result Input lands, and the record moves.
4. **Rules (section 7).** A Rule is `{owner record, when: Condition, then: Input, after: disarm|keep}` and is evaluated after each event its Condition depends on and at its `nextCheck`. Everything that watches becomes a Rule:
   - settle when done, merged or a change is live;
   - snooze;
   - the deploy watch and its time limits;
   - the built-in actions that follow a PR;
   - the owner's or agent's own watches ("tell me when CI on #12 is green").

   Quiet hours stay computed (#48): they watch nothing and do nothing. They are a pure function of the project's windows and the clock, read when the state is built and when a push is sent; no record changes when a window starts or ends, so a Rule would only store what the clock already says.

   Conditions read records only, so a Rule never calls the network; observers bring facts in as Inputs.
5. **The agent feed.** A projection from events to Notices for the thread's agent. A Notice is `{about: record ref, what: one sentence, urgency: wake | next | stop | silent, coalesceKey}`. It is the only way Fyr speaks to the agent; it replaces `tell`, the Stop hook reasons and the mid-turn reminders. Delivery is one queue per thread:
   - `wake`: delivered now, as a message (starts a turn when the agent is idle);
   - `next`: delivered as context at the next turn or tool call;
   - `stop`: delivered at the run's end and blocks the Stop once;
   - `silent`: only in the log and in `fyr_state`.

   Notices with the same `coalesceKey` merge ("CI on #12: failed → passed" is one line). The agent reads the whole current state with one tool, `fyr_state {about?}`, so a notice never needs to carry the full state.

**Who is told what (the feed's rules, defaults).**

| Event | Urgency |
|---|---|
| The result of an effect the agent asked for, or of an action anyone pressed on its records | wake |
| A fact that makes the agent's own change fail or block (CI failed, conflict, changes requested, deploy failed) | wake |
| A fact that moves its change forward (checks passed, approved, merged, live) | next |
| Its view or changes not touched in this run (upkeep, V0.52, V0.62) | stop |
| A change by the owner to the thread's settings, project or settle state | next |
| Everything else | silent |

**Waking.** When the agent is idle, every notice that is not `silent` starts a turn, `next` included; while a run is live, `next` waits for the next tool call or turn and `stop` for the run's end, as in the table. A settled or archived thread gets no `wake`: Rules for it are suspended (7.4) and its notices wait until it is revived.

**Records the agent shapes.** The agent writes through the same Inputs: open and revise changes, declare actions, set its live view, arm a Rule ("settle when", "tell me when"). The field rights make sure it cannot write facts (a PR's checks), approvals or the owner's state.

**Scope of a record (0016, #35).** A record has one owner host. Observed changes are host records keyed by repo and branch (#35); a thread holds references to the records it works on and Rules over them, so two threads in one folder see one change and each gets its own notices.

**Records that are not a thread (#48).** A project is a record of its own: its kinds (`project.create`, `.update`, `.delete`) are reduced by `ReduceProject`, and the thread Inputs a project change causes (the thread a new project files, the threads its folder moves) go through the threads' own reduce in the same transaction. The provider registry and T3 adoption are the host's setup, not state: their kinds are host kinds whose reduce returns one effect. A kind whose reply needs the outcome of its effect (a message cancel needs the CLI's answer; T3 adoption and the provider commands report what they did) is marked `await`: the engine runs its effects before it answers and stores the receipt with the outcome. Every command is an Input on the one path; no command family has its own.

**What stays as it is.** The event log and receipts (store), the wire DTO as a projection of records, the action registry's role as the list of owner commands (it becomes the Input kinds with `source: owner`).

**Consequences.**
- Solid core gets four issues in this order: the reducer path (2 and 3), Rules (4), the feed (5, replaces #34), then #35 on top (changes as host records with references).
- #33 (Thread components) stays: its parts are the Thread record's fields; its settle conditions become Rules in the Rules issue.
- #38 (layering) gets clear package lines: `core` holds records, inputs, reducers, conditions and notices (pure); `engine` runs the one path, the effect runner and the rule scheduler; `agent` delivers notices.
- A new feature is a record field, an Input kind, a reducer case, and perhaps a Condition kind and a notice rule. It never needs a new path for state, effects or telling the agent.
