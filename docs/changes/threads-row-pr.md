# V0.threads-row-pr: the agents' `threads` list names the PR the inbox row shows, and a PR whose change was given up never reads as open

→ V0.22, V0.multi-pr (3). *Amends V0.22 (the PR in `threads`) and V0.multi-pr (3) (the row's `+N`).*

A review of a day's threads, done by an agent from the `threads` tool, reported rows that "showed" a merged PR while another PR of the thread was open and conflicting, or hid a failing one. The inbox row did not do that: on the same data it showed the conflicting PR (V0.multi-pr (3)). The `threads` tool printed the thread's main PR instead (`mainChange`): the first PR the thread got, which keeps that place after it merges. So an agent reading `threads` saw a different PR from the one the owner sees on the row. A smaller row fault came up while checking: with no open PR, the row fell back to every PR of the thread, and a PR still open on GitHub whose change was abandoned, reverted or marked done ranked as open work and read "ready".

(1) **`threads` names the row's PR.** Each line shows `PR #N <word>` for the PR the row picks: of the open PRs of changes still in flight, the most urgent (failing > conflicts > changes requested > review required > ready > running), with `+N open` when the thread has more open PRs; with none open, the newest PR of a change not dismissed. The word is the row chip's (`failing`, `conflicts`, `review`, `running`, `open`, `merged`, `closed`, `missing`). `core.RowPR` is the Go twin of `prUrgency` in `web/src/pr.ts`.

(2) **A given-up PR is not open on the row.** When the row falls back to every PR, a PR still open on GitHub whose change ended (abandoned, reverted) or is done reads with that word (`#12 abandoned`, title "abandoned, still open on GitHub") and ranks after merged and closed PRs. The PR tab and the header are unchanged.

(3) **`+N` counts open PRs only.** A thread whose PRs all merged reads `#1672 merged`, not `#1672 merged +49`; every PR stays in the tooltip.

Desktop and phone share the chip (`rowPrs` in `web/src/offers.ts`, used by the desktop row and the phone's peek).
