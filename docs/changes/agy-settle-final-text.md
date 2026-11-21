# V0.agy-settle-final-text: an agy reactive turn settles on its last assistant text, not on a tool completion

→ amends V0.agy-open-tool-settle, V0.agy-reactive-wakeup, V0.thread-self-wake-churn.

V0.agy-open-tool-settle moved the settle arm of an unsolicited (reactive-wakeup) turn to the moment its last open tool completed. That is not the end of the turn. The live sessions complete a tool and then think for seconds, sometimes minutes, before the next tool call or the final answer; a tool batch's last completion is followed by more thinking and more tools, not by the turn's end. Arming the 1.5 s window there closed the turn while the model was still answering. The next thought or tool then opened another unsolicited turn, so one continuous answer became a long run of runs that each ended with no result text and no measured duration — the shape V0.thread-self-wake-churn reports.

1. **A tool completion does not arm the settle (`internal/cli/agycli`).** `handleToolUpdate` no longer arms the window when a tool of an unsolicited turn finishes. The model's next step may be a thought, another tool, or the answer; a completed tool says nothing about which. Only an interrupted turn keeps the arm there: its `session/cancel` has already ended the stream, and the tool it is waiting on is the last thing to release the turn.
2. **The turn's last assistant text is the settle point.** The `usage_update` arm and the text fallback already require buffered assistant text and no open tool, so a turn that is working stays open and the answer that ends it still settles promptly. The fallback still covers a turn that ends with text and no `usage_update`.

The test drives the live order: a reactive turn with a thought, a tool and its completion but no assistant text, then more thought and another tool batch. It fails if the turn settles on a tool completion and passes only when the final text plus `usage_update` end the turn once, with the whole answer in one result.
