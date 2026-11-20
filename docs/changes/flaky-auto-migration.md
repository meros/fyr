# V0.flaky-auto-migration: TestAutoProjectMigration waits for its runs to end before it writes the store behind the engine

→ a test fix; amends no behaviour.

`TestAutoProjectMigration` failed on CI (PR #431): "the auto project with a thread: {ID: …}" — thread b had no project after the restart.

1. The cause is a race in the test. It starts two threads, then writes thread b with `ProjectID = "p_app"` straight into the store, outside the engine. The engine still holds b in memory with no project. When b's run ended after that write, the engine wrote b again and dropped the project.
2. Found: 2 failures in 200 runs alone beside four busy loops; the message is the CI one.
3. Fix: the test waits for both threads to be done before it touches the store. Then 400 runs under the same load all pass.
