# V0.rule-digest-stable: a change's digest is the same before and after a save and load, so a `changeMoved` rule is not left true and unfired

→ SPEC sections 3, 5, 6 and 7; the `thread-rule-overdue` invariant (V0.thread-invariants).

`changeDigest` (the anchor of `changeMoved`, the built-in sync rule of a change with a PR) hashed `Conditions` and `Evidence` as they are. `dropConditions` (run by `host.restarted` on a PR that is no longer open) leaves an empty slice, which marshals as `[]`; the stored change loses it (`omitempty`) and loads as nil, which marshals as `null`. The rule fired on the restart and anchored on the `[]` digest; the next evaluation of the loaded change read as moved, held true with `LastValue=false`, and `thread-rule-overdue` reported it on every tick until some other input reduced the thread. Found by replaying a stored thread through `Engine.Load`: the anchor was the digest with `[]`, the current digest the one with `null`.

The digest now treats an empty list as nil.

Test: `TestRestartKeepsBuiltinsRuleQuietAfterSaveAndLoad` (`internal/core/thread_invariants_test.go`).
