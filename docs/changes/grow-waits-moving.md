# V0.grow-waits-moving: a thread that waits for memory to run its command is in Moving without you

→ V0.admission-tidy (1), V0.153.

The cause: an agent whose heavier command Fyr holds for memory told the owner so and ended its turn. Its row, kept in its group by V0.admission-tidy, then sat in Finished, seen, though it moves without the owner once memory suffices.

The fix: `sections()` puts a Finished (unread or seen) thread that waits to grow in Moving without you. Shared code, so desktop and phone match.

Tests: `web/test/grow.test.ts`.
