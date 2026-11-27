# V0.approval-context-flake: TestEngineApprovalContext waits for the first run to end before it records more messages

→ a test only; amends no rule.

**The flake.** `TestEngineApprovalContext` failed once in CI with the owner's first message "Run tests" placed last, after messages recorded later. The message was not written twice. `thread.create` sends it with an id, and `readTurns` holds a message with an id until its `message.state` read event arrives, then places it where the agent read it (V0.midrun-answer). The test only waited for the `user` event. The fake agent's run went on in the background, and on a busy runner its `message.state` read could land after the test's own records, so the message moved behind them. That is the order the conversation should show, so `ApprovalContext` needs no dedupe and no change. The log line `message.sent / message.state: database is closed` came from the same run still writing when the test ended.

1. The test waits until the thread's first run is done and its first message is read before it records more messages, so nothing from that run is written afterwards.
