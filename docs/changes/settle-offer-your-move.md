# V0.settle-offer-your-move: a thread with a settle offer is in Your move

→ V0.settle-request (4), V0.your-move, V0.row-calm. (owner, 20 Nov: "A thread that has a settle offer should be in \"your move\"")

(1) **Attention.** On a project that asks (V0.settle-request (2), *Ask me*), a thread whose agent called `settle_request` and whose run has ended is `blocked` in Needs you with the reason "Settle: <why>" ("Settle: the agent is done" without a why), the chip's words. It is the last cause, after a Run card: a question, an approval, a failure, a ready or stopped change and a Run card name the owner's move first. A running thread is Working as before (a new run clears the offer anyway, V0.settle-request (5)).

(2) **Group and row.** Your move puts it under **Approve** (a yes to say), and the row reads **Settle** with the why on the line, as a Run card reads **Run** (V0.row-calm). Desktop and phone share `rows.ts`, so both fronts show it the same way. The press opens the thread, where the Settle chip is.

(3) **Push.** As every `blocked` state, it pushes after the notify delay if still true (V0.20). This amends V0.settle-request (4), which kept the offer out of Your move and sent no push.

(4) **Tests.** `internal/core/attention_settle_offer_test.go`, `web/test/settle-request.test.ts`.
