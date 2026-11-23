# V0.flaky-project-add: TestProjectAdd waits until the front sees both members linked before it adds a second member

→ V0.flaky-rollback-seed (a flaky test is fixed at its root).

(1) **The cause.** `front.projectAdd` reads the link of the project it adds to from the front's own copy of that host's projects. The copy is filled by the host's stream. The test waited until the front saw the new member on host b with the link, then asked to add a second member on b and expected 409 ("already has this project"). It did not wait for the front to see the link on host a's project. When a's stream came in after b's, the front still read a's project as unlinked, made a new link, and created a second member on b: 200 instead of 409. CI failed this way once on a busy runner. Run alone (`-count=30`) and beside six busy loops (`-count=60`) it passed every time: the window is a few milliseconds.

(2) **The fix.** The test waits until the front sees both members linked, as the file's other link tests already do (`front sees both`). The command's behaviour is unchanged. It answers from the front's copy, which the host streams update, and the owner's add of a second member comes long after the first add's streams have arrived.

Tests: `internal/front/projectlink_test.go` (TestProjectAdd).
