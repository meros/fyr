# V0.no-interrupt: nothing that arrives takes over what the owner is typing or doing

→ U15, 13.2, the buildId rule, V0.composer-focus-reach. *Adds U26 and amends U15: a question no longer takes a composer that holds a message.*

**Why.** An owner who was writing in Fyr could find their field taken over by something that arrived. On the desktop, an agent's question turned the composer into its answer field mid-sentence: the draft vanished from view, the next keys went into the answer and Enter answered the question. On the phone, any question or approval unmounted the composer and closed the keyboard. A new build reloaded the page in the middle of an answer. A digit or Enter meant for something else landed on a card that had just appeared, and a stray `1` approved an approval. Fyr is used all day while agents keep working, so an arrival must wait for the owner, not cut in.

(1) **Desktop composer.** A question that arrives while the composer holds a message leaves the composer to the message: Enter sends it, and the card waits above. The composer becomes the answer field once it is empty, at once when the owner chooses "Other…" (`0` or a click), or when the answer already has text (`compose.ts` `answerTakesComposer`). The draft is kept either way.

(2) **Phone.** A question or approval that arrives while the owner writes (the composer has the focus or holds a draft) comes folded, and the composer stays under it with its focus and keyboard. Unfolding the card hides the composer as before; folding it again brings the composer back. A card that is there when the thread opens comes open as before. This is decided in the render the card arrives in, so the field is never unmounted.

(3) **Arrival grace for keys (desktop).** A card that arrives while its thread is open takes no key for its first second (`no-interrupt.ts` `ARRIVAL_GRACE_MS`): a key pressed in that moment does what it did before the card came. A card that is already there when the owner opens the thread takes keys at once. The phone has no card keys, and (2) covers a tap there.

(4) **Reload for a new build.** The reload waits while the owner is busy: a dialog or sheet is open, the focused field holds text, or a key was pressed in the last 10 s. It checks every 2 s and reloads at once when the page is hidden.

(5) **Settings fields.** A value the host pushes replaces a field's text only while the owner has not changed it (`SaveField`).

(6) **The rule (U26).** Nothing the host, an agent or the clock does takes the focus, removes or repurposes the field the owner is in, changes what their next key does, replaces their text or reloads the page while they use it. An arrival shows beside what the owner is doing and waits. A dialog or sheet opens only from the owner's press. New features follow it; an exception says why in its change entry.

(7) **Not in this change.** These were found and left for their own changes: an open thread that switches to the View tab the first time an agent publishes a live view; the live view's frame reloading on a republish while the owner uses it; the desktop opening the next thread when the host accepts an answer, even if the owner has moved on by then; a reload when the device is signed out.
