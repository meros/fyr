# V0.jump-mark-recorded: the conversation-jumps test reads the stop a jump marked from a record, not from the brief mark

→ V0.conversation-jumps, CLAUDE.md (a flaky test is fixed at its root).

(1) **What happened.** In a `make test` run that took 258 s against the usual ~155 s, the layout test failed with "V0.conversation-jumps phone: timed out after 10000 ms waiting for previous message" and "V0.conversation-jumps desktop: Cannot read properties of null (reading 'folds')". Other runs passed.

(2) **The cause.** A jump marks its stop with `.cv-hit` for 1.6 s (`web/src/conversation-ui.tsx`): "long enough to find it, gone before it is noise". The test found the landing by polling for that class and then read it again in a second evaluate. On a loaded runner a click, a poll or the read between them can take longer than 1.6 s. If the mark is gone before a poll sees it, the wait times out (the phone). If it goes between the poll and the read, the read finds no mark and returns null (the desktop). The app was right both times: the jump had landed. Shown by cutting the mark to 50 ms: the old test then fails "timed out … waiting for next message again" on an idle machine, and the new one passes.

(3) **The fix (test only).** Before each press, the test sets up a MutationObserver on the conversation that keeps the stop that gains `cv-hit`. The wait reads that record, its place, whether it is in view and the folds in one evaluate, and the fold check uses the value the wait returned. How long the mark lasts no longer matters to the test, and the app is unchanged.

(4) **Never interrupt the owner.** No change in the app.

Checked: the section alone passes 3 of 3 with the mark at 50 ms, and 3 of 3 with the real mark next to six busy loops.
