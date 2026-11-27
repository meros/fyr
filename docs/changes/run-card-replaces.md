# V0.run-card-replaces: a new Run card replaces the agent's earlier cards that never ran

→ V0.run-cards-stay-put, V0.149. *Amends V0.run-cards-stay-put (1): asking again made a new card, but the earlier one kept waiting.*

An agent that fixed its script and asked again left the old card waiting. The dock shows the oldest waiting card first, so the owner's next Run ran the old script. Its result came back as if it were news, the agent acted on it and asked again, and the thread went round in a loop of cards.

(1) **A new card replaces the waiting ones.** `run_request` closes the agent's earlier run requests on the thread that wait on the owner and never ran. Each one's place in the conversation logs an `action` event `{request: true, replaced: true, by: "agent"}` and reads "Replaced by a newer card: “why”", on the desktop and the phone (`actionEventLine`). A card that ran stays as it was.

(2) **What stays.** An optional request (a chip, V0.next-step-chips) replaces nothing. The playbook's own deploy card (V0.playbook-moments, id `deploy-<stage>`) is never replaced. `alongside: true` keeps the earlier cards, for when the owner must run both.

(3) **The agent hears it.** The tool's reply names the cards it replaced. The agent closed them itself, so it gets no "Not now" notice.

(4) **Never interrupt the owner.** A card the owner is running is not replaced: it has a run. A card replaced while the owner reads it goes from the dock, and the owner presses the newer one. No dialog opens and nothing takes the focus.
