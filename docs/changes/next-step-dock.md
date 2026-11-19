# V0.next-step-dock: everything that waits on the owner stands in one dock above the composer, a Run card too

→ V0.149, V0.run-cards-in-conversation, V0.ask-tool, V0.your-move. *Amends V0.run-cards-in-conversation (1): the card keeps its place in the conversation, and while it waits it also stands in the dock.* (owner, 19 Nov: "There is the run card, the action buttons and the change that has an apply... These three options are a bit confusing and too much for 'a random action to perform'... I often miss the run cards, even if they are nice otherwise.")

(1) **One idea.** What the agent hands the owner is a *next step*. A step either blocks the agent (it waits for the answer) or does not. This entry is about the steps that block: a question (`ask`, AskUserQuestion), a command to run (`run_request`), an approval (a merge, a Fyr deploy). V0.next-step-chips is about the steps that do not.

(2) **The cause.** A question stands in the dock above the composer, where the owner looks after reading. A Run card stood only at its place in the conversation, and what the agent wrote after it pushed it up out of sight. In 14 to 19 Nov the owner ran almost every card, but found them by scrolling.

(3) **The dock.** Every blocking step of the thread stands in the one dock, the newest first, "1 of 3" when there are several, the same pager the questions have now. A Run card in the dock is short: the why, the command's first line (… when there are more), the host, and Run (or Run as root), Copy, Not now, and Show in conversation, which scrolls to the full card.

(4) **The run.** Run in the dock takes the step out of the dock and scrolls the conversation to the full card, which streams the output as V0.run-cards-in-conversation (3) says. The output never stands in the dock. Not now takes it out of the dock and closes the card, as before.

(5) **The place stays.** The full card stays at its place in the conversation, and is the only place that shows the output and the whole command. While the step waits, the full card says "Waiting in the dock" instead of its buttons, so there is one place to press.

(6) **Unchanged.** Your move (Approve), the inbox reason "Run: …", the push and the keys. Desktop and phone share the dock component; the phone's buttons are 44 px (touch size).
