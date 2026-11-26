# V0.shared-edit-after-link: a shared edit right after a link reaches every member the link just joined

→ amends V0.211 (decision 0034; `front.projectLink`, `front.projectUnlink`), V0.linked-values-sync and V0.link-writes-through-a-stale-copy. *A flaky `TestProjectSharedPriority`: `high: a="" b="high"` in `make test`, and `a="high" b=""` under load.*

`front.projectShared` writes the new values to each online member of the link, and it finds the members in the front's own copy of each host's records. That copy is what the host's stream last carried, a frame behind the host. `projectLinkCommand` stored the link on each host but left the front's copy without it until that host's next frame. A shared edit sent in between found one member, wrote only that host, and answered 200 with `stored:[TEST-A]`. The other member got the values only later, from the catch-up in `syncLinksLocked`. With 8 busy loops beside `-count=200` this happened in 2 of 200 runs. The debug reply showed `stored` naming one host, and the front's member list had 1 entry.

So the 200 is meant to mean "stored on every online member", and the front answered from a copy that lacked the link it had just written. That was a race in the code, not a slow wait in the test. The test's wait (`frontSees`) read `/api/h/<host>/state`, which the front forwards to an online host. It saw the host's record, not the copy the edit reads, so it could not cover the gap.

(1) **The front notes what it just stored.** After a host answers `project.link`, `noteLink` puts that link (or no link, for `front.projectUnlink`) into the front's copy of the record. `noteShared` already does this for the shared values, and a fresh link now also notes the values it wrote. The host's event, when it comes, is the same record.

(2) **The test.** `TestProjectSharedPriority` (`internal/front/projectlink_test.go`) checks, with no wait, that the front's copy holds the link on both records once the link is answered (`copyLink`). It also checks that the edit's reply names both hosts in `stored`. The check that the copy holds the link fails without (1) under load; with (1), 200 of 200 runs pass under the same load.
