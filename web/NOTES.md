# web: notes for the v0 client

Build with `npm ci && npm run build` (output in `dist/`, embedded by the Go binary). `npm run typecheck`, `npm test` (node:test over the pure modules: `store`, `seen`, `question`, `buildid`, `conversation`, `markdown`). `node scripts/build.mjs --dev` builds unminified with inline source maps.

Mock mode: `dist/index.html?mock=1` serves the stress fixtures from `src/mock.ts` (no host needed); `?mock=2` is a sparse set with empty sections. `&open=<threadId>` opens a thread and `&dialog=new|adopt|snooze` opens a dialog, for screenshots.

## Contract questions (docs/API-v0.md)

Nothing here blocks the client; each item says what the client does today.

1. **No ordering field on ThreadDTO.** The client keeps the newer of two versions by `updatedAt` (equal replaces, because attention can change with time alone). This is only safe if `updatedAt` is strictly increasing per thread, including for time-driven upserts. A monotonic `rev` would remove the doubt.
2. **markSeen has no watermark.** SPEC 6.4 sends `thread.visit(eseq)`; the API's `thread.markSeen` has no args. A result that lands between the client's decision and the command is marked seen without having been shown. Suggest `args: {seq}` (the last event seq the client rendered) and the host ignores it when newer results exist.
3. **Needs-you order.** The brief says asking, failed, blocked, done, then newest `activityAt`. SPEC 6.5 says oldest-in-state first (a queue) and stable slots for Working. The client follows the brief; Working is ordered by `runStartedAt`, not by slot.
4. **Undo is client-side inverse commands.** There is no deferred action in the API, so undo sends `thread.unsettle` / `thread.unarchive` / `thread.unsnooze`. Open: does `thread.unsettle` also disarm `settleWhenDone` / `settleWhenMerged` when `e` armed them on a running thread? The client assumes yes.
5. **`answers` keyed by question text.** Two questions with the same text collide; a multi-select label that contains ", " is ambiguous after joining. Rare, but a positional array would be exact.
6. **No backward paging of events.** `after=0` returns the newest 400. A longer thread loses its start; there is no `before=` to load earlier events. The client shows what it gets.
7. **No branch on ThreadDTO.** The Rail design shows a branch chip in the header; the client shows project, profile dot, model and PR only.
8. **User images are a count.** `user` events carry `images: N`, so the log shows a chip, not the picture.
9. **Settled list size.** The client does not filter Settled to 24 h (SPEC 6.5); it renders the first 60 rows and a "Show all" button. If the host sends thousands, a server-side cap would be better.
10. **Section names.** The API uses `needs`/`archived`; SPEC 6.1 uses `attention`/`hidden`. The client follows the API.
11. **Errors.** The client assumes `409` and `400` replies have the body `{ok:false, error}`; other statuses are shown as `HTTP <code>`.
12. **buildId (V0.13).** Implemented: the first `buildId` seen (hello or state) is remembered; a different one after a reconnect reloads the page once; a missing one never reloads.

## Not in this client yet

- U4 (rows stay in place until focus leaves the section) is not implemented: rows move on the next upsert.
- `⌘K` palette, `⌘1`–`⌘9`, `⌘[`, away strip: not in v0.
- The theme bootstrap in `index.html` is an inline script; a strict CSP from fyrd would need a hash for it.
