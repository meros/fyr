# web: notes for the v0 client

Build with `npm ci && npm run build` (output in `dist/`, embedded by the Go binary). `npm run typecheck`, `npm test` (node:test over the pure modules: `store`, `seen`, `question`, `buildid`, `conversation`, `markdown`). `node scripts/build.mjs --dev` builds unminified with inline source maps.

Mock mode (the fixtures are a chunk, `mock-api-*.js`, that `api.ts` loads with `import()` only when `?mock=` is set; `test/bundle.test.ts` checks that the start bundle has none of it): `dist/index.html?mock=rail` is the exact content of the first light frame of `design/directions/04-rail.html` on the design's clock (14:32 today), for side-by-side comparison. `?mock=1` serves the stress fixtures from `src/mock.ts` (no host needed); `?mock=2` is a sparse set with empty sections. `&open=<threadId>` opens a thread and `&dialog=new|adopt|snooze|settings` opens a dialog, for screenshots.

## Contract questions (docs/API-v0.md)

Nothing here blocks the client; each item says what the client does today.

1. **No ordering field on ThreadDTO.** The client keeps the newer of two versions by `updatedAt` (equal replaces, because attention can change with time alone). This is only safe if `updatedAt` is strictly increasing per thread, including for time-driven upserts. A monotonic `rev` would remove the doubt.
2. **markSeen has no watermark.** SPEC 6.4 sends `thread.visit(eseq)`; the API's `thread.markSeen` has no args. A result that lands between the client's decision and the command is marked seen without having been shown. Suggest `args: {seq}` (the last event seq the client rendered) and the host ignores it when newer results exist.
3. **Needs-you order.** The brief says asking, failed, blocked, done, then newest `activityAt`. SPEC 6.5 says oldest-in-state first (a queue) and stable slots for Working. The client follows the brief; Working is ordered by `runStartedAt`, not by slot.
4. **Undo is client-side inverse commands.** There is no deferred action in the API, so undo sends `thread.unsettle` / `thread.unarchive` / `thread.unsnooze`. Open: does `thread.unsettle` also disarm `settleWhenDone` / `settleWhenMerged` when `e` armed them on a running thread? The client assumes yes.
5. **`answers` keyed by question text.** Two questions with the same text collide; a multi-select label that contains ", " is ambiguous after joining. Rare, but a positional array would be exact.
6. **No backward paging of events.** `after=0` returns the newest 400. A longer thread loses its start; there is no `before=` to load earlier events. The client shows what it gets.
7. **No branch on ThreadDTO, no host name in the state.** The client reads optional `branch` (ThreadDTO) and `host` (state) and shows the branch chip and the rail's host label only when they are sent; without `host` the rail says `local`. The Changes tab's `+42 −18` count needs a diff stat on the thread.
8. **User images are a count.** `user` events carry `images: N`, so the log shows a chip, not the picture.
9. **Settled list size.** The client does not filter Settled to 24 h (SPEC 6.5); it renders the first 60 rows and a "Show all" button. If the host sends thousands, a server-side cap would be better.
10. **Section names.** The API uses `needs`/`archived`; SPEC 6.1 uses `attention`/`hidden`. The client follows the API.
11. **Errors.** The client assumes `409` and `400` replies have the body `{ok:false, error}`; other statuses are shown as `HTTP <code>`.
12. **buildId (V0.13).** Implemented: the first `buildId` seen (hello or state) is remembered; a different one after a reconnect reloads the page once; a missing one never reloads.

13. **Working order.** The design lists Working in slot order (SPEC U14); the API has no slot, so the client sorts by `runStartedAt` as the brief says, and `?mock=rail` shows the four working rows in reverse of the frame.
14. **⌘⏎ queue.** While a question is open, ⌘⏎ sends the composer text with `thread.send` (the CLI queues it) instead of answering. There is no separate queue action in the API.

## Not in this client yet

- U4 (rows stay in place until focus leaves the section) is not implemented: rows move on the next upsert.
- The away strip ("Since 14:02 …") of the design is not in v0 (SPEC 0.5). `⌘K` focuses the search field, which filters the inbox by title, project and PR number; the palette, `⌘1`–`⌘9` and `⌘[` are not in v0.
- Key hints show only on buttons and in a bar that `?` toggles (the bar also holds the theme switch: auto, light, dark).
- The theme bootstrap in `index.html` is an inline script; a strict CSP from fyrd would need a hash for it.

## PWA and push (SPEC V0.15)

- The build emits `manifest.webmanifest`, `sw.js` and `icons/` (the PNGs are drawn by `scripts/icons.ts`, no dependencies). The service worker's cache is named after a hash of the shell it precaches (every script chunk from the esbuild metafile included); it never handles `/api` or `/mcp`.
- fyrd must serve `/sw.js` (JavaScript MIME type) and `/manifest.webmanifest` (`application/manifest+json`) from the root, and `/` must return `index.html`, which now uses absolute asset paths.
- The bell reads the browser's own subscription. If the host deletes a subscription (404/410 from the push service), the bell still shows "on" until the user turns it off and on; a `GET /api/push/subscriptions` (or a status in the subscribe reply) would let the client show it.
- Notification taps post `{type:"open", threadId}` to an open window, else open `/?open=<id>`.
