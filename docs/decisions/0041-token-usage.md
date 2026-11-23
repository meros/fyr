# 0041 Token usage: one metering ledger per host, exact totals from the CLI, Fyr's own overhead measured apart

Date: 2026-11-23 · Status: **accepted 2026-11-23**: nothing is built yet; the owner answered the four questions at the end · Builds on decision 0016 (one writer per record), 0020 (the one path), V0.44 (quota keyed by the auth identity), V0.90 (the runner keeps a process across a restart of fyrd), V0.115 (system agents) · Spec: a new 3.18 Usage and a change file per phase, once accepted

## Context

Fyr shows how much of a provider's quota window is used (V0.44) and no count of tokens. The owner wants to know where the tokens go: by kind of token, model, account, provider, project, thread, host and day, and how much of it Fyr itself causes (its prompt, its tools, its notices, its own model calls).

Today:
- The agent's stream goes through `agent.(*Manager).handle`. The `result` of a turn keeps only `duration_ms`. The `msg` event keeps a trimmed message (`trimMsg`) and drops `usage`.
- A system job (title, search, question, approval judge, setup, handover brief) runs `claude -p --output-format text`, which prints no usage, so those calls are not counted at all.
- The subagent count `task_progress.usage.total_tokens` is shown on the task row and kept nowhere.
- The raw protocol logs (`<data>/logs/<thread>.log`) hold every stream line, so the history can be read back.

## What the stream reports (checked against real protocol logs of claude 2.1.3xx)

| Field | Scope | Use |
|---|---|---|
| `assistant.message.usage` | one API request, as at `message_start`; one stream line per content block, all with the same usage | input side exact (`input_tokens`, `cache_read_input_tokens`, `cache_creation.ephemeral_5m/1h`); `output_tokens` is a partial count, never final |
| `result.usage` | the main agent's requests of this turn only | the main agent's share of a turn |
| `result.usage.iterations` | the last request of the turn only | not a per-request list; not used |
| `result.modelUsage[model]` | **cumulative for the session**, every model: the main agent, subagents, and the CLI's own small-model calls that no stream line shows | the exact total |
| `result.total_cost_usd`, `modelUsage[m].costUSD`, `costBasis` | cumulative, at list price | not used: usage is shown in tokens only (question 4) |

Three facts decide the design:
1. **Only `modelUsage` is complete.** In a turn with no subagent the difference of `modelUsage` between two results equals `result.usage` exactly. In a turn with subagents it is several times larger. Summing assistant lines (what transcript-based tools do) misses the CLI's internal calls, and its output counts are wrong.
2. **The cumulative counts survive a resume.** A new process that resumes the session (`--resume`, a refresh, a handover to the same CLI) reports counts that continue from the last process. A baseline per process double counts; a baseline per **session id** is right.
3. **A result can be delivered twice.** The runner keeps a process across a restart of fyrd and replays lines that were not acked (V0.90). Writing must be idempotent.

agy (ACP) returns `usage` on the answer to `session/prompt`, and `agycli` copies it to its synthesized `result`. It appears to be per turn and has the same field names, but no live run has proved its scope yet. Each adapter says its own semantics (below), and a test with a recorded fixture holds it.

## Options

**Where the numbers come from**

| | Source | Verdict |
|---|---|---|
| A | sum `assistant.message.usage`, deduplicated by message id | misses internal calls; output counts wrong. Rejected as the total |
| B | the CLI's session transcripts (`~/.claude/projects/*.jsonl`) | the same gaps as A, plus a second copy of state Fyr does not own. Rejected |
| C | `--include-partial-messages` and `message_delta` per request | exact per request, at the cost of many more stream lines on every thread. Rejected for now |
| **D** | **the difference of `result.modelUsage` per session id, plus the input side of A for attribution only** | exact totals, cheap. **Chosen** |

**Where the numbers are kept**

| | Store | Verdict |
|---|---|---|
| 1 | a `usage` field on the Thread record | the thread is rewritten on every apply and stays small. Totals by account or day would need every thread loaded. Rejected |
| 2 | `usage` events in the event log through `run.ended` | system jobs have no thread and no reduce. Type-filtered aggregation over JSON event data is slow and awkward. Rejected |
| **3** | **a `usage` table of its own, an append-only ledger written by one meter, outside the one path** | **Chosen** (question 1) |

