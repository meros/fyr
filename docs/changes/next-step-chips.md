# V0.next-step-chips: an optional next step is a chip under the agent's newest message, not a button in the header

→ V0.60, V0.68, V0.75, V0.149, V0.next-step-dock. *Amends V0.60: a thread's action is a chip under the newest message, an action is a prompt only, and a command goes through `run_request`; amends V0.149: `run_request {optional: true}`.* (owner, 19 Nov: "I often don't see the command buttons, even if they sometimes makes sense...")

(1) **The cause.** A thread's buttons stood in the header, above the tabs: the last place the owner looks after reading a reply. In 14 to 19 Nov agents offered 15 script buttons and 10 agent buttons. The owner pressed most script buttons once and about half the agent buttons. Half the script buttons were a command to run ("Force-push…", "Ship with nx"), so they were a Run card in another place, with other rules (argv, no shell, no root, no streaming).

(2) **The place.** An optional step is a chip under the agent's newest message, above the dock and the composer, on desktop and phone (the same component; 44 px on the phone, touch size). The header has no buttons.

(3) **Stale.** The chips belong to the message they stand under. When the owner sends a message or the agent starts a new turn, they go. An optional step never puts the thread in Your move and never sends a push.

(4) **Two kinds.** A chip either sends a prompt (`offer_action {label, prompt, in: this|new}`), or opens a Run card for a command (`run_request {…, optional: true}`). A run chip opens the full card at its place, without the dock: nothing waits on it. The output and the result reach the agent as a [Fyr] message, as before.

(5) **One way to run a command.** `offer_action` loses `run: script`, `argv`, `cwd`, `timeoutSec`, `bind`, `onOk`, `when` and `primary`, and `on` is only `thread` or `view`. A call with them is refused with one line that names `run_request`. Fyr runs a command on the owner's press only through a Run card.

(6) **The live view.** `data-action` and `fyr.run` in a live view stay (V0.68). A view's command is a Run card the view opens, confirmed the same way.

(7) **Fyr's own.** Merge, Re-run failed checks, Fix CI, Address review and Rebase stay on the PR's change card (V0.101); they are about the change, not the newest message.

(8) **The steer.** The Fyr prompt says: blocked on the owner, use `ask` or `run_request`; an obvious next move the owner may want, offer it (`offer_action`, or `run_request` with `optional: true`); never write either in the message's text.
