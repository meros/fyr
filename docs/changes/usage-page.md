# V0.usage-page: Settings → Usage and today's tokens on the status home, from every host's usage ledger

→ V0.usage-ledger, V0.usage-jobs, decision 0041 (phase 3), SPEC 3.18, API-v0. The ledger had no reader but the thread chip: the owner could not see where the tokens go by model, account, project or source, nor how much of it is Fyr's own calls.

(1) **`GET /api/usage`** sums the host's ledger by the asked keys over an inclusive range of the host's local days, with key = value filters (`usage.ParseQuery`: an unknown key or a malformed day is a 400), and names what it counts (`Meter.Answer`): thread titles and project names with their link from the engine (`ThreadTitles`, `ProjectNames`), provider names, and each account by the names of the providers on it, so an account id (a hash) is never shown and no login folder leaves the host. A new key, `hour`, is a row's UTC hour (`substr(at, 1, 13)`), which a client turns into its own local hour.

(2) **The front's merge** (`web/src/usage.ts`, `mergeUsage`). The client asks each online host and adds the sums by key: a model, a day, a source, a trigger, a part or a billing is one row everywhere; a linked project (decision 0034) is one row across its hosts; a thread, a provider and an account stay their host's, named with the host when there are several. A host that does not answer is named under the page's total.

(3) **Settings → Usage**, one page for both fronts (`settings-usage.tsx`, through the shared `pageView`): a range (today, 7, 30, 90 days), a key to group by, the range's total with each kind, one bar per day, the rows by the key (largest first, each with a thin bar of its share), and Fyr's own calls (every source but agent turns) with their share of the range. A tap on a row narrows the page to it, by its own value on each host (`perHostWhere`: a project's id is its host's; a model is asked of every host); a thread's row opens the thread; Show all clears it. Tokens only.

(4) **The status home** shows today's tokens: the total so far, the busiest hour and one bar per local hour from midnight to now (the hour now lighter: it is still filling), read again each minute. The same block on both fronts; none before any host counted a token today, or on a front that cannot read usage.

(5) **The bars** (`UsageBars`, shared) are one series, the total, in one hue with a 2 px gap and rounded data ends, no legend; each bar names its time and count on hover, a few hours or days are labelled under their bars, and the page's rows are the table view.

(6) **A read loop, fixed before it shipped.** The page's read first depended on the API object's identity; the Settings shell hands a new one on each render, so every answer started the next read and the page never settled. Each read now follows its query only (the API is held in a ref).

Tests: `internal/usage/view_test.go` (TestParseQuery, TestAnswerNames, TestRollupByHour), `internal/server/usage_test.go` (TestUsageEndpoint), `web/test/usage.test.ts` (the merge, linked projects, per-host providers, models, Fyr's own calls, ranges, today's hours, decoding); the layout test's new screen "settings usage" at three phone sizes, the status screen with today's tokens, and the Settings nav with eight pages on both fronts.