The ledger is a measurement like the guard's learned costs (SPEC 3, "Outside the one path", item 2): no reducer reads it, it changes no record, and it decides nothing a thread does. It becomes item 4 of that list. If a later feature must act on usage (a budget Rule, a card at 80 % of a budget), it reads a rollup as an observer and sends an Input, as the quota reader does now.

## Decision (proposed)

### 1. One normalized report per adapter

```go
// internal/cli
type UsageReport struct {
    Session   string                // the CLI's session id
    Scope     UsageScope            // Cumulative (claude) or PerTurn (agy, until proved otherwise)
    Models    map[string]TokenCount // canonical model id → counts
    Main      *TokenCount           // the main agent's share, when the CLI says it (claude: result.usage)
}
type TokenCount struct {
    Input, Output, CacheRead, CacheWrite5m, CacheWrite1h, Thinking int64
    WebSearch, WebFetch int64
}
type UsageReader interface { Usage(result Msg) (UsageReport, bool) }
```

Each adapter implements `UsageReader` next to its parser. The meter never reads a CLI's JSON shape.

### 2. The meter (`internal/usage`)

- **A pure fold.** `Fold(baseline map[model]TokenCount, r UsageReport) (delta, newBaseline)`. For `Cumulative`, delta = now − baseline per model. A counter that goes down is a reset (a new session under an old id, a CLI that does not restore counts), so delta = now, as a Prometheus counter reset is handled. For `PerTurn`, delta = the report. Table tests cover resume, reset, replay, a model that appears mid-session and a fork.
- **One transaction per report.** It writes the ledger rows and the new baseline (`usage_baseline(session, data)`) together. The row id is the CLI's result `uuid` (or `<job>:<uuid>` for a system job), so a replayed result writes nothing. A fyrd restart reads the baseline from the table.
- **A fork.** When Fyr starts a session from another (handover, `--fork-session`), it seeds the new session's baseline with the parent's last counts before the first result.
- **No backfill (question 2).** Counting starts with the release that ships phase 1. The meter takes stream lines, so a later import of the protocol logs would be possible, but it is not built.

### 3. The ledger

```sql
CREATE TABLE usage(
  id TEXT PRIMARY KEY,          -- result uuid, or job:uuid
  at TEXT NOT NULL,             -- host clock
  day TEXT NOT NULL,            -- local day of the owner's zone, for rollups
  source TEXT NOT NULL,         -- turn | job:title | job:search | job:question | job:approval | job:setup | job:brief | calibration
  trigger TEXT NOT NULL,        -- owner | notice | wake | forced | background | resume | ''  (what started the turn)
  thread TEXT NOT NULL, project TEXT NOT NULL,   -- at the time; '' for a job with no thread
  provider TEXT NOT NULL,       -- provider id (3.16)
  account TEXT NOT NULL,        -- the auth identity of V0.44: two providers on one login count as one account
  billing TEXT NOT NULL,        -- subscription | api-key | endpoint
  adapter TEXT NOT NULL, session TEXT NOT NULL,
  model TEXT NOT NULL, effort TEXT NOT NULL,
  part TEXT NOT NULL,           -- main | other (subagents and the CLI's own calls: modelUsage − result.usage)
  input INTEGER, output INTEGER, cache_read INTEGER, cache_write_5m INTEGER, cache_write_1h INTEGER,
  thinking INTEGER, web_search INTEGER, web_fetch INTEGER,
  fyr_version TEXT NOT NULL);
CREATE INDEX usage_day ON usage(day);
CREATE INDEX usage_thread ON usage(thread, at);
```

There is one row per turn, model and part. The dimensions are copied into the row when it is written, because a thread moves between projects and a provider is renamed or deleted. No money is stored or shown (section 6).

### 4. Fyr's own overhead, in four measured parts

Overhead is tokens that Fyr causes and the owner did not ask for. Each part says how exact it is.

