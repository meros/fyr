# V0.slash-first-turn: a slash command that starts a run runs as a command, not as plain text

→ V0.79 (4), V0.95 (1). *Amends V0.95 (1) (the turn's first message carries the pending notices).* 

(1) **The cause.** The CLI runs a command or a skill only when the user message's text starts with `/` (and, with several text blocks, when the last one does). V0.95 (1) put the pending notices in front of the first message of a run, so a `/command` sent to an idle thread with any notice pending reached the agent as plain text; a skill that only the user may start (`disable-model-invocation`) then could not run at all. Checked on CLI 2.1.322: `/cost` alone runs with no API call; `[Fyr] note.\n\n/cost`, or `/cost` followed by a second text block, calls the model.

(2) **The rule.** The owner's message (no `from`) whose text starts with `/` after white space takes no notices. They stay pending: the run's hooks take them at its first tool call or its stop, or the feed sends them after the run. Attachment lines after the command stay as they are (they become the command's arguments); image blocks before it do not stop it.

Tests: `internal/agent/slashturn_test.go` (a `/command` takes no notices at the turn; a plain message does).
