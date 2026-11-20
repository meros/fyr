# V0.scroll-to-bottom: a conversation scrolled up shows an arrow that takes you back to the end

→ V0.38 (the conversation follows new content at its end). (owner, 20 Nov: "Add a 'scroll to bottom' arrow somewhere when scrolled up".)

(1) **When.** The arrow shows while the conversation is more than 24 px from its end, the same line V0.38 uses to decide whether to follow new content. At the end, and on opening another thread, it is gone.

(2) **Where.** A round button with a down arrow floats over the conversation's bottom right corner, 12 px above the composer's edge. It takes no room in the conversation (a zero-height sticky row at the end of the scroller), so it moves nothing when it comes and goes.

(3) **What it does.** It scrolls to the end (smoothly unless the system asks for reduced motion) and the conversation follows new content from there, as V0.38 does at the end.

(4) **Both fronts.** One implementation in `Conversation` (`thread.tsx`). The phone's button is 44 px, for the finger; the desktop's is 36 px.
