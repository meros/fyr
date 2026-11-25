# V0.thread-read-messages: `thread_read`'s `last` counts messages, not raw events

→ V0.22, SPEC 5.11 (the agent tool table).

Why: `thread_read` read the last `last` events of a thread (default 200) and then dropped everything but the words. A thread that had made more than 200 tool calls since its last text returned no words at all, and the reader had to guess at `last: 1000` to see anything.

1. `last` is the number of messages to show: a user turn or a text block of the agent's top-level message. Default 30, at most 500.
2. Fyr reads the log back from its end, a page at a time, until it holds that many messages or reaches the start; it looks at most 20,000 events back. Tool calls between the messages are counted as before (`(N tool calls)`), not limited.
3. When older events were left unread, the result says so under its header, so the reader knows to ask for a larger `last`.
4. The 8,000-character limit on the result stays (its tail is kept).
