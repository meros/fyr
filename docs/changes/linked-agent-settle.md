# V0.linked-agent-settle: a linked project shares "Agent says done" between its hosts, like its priority

→ amends V0.settle-request and decision 0034 (linked projects).

The owner: "Changing this doesn't persist." On a project linked across hosts, the project page saved "Agent says done" to the one host record the page edits, while the page could show another member's record; the press looked lost, and each host settled its threads by a different rule.

1. `agentSettle` is a shared value of a link, as name, colour, glyph icon, quiet hours and priority are: the page sends it with `front.projectShared`, the front writes it to every online member, stamped, and an offline member catches up when it is back. null is ask.
2. The front refuses a value other than ask, auto or null before any host is written.
3. The linked project in the lists shows its newest member's value.
4. A link whose members already differ keeps that until the next shared change: any press on the page writes the newest values to every member.
