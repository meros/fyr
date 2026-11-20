# V0.row-group-code: the host sends the group of a Your move row as a code, and the two parts of a reason a row splits, so no front reads the reason's words

→ V0.your-move, V0.row-calm, V0.149 (5), V0.settle-offer-your-move; #454.

(1) **The cause.** The fronts put a Your move row in its group (Answer, Approve, Unblock) and split its reason into the short word and the line under it by reading the host's English: the prefixes `Run: ` and `Settle: `, and a match of the text before `: ` against the thread's change titles. A reworded reason on the host would quietly move rows to another group, and the rule of what is the owner's move lived twice, once in the words and once in the front.

(2) **The code.** In Needs you, `attention.group` is `answer` (a question among the owner's open asks), `approve` (asks that are only tool approvals or effects, a command to run, a settle offer, a change whose needs in Needs you are all a ready PR) or `unblock` (a failure, an agent not responding, a change stopped on a failure). The host sets it where it builds the reason (`core.ComputeWith`); out of Needs you it is absent. A code a front does not know reads as Unblock.

(3) **The parts.** A reason the row splits carries `attention.word` and `attention.subject`: `Run` and the why of a run request, `Settle` and the why of a settle offer, the need ("Merge", "Review and merge", the failure) and the change's title of a change's need. `reason` stays the whole line, for notifications and older fronts. The desktop and the phone inbox draw the same row (`web/src/rows.ts`).

(4) **An older host.** A front reads several hosts, and during an update a host may not send the code yet. When `attention.group` is absent, the front reads the reason as before. That fallback is marked in `rows.ts` and goes one release later, with the old routes (#372).

Tests: `internal/core/attention_group_test.go` (TestAttentionGroupCodes), `internal/server/contract_test.go` (the fixture has a run request in Needs you), `web/test/your-move.test.ts`, `web/test/rows.test.ts`, `web/test/contract.test.ts`.
