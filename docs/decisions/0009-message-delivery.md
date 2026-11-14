# 0009 Message delivery follows the CLI's own queue

Date: 2026-11-14 · Status: accepted · Spec: 0.5 V0.33, 16.2

**Context.** The owner asked to see where the agent is, which messages still wait for delivery and which the agent has read, and to cancel a message the agent has not read. A message sent while a run is live ("steering") is written to the CLI's stdin at once (V0.3). Fyr did not know when the agent took it in, and could not take it back.

**What the CLI does (claude 2.1.322, measured 14 Nov 2026 with haiku).** A turn ran two `sleep 6` Bash calls; two more user messages, each with a `uuid`, were written 3 s and 4 s in.

- Every user message with a `uuid` gets `{"type":"command_lifecycle","command_uuid":<uuid>,"state":"queued"}` on stdout the moment the CLI reads it.
- A message written during a turn waits in the CLI's queue. At the next tool boundary (after the tool results, 8.7 s) the CLI takes in everything queued, in the same turn, and reports `state:"started"` for each, before the model's next reply. When the CLI is idle, `queued` and `started` follow each other at once and the message starts a turn.
- When the turn ends, each message it carried gets `state:"completed"`.
- The control request `{"subtype":"cancel_async_message","message_uuid":<uuid>}` withdraws a queued message: the reply is `{"cancelled":true}` and stdout gets `state:"cancelled"`; the model never saw it (the reply named only the first steering message's word). For a message already started the reply is `{"cancelled":false}`.
- `--replay-user-messages` also echoes each user message (`isReplay: true`, same `uuid`) when it is taken in, but `command_lifecycle` comes without any flag, with the flags Fyr already passes, so Fyr does not need it.
- The CLI's schema text says an interrupt can report `cancelled` for messages of the stopped turn, and an interrupted turn may put a message back in the queue (`queued` again).

**Options.**

1. Fyr keeps its own outbox and holds messages until it sees a tool boundary, so Cancel has a window. Fyr would have to guess tool boundaries, would delay steering by one step when it guesses wrong, and duplicates a queue the CLI already has.
2. Write at once, as today, and follow the CLI's queue: `command_lifecycle` is the delivery receipt and `cancel_async_message` is the cancel.

**Decision.** Option 2. Fyr gives each message an id, sends it as the user message's `uuid`, and maps the CLI's reports to message states: `sent` (written, no report yet), `queued` (in the CLI's queue: "Waiting"), `read` (`started`, or `completed` without a `started`), `cancelled`, `lost`. The cancel window is exactly the time the message waits in the CLI's queue, which is the time it is useful: while the agent works on a step. Steering is never delayed. `thread.cancelMessage` answers only after the CLI answered, so the UI never shows a cancel that did not happen.

The thread keeps the unread messages in `outbox`. When the process ends, or the host restarts, with messages still in it, they become `lost` ("Not delivered") because the CLI's queue died with the process. Fyr does not resend them by itself: after a restart the thread is failed (V0.6) and the owner decides; the client offers "Send again". A CLI without `command_lifecycle` still works: its messages become `read` when the turn ends, and Cancel answers 409 because the CLI does not know the request.

**Consequences.** Delivery depends on two undocumented parts of the CLI's stream protocol (`command_lifecycle`, `cancel_async_message`). The fake CLI models both (a queue, `queued`/`started`/`completed`/`cancelled`, and the `slow` script that takes queued messages in at a tool boundary), so a CLI change shows up in the conformance recording planned in 0003, not in the unit tests. A cancel is final at the CLI, so the client keeps the cancelled message in place, struck through, with Send again, instead of an undo toast.
