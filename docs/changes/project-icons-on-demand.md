# V0.project-icons-on-demand: image icons show again, and the owner can change a project's icon from any thread

→ V0.116, V0.179, decision 0031, decision 0034. *Amends V0.116 (an agent never replaces the owner's value): it may when the owner's newest message asks for it.* (owner, 18 Nov, with a screenshot of the project chips: "the project icons don't work anymore", "Only emojis", and "make sure the project setup can be run on demand from any thread: change the icon to this one".)

(1) **The bug.** Since V0.179 a project's id in the client is its ref, `<hostId>/<id>`. The icon URL sent the ref to the host, which answered 404, so every image fell back to its glyph on both fronts. The URL now carries the host's own id.

(2) **`ownerAsked`.** `project_setup` takes `ownerAsked: true` for a change the owner just asked for, from any thread on the host and for any project on it. The icon, name and colour given then replace the owner's values and become the owner's. A colour taken from the icon stays the agent's and does not replace one the owner chose.

(3) **What is checked.** A thread records the person behind its newest message (`lastAsker`: a `device:` or `local:` actor). A message from Fyr itself or from another agent clears it, and `ownerAsked` is then refused. Fyr checks who wrote the newest message, not what it says; the agent answers for reading it.

(4) **Linked projects (decision 0034).** Name and colour stay the link's and the reply says so. The image is stored on this host, and the reply says that another host's image may be the one shown.
