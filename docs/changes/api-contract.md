# V0.api-contract: no Api call rejects; a read that can fail answers a Result, and an upload gives up when it stalls

→ V0.web-review-fixes (2), V0.18, V0.127. *Amends V0.web-review-fixes (2): the reads that still threw now answer a failed Result.*

The client's Api had two contracts: the commands, the writes and some reads resolved `{ ok: false }` or an empty value, while the folder picker's calls, the push key, the handover plan, the events pages, the Changes tab, the slash commands, the settings reads and the AI search threw. Each caller had to know which, and a missed catch was an unhandled rejection. The attachment upload had no time limit and left its cancel listener on the signal.

1. **One contract**, written at the top of `web/src/api.ts`: no method rejects. A read whose caller shows why it failed answers `Result<T>`: `{ ok: true, data, readAt? }` or `{ ok: false, error }`. A read whose caller has nothing to show answers its empty value (`null` for one record, `[]` for a list over the hosts). A command, a write and an upload answer `{ ok, error }`. One `read` makes every Result: a network failure or a timeout is "Fyr is not reachable", a refusal the host's error, a body the read cannot use (no push key, no folder made, no handover plan) the decoder's reason.
2. **Callers** read `ok` instead of catching: the folder picker, the Changes tab, Settings (providers, auto-deploy, updates, auto-merge, system agents, setup, devices, hosts), the slash menu, the handover form, push sign-up, the AI search and the timeline loads. The mock Api keeps the same contract.
3. **Upload** (`upload` in `web/src/http.ts`): a send that moves no byte and gets no answer for 30 s (`REQUEST_TIMEOUT_MS`) is given up and the chip says the host stopped answering; the time counts from the last progress, so a large file on a slow link is not cut. The cancel listener is removed when the upload ends, however it ends.
4. **Step label** (`mobile.ts` `stepLabel`): one return instead of two branches with the same words.

Tests: `web/test/http.test.ts` (every Result read with a network failure and with a refusal, the bodies a read cannot use), `web/test/upload.test.ts` (answer, stall on the test's clock, progress keeps it alive, cancel, network error, the listener removed).
