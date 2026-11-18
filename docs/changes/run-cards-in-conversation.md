# V0.run-cards-in-conversation: a Run card stands in the conversation, where the agent asked

→ V0.149. *Amends V0.149 (2) (the card was under the conversation, under the newest message).* (owner, 18 Nov, a screenshot of a card held above the composer with its output in a small scrolling box: "I like these run cards! But they should be *in the convo* not stuck to the bottom. It's hard to see the results from the run this way...")

(1) **The place.** `run_request` logs a `run.requested` event `{on, action}`. The card stands at that place among the messages and scrolls with them, on the desktop and the phone (the same component). It is never folded into the agent's steps. The same id asked again moves the card to the newest place.

(2) **No place in the log.** A request asked before this version, or further back than the loaded part of the log, stands at the end of the conversation, so a card that waits for the owner stays in reach.

(3) **The output.** The output has no scroll box of its own: it is as tall as its lines, folded after 12 as before. At the bottom of the conversation the view follows a card whose output grows; away from the bottom nothing moves.

(4) **Closing.** Not now and Close remove the card from the conversation; the `action` line of V0.149 (6) stays.
