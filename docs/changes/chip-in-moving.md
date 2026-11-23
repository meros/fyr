# V0.chip-in-moving: a row in Moving shows its pull request chip

→ V0.ci-wait-moves (a thread waiting for CI is in Moving), V0.100 (the row shape). *Amends the rule that a working row has no PR chip.*

Why: a thread waiting for CI sits in Moving, and its PR was only named in the detail line, with no chip, while the same PR has a chip in every other section.

1. The one-line row (`OneRowBody`, desktop and phone alike) shows the PR or change chip in every section, working or not.
2. The detail line drops "on #N" when the chip already names that PR ("Waiting for CI: 0 of 1 checks done"); a number that is not one of the thread's PRs stays.
