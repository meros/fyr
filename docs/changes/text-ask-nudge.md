# V0.text-ask-nudge: a question Fyr reads in the agent's text goes back to the agent, which asks on a card or ends its turn; the owner no longer sees Fyr's guess

→ V0.210, V0.ask-tool (5), V0.header-one-block (3), decision 0033. *Amends V0.210 (3) to (5): a yes no longer puts the thread in Needs you, and the line "Fyr read this as a question" with Not a question is gone from both fronts.* 

**Why.** In its first two days the job raised 123 lines, and the owner pressed Not a question on 31 of them. Decision 0033's test already said that 32 of the 54 yeses were not blocking (22 of 54 were). The false ones were mostly optional offers ("I can arm a watch… if you want one"), turns that had already offered a chip ("Press the Settle chip"), work waiting on someone else, and once "Ingen åtgärd krävs från dig just nu" ("no action is needed from you right now"). The true ones were agents writing a question instead of calling `mcp__fyr__ask`. The agent can tell which is which; the owner should not have to.

(1) **The nudge.** A yes for the run that ended last logs `textAsk.nudged {question, run}` and wakes the agent once (notice rule `textquestion`, now a wake within the wake budget). The notice gives the question Fyr read and says: if the owner must answer or choose before the work can go on, call `mcp__fyr__ask` now; if it was an optional offer, use `offer_action` or `run_request {optional: true}`; if nothing waits on the owner, end the turn without writing anything more. The owner sees only what the agent then does: a question card, a chip, or nothing new. Fyr's reading never appears in the owner's UI, in Needs you, in a push or in a row.

(2) **Once, never a loop.** The thread keeps `textNudged`, the run that was nudged. The same run is not nudged twice, and the run the nudge starts (the agent's answer to it) is not read again. A later run is read as before.

(3) **Fewer calls.** A turn that ended with a chip or an optional command on the thread (`offers`) is not read: the agent already used the tools.

(4) **What goes.** The thread fields `textAsk` and `textAskCleared`, the action `thread.notQuestion`, the event `textAsk.raised` from now on, the Your move group "Questions in text", the row word "Question in text" and the push's "Question in text". Old `textAsk.raised` and `textAsk.dismissed` events stay in the log. The system job keeps its name, "Questions in text", in Settings → System agents.

(5) **Cost.** One short agent turn per yes: about 1 in 10 clean turn ends (123 in 2 days), against the stop hook decision 0033 rejected, which ran on every turn end.
