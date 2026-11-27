# V0.run-card-keeps-run: a Run card that ran never comes back as fresh

→ V0.149, V0.run-card-rerun, V0.next-step-dock, I7. *Amends V0.149: a holder kept only its last 10 runs.*

A thread with more than 10 Run cards dropped the oldest card's run when the 11th run started. The dropped card looked as if it had never run: on the host it waited on the owner again (Needs you, and "It is running" no longer held it), and on both fronts it came back to the dock with a fresh Run button and no confirmation. Each press in the dock started a run, which dropped the next card's run, so that card came back under the same button. Pressed a few times in a row, every card ran again in the order of its last run, and cards that had succeeded ran with no confirmation, some writing to outside services (#735).

(1) **A standing card keeps its last run.** A holder still keeps its newest 10 runs, and also the newest run of every action it still offers, and every run that runs. A card the owner closed gives its run back to the cap. So the runs stay bounded by 10 plus the cards that stand.

(2) **The host asks for the confirmation too.** A press of a run request whose last run succeeded runs only with `again: true`, which the card sends after Fyr's "Run again?" confirmation (V0.run-card-rerun). Without it the host refuses: "It already succeeded; press Run again on its card and confirm". A fresh or failed card still runs from one press. The mock does the same.

(3) **A press records its commandId.** The `action` event of a press (and of Not now) carries the command's `commandId` beside the actor and the front, so the log tells a resend from a new press. A press sent again with the same commandId runs once (I7); a test holds it for `action.run` too.

(4) **Never interrupt the owner.** Nothing new appears by itself: a card that ran simply stays as it was, and the confirmation opens only from the owner's own press of Run again.
