# web: notes for the v0 client

Build with `npm ci && npm run build` (output in `dist/`, embedded by the Go binary). `npm run typecheck`, `npm test` (node:test over the pure modules), `npm run test:layout` (Playwright layout suite over both desktop and phone). `node scripts/build.mjs --dev` builds unminified with inline source maps.

Mock mode (the fixtures are a chunk, `mock-api-*.js`, that `api.ts` loads with `import()` only when `?mock=` is set; `test/bundle.test.ts` checks that the start bundle has none of it): `dist/index.html?mock=rail` is the baseline fixture on the design's clock (14:32 today). `?mock=1` serves the stress fixtures from `src/mock.ts` (no host needed); `?mock=2` is a sparse set with empty sections. `&open=<threadId>` opens a thread and `&dialog=new|adopt|snooze|settings` opens a dialog, for screenshots. `&dialog=settings&page=automation` (or `providers/<id>`, `providers/new`, `projects/<id>`, `device`, `setup`) opens Settings at a page on the desktop; `?settings=providers/<id>` is the same screen on the phone. Settings states for screenshots and the layout test: `&providers=fail`, `&providers=none`, `&providers=nokey`, `&conn=offline`. Every command the mock receives is in `window.__fyrCommands` (`{action, threadId, args}`).

## Remaining contract notes (`docs/API-v0.md`)

1. **Needs-you and Working order.** Needs you orders by urgency group (`answer`, `approve`, `unblock`) then activity; Working orders by `runStartedAt` rather than stable slot numbers (SPEC U14).
2. **`answers` keyed by question text.** Two questions with the exact same text collide; a multi-select label that contains `", "` is ambiguous after joining.
3. **User images are a count.** `user` events carry `images: N`, so the log shows a chip rather than the inline image.
4. **Settled list size.** The client renders the first 60 rows and a "Show all" button rather than a server-side 24 h cap.
5. **Section names.** The API uses `needs`/`archived`; SPEC 6.1 uses `attention`/`hidden`. The client follows the API.
6. **⌘⏎ queue.** While a question card is open, ⌘⏎ sends the composer text with `thread.send` (the CLI queues it) instead of answering.

## Not in this client yet

- `⌘K` focuses the search field (filtering by title, project and PR number); a full command palette and `⌘[` are not in v0 (`⌘1`–`⌘9` stable slots were dropped in M5).
- The theme bootstrap in `index.html` is an inline script; a strict CSP from fyrd would need a hash for it.

## PWA and push (`SPEC V0.15`, `V0.mac-notify`, `V0.linux-desktop`)

- The build emits `manifest.webmanifest`, `sw.js` and `icons/` (drawn by `scripts/icons.ts`). The service worker's cache is named after a hash of the shell it precaches (every script chunk from the esbuild metafile included); it never handles `/api` or `/mcp`.
- `fyrd` serves `/sw.js` (`text/javascript`) and `/manifest.webmanifest` (`application/manifest+json`) from the root, and `/` returns `index.html`.
- Native desktop shells (`Fyr.app` on macOS, `fyr-desktop` on Linux) inject `window.fyrPush`, bypassing Web Push and reading queued pushes from `owner.sock` (`/push/native`, `/push/native/stream`).
- Notification taps post `{type:"open", threadId}` to an open window, else open `/?open=<id>`.
