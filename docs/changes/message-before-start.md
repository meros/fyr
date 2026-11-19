# V0.message-before-start: the owner's message shows while the agent starts

→ V0.153, V0.33. (owner, 19 Nov, a new thread under memory pressure: the conversation said "No messages yet." above "Waiting for the machine (memory pressure) · since 11:49": "It should show the sent message and "waiting" or something".)

(1) **The cause.** The agent manager wrote the message's `user` event only after the process had started. Under pressure that start waits up to `PressureMaxWait` (15 min, V0.153), and V0.153 left this open: "the owner's message shows in the conversation only once the start succeeds".

(2) **The change.** `send` records the message (its `user` event, and the thread's outbox as "sent") before it starts the process. The conversation shows it as an unread message ("Sent") above the status line "Waiting for the machine (…)" on desktop and phone (one component). When the start fails, the message is marked "Not delivered", as before. When the process has started, the same record adds `run.started` and the text that went to the CLI. A thread whose only message is unread no longer says "No messages yet." and has no "Agent is here" line with nothing above it.

Tests: `internal/engine/pressure_test.go` (TestMessageShowsWhileStartWaits).
