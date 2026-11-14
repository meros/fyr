# 0006 The v0 client uses the Rail direction

Date: 2026-11-14 · Status: accepted (owner) · Issue: #11 · Design: `../../design/directions/04-rail.html`

**Decision.** The desktop client follows direction 04 Rail: Geist and Geist Mono, a left rail that always shows each section's icon, count and `g1`–`g5` key, the inbox list, and the thread. Question options are keyed tiles.

**Why.** The owner chose it over the recommended Ledger. The rail keeps every section count visible, which fits a workflow of about 1,900 thread visits a day where the counts drive the next move.

**Consequences.** Three columns leave the thread the least room: the rail stays narrow (76 px) and the inbox list stays at or under 400 px. Question options are always the 2 × 2 grid of keyed tiles, as in the design; a tile has a fixed height and clamps its label and description to 2 lines (the stress frame of 04-rail shows this). Amended 2026-11-14: an earlier version of this record switched to one column for labels over 40 characters, which made the client drift from the chosen design. The client matches `design/directions/04-rail.html` in structure: inbox search field on top, thread header with tabs (Conversation, Changes, PR) and chips (state, project, branch, model, PR), grouped tool-call cards with durations, the composer as the answer field while a question is open, and empty sections collapsed to their header. The long-text rules from `design/directions/README.md` (fixed row heights, one-line ellipsis, tabular numbers, `min-width: 0`) are binding for the client.
