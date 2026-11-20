# V0.agy-interrupt-before-prompt: a turn is announced "started" only once its prompt is on the agent's pipe, so an Interrupt cannot pass it

→ amends V0.agy-adapter (2); `internal/cli/agycli` (`promptWorker`, `request`).

The adapter emitted `command_lifecycle {state: "started"}` before it wrote `session/prompt`, and its pipe was written from two goroutines without an order between them. Fyr's Stop sends `session/cancel` the moment it sees `started`, so on a slow machine the cancel could reach the agent ahead of the prompt it cancels. ACP has no answer for a cancel of a turn the agent never received: the real agent waits for a prompt, the adapter waits for an answer, and the turn never ends.

CI saw it once as `TestACPQueueCancelAndInterrupt` hitting `go test`'s 10-minute timeout (`TestMain` → `acp_test.go:496`, the result loop; the prompt request still pending in `acp.go`). It was not a slow wait: `go test -count=60` under six busy loops hangs the same way, and `-count=30` of the new test does too.

**The fix.** `requestOn` takes a callback that runs after the request's bytes are written and before its answer is waited for; the prompt worker announces the turn there. `started` is now proof that the prompt is out, so any `session/cancel` Fyr sends after it reaches the agent after the prompt.

**The test.** `TestACPStartedFollowsThePrompt` runs 20 `slow` turns of the fake agent (a turn that lasts until it sees `session/cancel`), interrupting each one the moment it is announced. With the announcement first, the test hangs; with the fix it passes, `-count=40` under four busy loops included. The fake agent's `slow` case was already there for `TestACPQueueCancelAndInterrupt`; its reading of the next stdin line is what makes the order matter.
