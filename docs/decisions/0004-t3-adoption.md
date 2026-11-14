# 0004 Adopt open T3 threads by Claude session id

Date: 2026-11-14 · Status: accepted · Spec: 0.4 (cut-over), 0.5

**Context.** The owner has open threads in T3 Code. v0 should take them over so T3 can close.

**Findings.** T3 v0.1.7 stores threads in `statev2.sqlite`. `orchestration_v2_projection_threads.payload_json` has the title, `worktreePath`, `settledAt`, `snoozedUntil`, `archivedAt` and `lineage.parentThreadId`. The Claude session id is `nativeThreadRef.nativeId` in `orchestration_v2_projection_provider_threads.payload_json`. The provider instance maps to a `claude` wrapper binary in T3's `settings.json`. The transcript is `~/.claude*/projects/<cwd slug>/<session id>.jsonl`.

**Decision.** `fyr import-t3` (and the matching UI action) reads T3 read-only and adopts open top-level threads: title, cwd, profile, model, Claude session id, the latest PR, and the last 400 transcript messages as history. The next message resumes the same Claude session with `--resume`.

**Consequences.** A thread must not run in both apps at once: two resumes of one session fork it. Fyr marks adopted threads with their T3 id and skips them on a second import. Subagent child threads and settled threads are not adopted.
