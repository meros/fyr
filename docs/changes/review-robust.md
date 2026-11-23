# V0.review-robust: smaller robustness fixes from the review of main

→ V0.review-hangs, V0.json-helpers, V0.147.

Why: the same review found places that answer the wrong status, grow without bound, leave half-written files, hide errors or cut text in the middle of a character.

(1) **MCP.** A JSON-RPC body is read with `httpguard.DecodeJSON`: one over the 4 MiB cap is answered 413, not 400. The map of which session token read the tool list dropped no token: when a new token reads it, the tokens `auth` no longer knows (ended sessions) are pruned.

(2) **Stall dumps.** A dump is written with `fsutil.WriteAtomic`, so `Latest` never reads half of one, and `Latest` reads a dump's first four lines instead of the whole multi-megabyte file.

(3) **Attachments.** `Save` checks the file's `Close` (a full disk can show only there) and removes the attachment's folder when `meta.json` cannot be written.

(4) **LAN bus file.** `discovery.File` returns an error for a file it cannot read or parse; a missing file is still no candidates. The peer page logs the error.

(5) **pmset.** The battery check runs `pmset` through `proc.Command` with a 3 s context.

(6) **Project icons.** `projicon` keeps one client (and its connections) for every fetch instead of a transport per fetch.

(7) **Store.** `Write` returns no events when the commit fails, so nothing is published for a write that did not happen. The database URI is built with `net/url`, so a `?`, `#` or `%` in the data folder's path stays in the file name.

(8) **Login lines.** The last error line of a login is cut with `cli.Clip`, by runes, not bytes.
