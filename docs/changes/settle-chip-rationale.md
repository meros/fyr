# V0.settle-chip-rationale: a Settle chip says why the work is done, never the thread's title again

→ V0.settle-request (4). (owner, 20 Nov: "The settle request button doesn't have to show the same threads name.. instead show a rationale. \"Settle, pr merged\" etc")

(1) **The agent's why.** `settle_request`'s `why` is the reason the work is done in a few words ("PR #12 merged", "question answered", "pushed to main"), not what the thread was about: the owner already sees the title. The tool's schema and the Fyr prompt both say so.

(2) **The chip.** A why that only repeats the thread's title (the same words, or one inside the other, ignoring case and punctuation) is dropped and the chip reads "Settle: the agent is done". Desktop and phone share the chip (`settleChip` in `web/src/runcard.ts`).

(3) **Tests.** `web/test/settle-request.test.ts`.
