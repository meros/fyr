# V0.settle-request: an agent whose work is done calls settle_request, and its project decides whether Fyr settles the thread or the owner does

→ V0.203, V0.106, V0.next-step-chips, V0.settle-leaves, V0.project-priority. (owner, 19 Nov: "Add a settlement mcp hint... When a job is clearly done with all asks behind the model should be encouraged to call a settle request. This can be honored automatically or manually depending on project (new setting)")

(1) **The tool.** `settle_request {why}`: the agent says its work is done. The Fyr prompt steers it there: when the work is clearly done and nothing is left behind (every ask answered, nothing waits on the owner, no PR still needs the agent), call it before the turn ends. The tool's description says when not to: while waiting on CI, a review, the owner or a job of its own. `settle_when` stays for "settle once X happens".

(2) **The setting.** A project has *Agent says done*: **Ask me** (the default, stored as none) or **Settle** (`agentSettle: "auto"`). It is this host's record, not a link's shared value. Desktop and phone share the project page.

(3) **Settle (auto).** The request arms "settle when done" as the agent's (V0.203): the thread settles once the run ends, no task is live, nothing waits for the owner and no PR is open. As every agent settle, it does not fire on a pinned thread and unpin drops it (V0.106).

(4) **Ask me.** The thread keeps `settleAsk {at, why}` and shows a chip "Settle: <why>" under the agent's newest message, first among the next steps, once the run has ended (V0.next-step-chips (2); the same component on desktop and phone, 44 px on the phone). Its press is the thread's Settle: on the desktop it opens the next row (V0.settle-leaves (1)), on the phone it goes back to the list (U8). It never puts the thread in Your move and sends no push. *Amended by V0.settle-offer-your-move: the offer puts the thread in Your move.*

(5) **Stale.** A new run, a message to the agent, a settle and an unsettle clear `settleAsk`, as an optional step goes when the next turn starts. A later turn that finishes the work calls the tool again.

(6) **Tests.** `internal/engine/settle_request_test.go` (ask, the owner's settle clears it, auto settles, a bad value is refused), `internal/agent/prompts_settle_request_test.go`, `web/test/settle-request.test.ts` (the chip, the setting).
