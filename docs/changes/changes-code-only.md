# V0.changes-code-only: a change is code Fyr tracks itself; the agent no longer declares other changes

→ V0.59, V0.60, V0.72, V0.84, V0.98, V0.101, V0.next-step-dock, V0.next-step-chips. *Amends V0.59: the agent cannot open a change of any kind but code, and a change has no asks but merge.* 

(1) **The cause.** In 14 to 19 Nov agents opened 73 changes that were not code: 46 were "Deploy/Update Fyr" records that `fyr.deploy` has since replaced, and 3 ever had a button. Apply, deploy, revert, review and approve on such a change, and `bind`, `onOk` and `when` on its buttons, were a third way to hand the owner a step, beside the Run card and the button.

(2) **What a change is.** A change is code: a pushed branch or a PR, which Fyr finds and reads itself (checks, reviews, conflicts, head). Its card, the board, Ready, the merge ask, Not now, Refresh now, Done and End stay as V0.101, V0.98, V0.84 and V0.72 say.

(3) **What goes.** `change_open` takes `kind: code` only (a branch or a PR, for a push Fyr cannot see). `change_update` stays for what only the agent knows about a code change. `change_request` takes `merge` only. A kind or an ask that went answers with one line that names its replacement:

| Before | Now |
|---|---|
| An infra, host or data change to apply, deploy or revert | `run_request` with the command and the why (V0.next-step-dock) |
| Approve or review a document or a design | `ask` |
| Proof: a plan, a log, a preview | `show`, in the conversation |
| A change Fyr cannot see, only to record it | nothing: the conversation is the record |

(4) **The old records.** A change that is not code and has not ended ends as abandoned, with "Fyr no longer tracks changes that are not code" as its reason; it stays under Done here and nothing about it reaches Your move. An open ask on it is closed and the agent hears so once.

(5) **The tools.** The aliases `change_revise`, `change_status`, `change_attach` and `change_unchanged` go. `effect_request` stays for `pr.merge` and `fyr.deploy`.

(6) **The prompt.** The Fyr block no longer says "open a change before the first write to something Fyr cannot see"; it says a write the owner must approve is a Run card.
