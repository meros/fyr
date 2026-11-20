# V0.subagent-timeline: an opened subagent shows its prompt and everything it did, in order

→ V0.16. (owner, 20 Nov: "Maybe we should add support in fyr to see the subagent data?", then chose "Full timeline in order".)

(1) **What was missing.** The CLI sends a subagent's texts, thinking and tool calls with `parent_tool_use_id` set (checked on CLI 2.1.327, with and without `--forward-subagent-text`: the same messages, so Fyr passes no new flag). An opened Agent row showed the subagent's tool calls in one group and then only its last text; the earlier texts, their order among the calls, and the prompt it was given were dropped.

(2) **The timeline.** `ToolCall.sub` is the subagent's steps in the order they came: each text, and each run of tool calls one after another as one nested tool card. It replaces `subCalls` and `subTexts`. A text renders as markdown, like the main agent's.

(3) **The prompt.** The `prompt` input of the Task/Agent call shows first, under a "Prompt" label, folded to three lines with its line breaks run together; a tap shows it whole with its line breaks. Desktop and phone share `AgentsRow` in `thread.tsx`, so both show the same.

(4) **Thinking** stays hidden, as it is for the main agent.
