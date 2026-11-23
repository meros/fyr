# V0.conversation-jumps: a conversation scrolled up jumps to your previous or next message and to the next card that waits for you, and the end arrow counts what arrived

→ V0.scroll-to-bottom (the end arrow), V0.41 (folded work), V0.run-cards-in-conversation, V0.next-step-dock. Issue #55: in a long conversation the owner could only drag; the jump to the end came first (V0.scroll-to-bottom), this is the rest.

(1) **Stops.** A stop is the owner's own message (written in the composer or pressed in the live view; a message from another thread is that thread's, not his) or a Run card that waits for him (asked, not optional, not run yet; also one that stands at the end because its place is not loaded). Every owner message is a stop, not only the ones a card answers: they are the entries of a long thread. The pure part is `web/src/jumps.ts`.

(2) **No map.** A pair of previous/next controls, not a map of the conversation down the right edge: the pair works the same on the phone, where a map is too narrow to hit.

(3) **Where.** Beside the end arrow, in the same zero-height row over the conversation's bottom right corner, and shown under the same rule (more than 24 px from the end): Needs you (with the count when more than one waits), the pair (your previous message, your next message), the arrow. A pair button with nowhere to go is off. On the phone each control is 44 px tall and the pair's halves 44 px wide (touch size); on the desktop 36 px.

(4) **The jump.** It scrolls the stop to 12 px under the top of the conversation and marks it with a ring that fades in 1.6 s (it stays still under reduced motion). Next is the first stop a jump would scroll down to, previous the last one it would scroll up to; from the stop the last jump landed on, while it is still in view, the next jump steps in order, so at the end (where the view cannot bring a late stop to the top) the jumps still go on. Your messages do not go round; Needs you goes round to the first card. A fold stays folded: a message or a Run card is never inside one.

(5) **Cards in the dock are not stops.** A question, an approval and a merge ask stand in the dock above the composer, always in view, so a jump inside the conversation would find nothing to show. A Run card in the dock still has its full card in the conversation, which is a stop.

(6) **New messages.** When the owner leaves the end, the arrow remembers the last message there; what arrives after it and counts (what the agent says, a Run card, a preview, a message from another thread; not tool calls, run marks or his own messages) shows on the arrow as "3 new". Back at the end, or on another thread, the count is gone. Older messages loaded above do not count.

(7) **Keys, desktop only** (the phone has no keyboard): `[` your previous message, `]` your next message, `w` the next card that waits for you, while a thread is open and the focus is not in a text field. They show in the `?` key list. The keys reach the conversation as a window event (`fyr:jump`).

(8) **Both fronts.** One implementation in `Conversation` (`conversation-ui.tsx`), which the desktop and the phone share.
