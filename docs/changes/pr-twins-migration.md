# V0.pr-twins-migration: stored changes that hold one PR fold once more, as a store migration

→ SPEC sections 3, 5, 6 and 7; completes V0.thread-change-pr-twins and V0.versioned-store.

Evidence: `thread-change-invalid` fired at load with `duplicate PR URL <url> across changes`, on every start, after V0.thread-change-pr-twins had shipped.

(1) **Cause.** V0.thread-change-pr-twins put `FoldSamePR` in the load passes (`core.Migrate`). Since V0.versioned-store those run once per store, as migration 1, so a store already at version 1 never folded its stored twins.

(2) **Fix.** Migration 2, `foldPRTwins`, runs `core.FoldSamePR` on every thread once and stores what it changed.

Tests: `internal/store/migrate_test.go` (TestOpenFoldsStoredPRTwinsAtVersionOne).
