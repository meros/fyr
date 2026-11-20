# V0.merge-label: a Merge button in the list says "Merge", not the head's hash

→ V0.97 (1), V0.101 (6). *Amends V0.97 (1) "Each Merge button shows the short SHA" and V0.101 (6) "Merge a1b2c3d" for the inbox row and the change card.* 

(1) **The label.** The inbox row's Merge (a Ready change), the change card's Merge and the yes of a merge ask read "Merge" ("Review and merge" when a required review waits). A hash on the button told the owner nothing they could check at a glance; the PR chip beside it already names the PR.

(2) **The head stays bound.** Each press still sends the head it was shown (V0.97), and Fyr still refuses a head that moved. The head is named in the button's title and on the card's line ("Green and waiting for your merge. Merges a1b2c3d."), where it can be looked up.

(3) **The PR tab** keeps "Merge a1b2c3d…": it opens the merge dialog, which is where the owner reads the PR's details.

Desktop and phone share the row (`NeedButton`) and the card (`NeedLine`), so both fronts change together. Tests: `web/test/need.test.ts`, `web/test/merge-wait.test.ts`.
