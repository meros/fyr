# V0.one-tool-vocabulary: one agent tool per job: change_request asks for every effect, watch arms every settle condition, a retry returns what it made, and a test checks that every tool an agent is told to call exists

→ V0.12, V0.13, V0.59 (5), V0.86 (2), V0.101, V0.136, V0.209, V0.prompt-fits-setup, decisions 0020, 0024. *Amends V0.12 (`effect_request`, `pr_link`), V0.13 (who may ask for `fyr.deploy`), V0.59 (`settle_when`) and V0.86 (2) (`slot_acquire`, `slot_release`).*

The agent tools had drifted. The `gh pr merge` gate and the test agent named `effect_request pr.merge` while the prompt named `change_request`. `fyr.deploy` could be asked by every project's agent. `settle_when` did what `watch {then: settle}` does. `slot_acquire` and `slot_release` described themselves as not needed since V0.209. The hidden `pr_link` alias was still callable. And each call of `change_request` or `watch` made a new ask or watch, so an agent that retried after a lost reply made two.

(1) **Merge and deploy are `change_request`.** `effect_request` is gone. `change_request {change, ask: "merge", args?: {method, head}}` merges; `change` may also be a PR URL the thread does not track yet, which Fyr then tracks and holds for CI as `effect_request` did. `change_request {ask: "fyr.deploy", args: {dir?, tests?}}` deploys Fyr and needs no change. The `gh pr merge` gate, the MCP instructions and the test agent name `change_request`. The result line says "Your change_request …" for every ask.

(2) **Only Fyr's own repo deploys Fyr.** A `fyr.deploy` ask is taken only from a thread whose folder, or whose project's folder, is a Fyr checkout (it holds `cmd/fyrd` and `docs/changes/README.md`). Any other thread is refused with a line that names `run_request`. The owner's own deploy (`host.deploy`, auto-update) is unchanged, and so is the flow on Fyr's repo: a deploy of a branch builds and switches once the owner presses.

(3) **Settle conditions are `watch`.** `settle_when` is gone. `watch {when: {kind: "workDone"}, then: "settle"}` is "settle when done" (only alone, only with then settle); "settle when merged" or when a change reaches a phase is `watch {when: {kind: "changeReached", change, phase}, then: "settle"}`. `settle_request` stays.

(4) **`slot_acquire`, `slot_release` and `pr_link` are gone.** Fyr admits a heavy command at the gate (V0.209); `change_open {pr}` links a PR.

(5) **A retry returns what it made.** A `change_request` whose effect and args match an ask of the thread still open or held for CI returns that ask ("Already asked"). A `watch` whose condition, purpose and outcome match an armed watch of the agent returns that watch ("Already armed").

(6) **A removed tool says what replaced it.** An agent that read the old tool list (V0.tool-reload) and calls a removed tool gets a tool error, "Fyr removed the tool X: …", that names the replacement. The removed tools are not listed.

(7) **The test.** A test fails when a prompt, the MCP instructions, a gate reason, a [Fyr] notice or a tool description names `mcp__fyr__X` and X is not a registered tool, and when an agent-facing text names a removed tool.
