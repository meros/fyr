# V0.link-writes-through-a-stale-copy: a project link is written to every named host, also one this front already shows with the link

→ amends V0.211 (decision 0034; `front.projectLink`). *A flaky `TestProjectLink` on main: `relink: 200 … stored:[TEST-A TEST-B], b ""`.*

The front's copy of a host's records is what the host's stream last carried, and a stream can be a frame behind the host: a host that lost the link on its own (its settings page, a restore, an older build) still showed it to the front. `projectLinkCommand` read that copy and skipped the write to a member it showed with the link already, so the reply said the host stored the link while the host's record had none.

(1) **Every named host is written.** A member the command names gets the link, whether or not the front's copy shows it with that link; `project.link` on a record that has the link already reduces to the same state. `front.projectUnlink` writes the same way: it sends the empty link to the host, whether or not the front shows the record with one. A link is stored on the host, and only the host's answer says it did.

(2) **The test the flake hit.** `TestProjectLinkThroughAStaleCopy` (`internal/front/projectlink_test.go`) takes the link from b's record behind the front's back and puts it back in the front's copy only, then relinks: b ends with the link. It does the same for the unlink. It fails without (1) with the same shape CI saw. `testFront` (`front_test.go`) hands a test the `Front` behind its URL.

This was a product bug, not a slow wait: the test was right to expect the host to have the link its command was answered for. The helper waits from V0.old-routes-end stay; they cover the other half, a project the front has not seen yet.
