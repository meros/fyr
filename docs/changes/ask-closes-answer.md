# V0.ask-closes-answer: the agent's text before a question card or a Run card stays a message, not a folded step

→ V0.41 and V0.midrun-answer (decision 0011).

(1) **The cause.** V0.41 makes only the agent's last message of a run an outcome. An agent often explains something at length (headings, a table), then calls `mcp__fyr__ask` so the owner can answer on a card, then writes one short closing line ("I'll wait for your answer") and ends the run. The closing line was the outcome; the explanation the question depends on folded into "1 message · 1 steps", and the owner nearly missed it. The question card shows apart from the log, so nothing on the folded line said that the owner's decision rested on the text inside it.

(2) **A question closes an answer.** When the agent calls a tool that puts something in front of the owner and waits (`mcp__fyr__ask`, `AskUserQuestion`, `mcp__fyr__run_request`), its last non-quiet text since the run started or since the owner's last message is an outcome, as V0.midrun-answer does for a read owner message. `conversationBuilder` marks that text (`asks`) when the call arrives, because `calmTools` hides the card calls from the items `roles()` sees. A text in an earlier run is not closed, a forced view turn's quiet text stays quiet, and narration between the question and the run's last message still folds. Nothing is classified by its wording.

Tests: `web/test/pacing.test.ts` (the reported run; `AskUserQuestion` and `mcp__fyr__run_request` with narration after them; a question in a later run).
