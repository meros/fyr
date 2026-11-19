# V0.ask-tool: the agent asks on a card with Fyr's own tool, and is told when it asked in text

→ V0.3, V0.12, V0.149, V0.210, decision 0033. (owner, 19 Nov: "Fyr works best when the agent uses ask the user... we should probably expose the same tools with fyr mcp and steer agents to use that instead of leaving open things in text.")

(1) **The tool.** `mcp__fyr__ask {questions}` takes what the CLI's AskUserQuestion takes: 1 to 4 questions, each with a text, an optional header, up to 4 options and multiSelect. A question may have no options; the owner then answers in their own words. It opens the same `ask` Decision, so the card, the keys, Your move (Answer), the push and both fronts are unchanged.

(2) **It does not hold the turn.** The call returns at once. The Decision is marked `async`: it lives in the thread, not in the agent's process, so a restart of Fyr or of the agent keeps it (an AskUserQuestion is lost there and fails the thread). The agent goes on with what does not depend on the answer, or ends its turn.

(3) **The answer.** The owner's answer closes the Decision in the same reduce and reaches the agent through the feed as a wake: "The owner answered your question: … → …", the questions in their order.

(4) **Limits.** The same questions asked again are the same card. At most 4 such questions wait on one thread. Archiving the thread voids them and is not held back by them.

(5) **The steer.** The Fyr prompt says: never leave a question or a choice in the message's text, call `mcp__fyr__ask`; ask only what the user must decide, never for permission to go on. When Fyr reads a question in the agent's text (V0.210), the agent is also told so with the owner's next message (never a wake, so it costs no turn), and pointed to the tool.

(6) **Not in this change.** AskUserQuestion still works as before. The agent cannot withdraw a question; the owner answers it in their own words.
