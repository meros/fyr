# V0.arrived-from: the thread an answer moves you to says where you came from

Answering a question on the desktop opens the next thread that needs the owner at once (`answerThenNext`); the
phone offers it as "Next: …". It is a quick way through every open question, but the switch is easy to miss: the
owner looks up and reads a thread they did not choose, with no sign of how they got there.

Now the thread reached that way shows a bar under its header, on both fronts (`web/src/arrived-bar.tsx`, state in
`web/src/arrived.ts`):

- **✓ Answered "<title of the thread answered>"** · moved on to this one · N more need you
- **‹ Back** opens the thread answered (the desktop's Alt+← does the same, V0.145); **×** hides the bar.
- It slides in with a wash that fades, so the switch itself is seen (no motion under `prefers-reduced-motion`).
- It stays while the owner is on that thread, through its own question card, and goes once they open any other
  thread; it does not come back when they return.

Phone: the bar shows with the question card open too (the card is what the owner answers next) and its buttons are
44 px for touch. The phone still asks before it moves ("Next: …"); the desktop still moves at once.