| Part | What | How measured | Exact? |
|---|---|---|---|
| **O1 System jobs** | title, search, question, approval judge, setup, handover brief | `Printer.Print` returns `(string, UsageReport, error)`; claude runs with `--output-format json`; agy reads its result. Rows with `source = job:*` | exact |
| **O2 Fixed context** | Fyr's appended system prompt, its MCP server's instructions and tool schemas, its hooks' text, present in **every** request | **calibration**: once per (Fyr prompt hash, MCP schema hash, CLI version, model), run the real launch with a trivial prompt twice: once as Fyr starts a thread, once without Fyr's prompt and MCP server. The difference in the first request's input side is S tokens. The two runs are rows with `source = calibration`, which are overhead too. Then per turn: requests × S as cache-read tokens, and S as cache-write tokens on each request whose `cache_read` is smaller than S (a cold cache) | S is exact for that build and model; the per-turn split is exact on the input side |
| **O3 Fyr tool I/O and injected text** | `mcp__fyr__*` calls and their results; `[Fyr]` notices, wake notes, the Stop and PostToolUse hooks' additional context | per session, a running sum R of the estimated tokens of Fyr's blocks still in context (bytes × the session's own tokens-per-byte, which the exact input side of each request gives). Each later request adds R cache-read tokens. R drops to 0 at a compaction | estimate, with the method named in the row |
| **O4 Fyr-started work** | turns Fyr started without a message from the owner: forced turns that only update the view (V0.37), wake-ups, notices | the `trigger` column. Only `forced` counts as overhead; a CI-failed notice that starts a fix is work, and is shown as a trigger, not as overhead | exact |

The schema-size estimate is not used for O2 because the CLI may defer MCP tools (tool search) or not, so the bytes of the schemas say little about what reaches the model. Calibration measures what the CLI actually sends. O2 and O3 go in a second table, `usage_overhead(usage_id, part, kind, tokens, method)` (kind: cache-read, cache-write, input, output), so a better method can recompute them without touching the measured ledger.

Per-request input-side facts (deduplicated by message id: requests, cold requests, the sum of context, Fyr's resident R) are gathered in the session between results and written with the turn's rows. They are never stored per request.

### 5. Reads and the fronts

- **Host.** `GET /api/h/<host>/usage?from&to&group=day,model,account,…&filter=…` returns additive rollups (sums of counts, never averages), plus `overhead` by part. Group keys: `day, model, provider, account, billing, project, thread, source, trigger, part, adapter, host`.
- **Front.** It asks each host in its roster, merges the rollups by key (they add up), groups linked projects (`front.projectLink`) as one, and serves an offline host's last rollup with `X-Fyr-Read-At`, as for threads (3.17). One writer per host (0016): no host writes another's usage.
- **Live.** After each write the meter publishes `usage.delta {thread, counts}` on the hub. The thread header's total and an open Usage page update without a poll.
- **Surfaces, desktop and phone alike.** A thread total in the thread header. A Usage page under Settings: a range, a group-by, a stacked bar by day, a table you can sort, and "Fyr overhead" as its own breakdown (O1 to O4 by job and part).

### 6. Tokens only (question 4)

The page shows tokens by kind (input, output, cache read, cache write 5 min and 1 h, thinking) and requests, never money: a subscription is not billed per token, and a price table would be a second thing to keep right. A total that mixes kinds ("tokens") is labelled as a plain sum, since a cache-read token and an output token weigh very differently against a quota. Cost or quota share can be added later from the same counts, without a migration.

### 7. Phases

1. The meter, the ledger, claude's `UsageReader`, the trigger column, a thread total. Tests: the fold table, a recorded-log replay that compares against the CLI's own cumulative counts, a restart mid-turn, a duplicate result.
2. System jobs through `--output-format json` (O1); agy's reader, proved by a live run and pinned by a fixture.
3. Rollup reads, the front merge, the Usage page on both fronts.
4. Overhead O2 (calibration) and O3, in their own table.

Each phase is its own change file in `docs/changes/`.

## Not chosen

- **A cost limit that stops a thread.** It needs the ledger first, and then a Rule (7.2), not a meter that acts.
- **Per-request rows.** They add volume with no breakdown that a turn row cannot give. The protocol logs keep the requests for a rewrite.
- **Counting tokens with the provider's count-tokens endpoint.** It needs an API key, and a login account has none. Calibration works on every account.

## Questions for the owner (answered 2026-11-23)

1. **Outside the one path?** Yes: the ledger is a measurement, item 4 of "Outside the one path" (SPEC 3), as proposed.
2. **History?** Start at the release; no backfill from the protocol logs.
3. **What counts as overhead?** System jobs, fixed context, Fyr tool I/O and injected text, and forced turns (O1 to O4). Turns a notice or wake-up started are shown by trigger, not as overhead.
4. **Money?** Tokens only: no API-equivalent $ and no quota share on the page.
