# V0.automation-folds: the Automation page folds its reference groups to one line each

→ V0.119, V0.126, V0.164, V0.173, V0.176, V0.188, Decision 0029 S1. 

(1) **The fold.** Each group on Settings → Automation except Auto-update has a header that folds it: a click, tap, Enter or Space on the title. Folded, the header keeps its title and one summary line, and the card and its foot are not drawn. The fold is remembered per device (`fyr.settingsFold`), per group and host.

(2) **What starts open.** The groups that ask for a decision: Auto-merge, Leftover processes and Undeclared now. Updates starts open while a release is rolled back. The rest start folded with their summary: Updates (what runs), GitHub API (the limit), Worktrees nobody works in (how many; Fyr removes them by itself), Memory guard (its summary line), Learned heavy jobs (how many), When a thread counts as heavy, Heavy jobs (the budget).

(3) **Open whatever was stored.** Leftover processes when the rail's leftovers link lands on it (sub `leftovers`), and GitHub API while the limit is low.

(4) **Both fronts.** One component (`Group` in `settings-rows.tsx`) folds on the desktop and the phone. The phone's header is a 44 px target, for touch.
