# V0.midrun-answer: an answer that the owner's mid-run message closed stays at full weight, a folded line names the messages it holds, and thread_read orders a message where the agent read it

→ V0.41 and decision 0011 (Consequences), V0.33, V0.22, V0.45.

(1) **The cause.** V0.41 makes only the agent's last message of a run an outcome; every earlier one is a step and folds. Decision 0011 rejected folding by user message so that a steering message mid-run does not end the turn. It did not consider a text that fully answered the owner's request before the owner's next mid-run message was read. In one run the agent wrote a complete answer (a priced table) and made one tool call; the owner's next message was read with that call's result; the agent then wrote a status line, made five more calls and wrote a second answer. The first answer folded into a line that read "Ran N commands · N steps", which names only tools, so the owner could not find it.

(2) **A read message closes an answer.** In `roles()` (`web/src/conversation.ts`) the agent's last non-quiet text before a mid-run owner message that the agent read is an outcome too, within the same run. The message sits where it was read (V0.33), so the text before it is what the agent had said when it read it. A message that was cancelled or lost, or one from another thread, closes nothing. This stays within decision 0011: the turn is still the run, nothing is classified by its wording, and the narration between the two answers still folds.

(3) **A folded line names its messages.** A fold that holds agent text starts with how many ("1 message · Ran 5 commands"); a fold of messages only reads "2 messages". No text hides behind a line that names only tools.

(4) **One order for every reader.** `readTurns` (`internal/engine/turns.go`, behind thread_read and a handover's conversation) places a user message that has an id at its `message.state` read, cancelled or lost event, as `conversationBuilder` does. A message whose state never arrives stays where it was sent. Before, it placed every message at send time, so the web and thread_read disagreed on order.

Tests: `web/test/pacing.test.ts` (the reported run, a message never read, an earlier run, fold labels; the mid-run case now keeps "Checking the repo." as an outcome), `internal/engine/turns_test.go` (TestReadTurnsPlacesMessageWhereRead, TestReadTurnsCancelledAndLostTakeTheirPlaceToo, TestReadTurnsUnreadMessageStaysWhereSent).
