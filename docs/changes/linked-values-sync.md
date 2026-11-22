# V0.linked-values-sync: a setup run the owner asked for sets every field, and a linked project's image reaches every host

→ V0.116, V0.117, V0.project-icons-on-demand, decision 0034. *Amends V0.116 (2) and (6) (a setup run keeps the owner's values) and decision 0034 (the image is not copied).*

Two problems. A setup run kept every field the owner had set, so pressing Set up automatically after removing an image, or a run for a project whose name the owner just typed, set nothing ("Kept the name: the owner set it."). And a linked project's image lived only on the host that stored it: a member whose own record had no image borrowed another host's picture, which failed while that host was offline, so the icon showed on one device and not on the others.

(1) **A run the owner asked for sets all.** A run started by New project (a project made with a folder) or by Set up automatically is recorded with `setup.all`. In that run's own thread, `project_setup` acts as if the owner had asked (`ownerAsked`): the icon, name and colour it gives replace what is there and become the owner's, the colour taken from the icon too. The brief says so. The pass for projects from before (V0.116 (6), V0.117) is not recorded with `all` and keeps the owner's values. Another thread gains nothing from the flag.

(2) **The image has its own stamp.** `imageAt` says when a record's image was set or removed. The front takes, for each link, the member whose `imageAt` is newest, and sends every online member with an older `imageAt` that image: the host kind `project.sharedImage {id, link, at, image, by, png?}` carries the picture (the source's 128 px square, base64), the host stores it as an upload and takes the source's `imageAt`; an older stamp changes nothing and stores no file; `image: ""` removes the image and its files. A picture is read from its own host, so it is copied while that host is online; a removal needs no source. The stamp is in the command id, as for the shared values. A name edit moves `sharedAt` only and copies no picture.

(3) **The client.** An upload or Remove image on a linked project goes to one record (this host's, else the first online member); the front copies it to the others, also to one that is offline now once it is back. A linked project shows the image of its online member set last, a removal included; of equals one with an image, then the first in host order.

(4) **Upgrade.** A record from before keeps its `imageAt`. An image with no `imageAt` loses to a later removal on another member: that removal is the newest value set. A host from before this change answers `project.sharedImage` as an unknown action; the front logs it and tries again on the next frame.

Tests: `internal/engine/setup_test.go` (TestProjectSetupRunSetsAll, TestAutoSetupOnCreateSetsAll, TestProjectSharedImage), `internal/front/projectlink_test.go` (TestProjectSharedImageReachesEveryHost), `web/test/linked-projects.test.ts` (a removal set last).
