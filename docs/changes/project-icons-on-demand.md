# V0.project-icons-on-demand: image icons show again, and the owner can change a project's icon from any thread

→ V0.116, V0.179, decision 0031, decision 0034. *Amends V0.116 (an agent never replaces the owner's value): it may when the owner's newest message asks for it.* 

(1) **The bug.** Since V0.179 a project's id in the client is its ref, `<hostId>/<id>`. The icon URL sent the ref to the host, which answered 404, so every image fell back to its glyph on both fronts. The URL now carries the host's own id.

(2) **`ownerAsked`.** `project_setup` takes `ownerAsked: true` for a change the owner just asked for, from any thread on the host and for any project on it. The icon, name and colour given then replace the owner's values and become the owner's. A colour taken from the icon stays the agent's and does not replace one the owner chose.

(3) **What is checked.** A thread records the person behind its newest message (`lastAsker`: a `device:` or `local:` actor). A message from Fyr itself or from another agent clears it, and `ownerAsked` is then refused. Fyr checks who wrote the newest message, not what it says; the agent answers for reading it.

(4) **Linked projects.** *Amends decision 0034 (`project_setup` cannot change the shown fields; the image shown is the first online member's).* With `ownerAsked`, a new name or colour is stored on this host's record with a new `sharedAt`. The newest stamp already wins, so the front passes the values to the other members, and an offline member catches up when it is back. A record's `imageAt` says when its image was set. The image is still not copied: a linked project shows the newest image of its online members, of equals the first in host order, so a new image is not shown while its host is offline. Without `ownerAsked` an agent still cannot change a linked project.
