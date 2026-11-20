# V0.row-calm: an inbox row says each thing once and takes only the lines it needs

→ V0.135, V0.91, V0.101, V0.206. *Amends V0.135 (1): the title's box is no longer always two lines high.* On both fronts (`web/src/row-ui.tsx`, `web/src/rows.ts`), in every section:

1. **Title.** The title takes the lines it needs, one or two, and is still cut at the end of the second. Only a row whose title has not come yet (shown as "Untitled") keeps two lines of room, so the row does not jump while its title arrives. A short title no longer leaves an empty line between itself and the line under it.
2. **The state word names the action.** An approval says "Allow <tool>" (for example "Allow Bash"), not "Approval", the way an effect says "Merge #12". A Needs you reason of the form "<change title>: <need>" (V0.101) shows the need ("Merge") as the state word and puts the change title first on the line. "Run: <why>" (V0.149) shows "Run" and the why.
3. **No echo.** The line under the title leaves out what the state word already says. If its first part names the same PR ("Merge pull request #4170" under "Merge #4170"), that part is dropped. If it starts with the state word, that part is dropped, or only the word when nothing follows it ("API error 529: overloaded" reads "529: overloaded"). The PR chip is left out when the state word names that PR ("#4170 ready" under "Merge #4170").
4. **The Needs you button** (Merge, Answer) and its beam dot sit at the right of the state line, under the age, not on a line of their own.
5. **CPU.** A working row's CPU reading sits with the chips, at the end of the second line, so cutting the line never takes it. It is monospace like the age, and muted unless it is hot. A PR chip has room for "#9999 conflicts".

The provider stays on every row (V0.206).

6. **One word, not two.** When the row's button says its state word ("Merge" beside a "Merge" state), the line leaves the word out and starts with what the button acts on.
7. **Project name.** A long project name is cut in the text at 14 characters (the full name is its tooltip), not by its box, so no gap is left before the "·" after it.
8. **Phone section button.** "Settle merged" in the Finished, seen header keeps its 44 px touch height but draws 30 px, inside the header's band.
