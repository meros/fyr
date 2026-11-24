# V0.diff-line-comments: comment on diff lines in the Changes tab and send the comments to the agent

→ 13.5. *Amends V0.25 (the diff is no longer read-only), V0.65.* (issue 641)

To ask about a line, the owner had to leave the Changes tab and describe the file and line in the composer. Now they comment on the line itself, and the agent gets the file, side, lines and head.

1. **Pick.** In an open file's diff, an added, removed or context line can be picked; hunk headers and notes cannot. Desktop: a press on the line's numbers picks it and opens the comment box under it; Shift and a press on another line of the same file makes a range. A focused line's numbers take `c` (the box opens for that line, or for the pick it is in), which does not reach the list's `c` (new thread). Phone: a tap anywhere on the line picks it and a second tap on another line makes the range, because a touch screen has no Shift; a bar at the bottom of the tab names the pick, with Cancel and Comment, and Comment opens the box as a bottom sheet, because there is no room for it under a line at phone width. A phone diff line is 26 px tall and the full width of the diff, not 44 px: 44 px rows would show a third of the code. The bars' and the box's buttons are 44 px.
2. **Comments.** The box names the file name and lines (the lines never ellipsize) and has Cancel and Add comment; `Ctrl+Enter` (`⌘⏎`) adds the comment and sends every pending one; Escape cancels. A pending comment shows under its last line, in the diff it was written on (whole branch or new since you looked), with Edit and Delete. One whose line is no longer in the diff after a Refresh shows at the end of the file's diff and is still sent. The box and a pending comment stay in the visible width when the diff scrolls sideways.
3. **Drafts** are kept per thread (key `fyr.diffComments.<thread ref>`) like the composer draft (V0.29): they survive a reload, a screen change and a tab change; Send clears them, and so does Discard.
4. **Send.** A bar over the files says "N comments on the diff" with Discard and Send. Send sends one message through `thread.send`, the composer's path, so a running thread gets it as it gets a typed message:

        Comments on the diff (2):

        web/src/x.ts:42-45 (new) · whole branch at def5678
        ```diff
        +the quoted lines, with their diff sign
        ```
        The comment.

        go/b.go:7 (old) · since abc1234, at def5678
        ...

   The side is `(new)` for added and context lines (new line numbers) and `(old)` for removed ones; a range over both names both (`42-45 (new), 40-41 (old)`). `whole branch at <head>` or `since <seen head>, at <head>` (short shas) says which diff the numbers refer to; a since look whose commit is gone counts as the whole branch. The fence is longer than any run of backticks in the quote. Send is off, with the reason, while `thread.send` is unavailable.
5. **In the conversation** the owner's message of diff comments shows "N comments on the diff", then each comment's place with its quoted lines folded, then its text.
6. Out of scope: posting the comments to the GitHub PR as review comments, and comments that stay attached to lines across new commits.
