# 0006 The v0 client uses the Rail direction

Date: 2026-11-14 · Status: accepted (owner) · Issue: #11 · Design: `../../design/directions/04-rail.html`

**Decision.** The desktop client follows direction 04 Rail: Geist and Geist Mono, a left rail that always shows each section's icon, count and `g1`–`g5` key, the inbox list, and the thread. Question options are keyed tiles.

**Why.** The owner chose it over the recommended Ledger. The rail keeps every section count visible, which fits a workflow of about 1,900 thread visits a day where the counts drive the next move.

**Consequences.** Three columns leave the thread the least room: the rail stays narrow (76 px) and the inbox list stays at or under 400 px. The 2 × 2 option grid reads poorly when one option is long: when any option label is over 40 characters, the tiles fall back to one column. The long-text rules from `design/directions/README.md` (fixed row heights, one-line ellipsis, tabular numbers, `min-width: 0`) are binding for the client.
