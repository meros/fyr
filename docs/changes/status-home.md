# V0.status-home: with no thread open, the desktop shows a status screen, and the phone has it as its own screen

→ V0.109, V0.145, V0.your-move, V0.80, V0.44, V0.98. *Amends V0.109 (1) (after the owner closes the thread no row stays marked, and j/k start at the top) and V0.145 (1) (the desktop no longer opens the first thread on load, so nothing replaces the page's entry until the owner opens one).* (owner, 19 Nov: "Add ability to 'deselect' the current thread somehow. When no thread is selected, replace rhs with a nice status screen", then, choosing from the proposal: Esc a second time and a click on the list's empty space; the hybrid layout; the desktop starts on the status screen; the phone gets its own Status screen.)

(1) **Closing the thread (desktop).** Esc in the list closes the open thread: from the thread pane the first Esc brings the focus to the list (as before) and the second closes it. A click on the list's empty space (below the rows, not on a row, a section head or a button) closes it too. A running thread is never stopped by this Esc: the interrupt stays where it was (Esc in the thread pane). After a close no row is marked, the URL names no thread (`open` and `tab` go, other parameters stay), and a reload shows the status screen. The history entry stays: Back and Forward still reach the thread, and opening it again writes it into the URL again.

(2) **Start.** The desktop starts on the status screen. `?open=` (a link, a push tap, a notice) still opens its thread. The phone starts on the inbox, as before.

(3) **The status screen.** One component on both fronts (`home-ui.tsx`, pure parts in `home.ts`), top to bottom:
- **The day and time**, and one line: "3 running · 2 need you · 5 merged today", or "Nothing running · nothing needs you".
- **Your move**: the first five threads in Your move's order (V0.your-move groups, then the newest), each with the inbox row's state and line and one button. A thread with exactly one green PR has **Merge**, which merges here (the board's Merge, bound to the head shown, V0.97); otherwise Answer, Review or Open, which open the thread. "All n →" goes to Your move in the list (the phone: back to the inbox).
- **Moving without you**: a tile per thread in that section, running ones first and the longest running first: its live activity (or why it waits, such as Waiting for CI), its host when there are several, how long it has run, and its CPU. A tile opens its thread.
- **All quiet**: when nothing needs the owner and nothing moves, one calm line.
- **Since you looked**: merged and done changes (V0.98), settled threads and failed threads since the owner last left the screen, newest first, eight at most. The first visit covers a day; a look older than a week is cut to a week. The last look is kept on this device (`fyr.home.lookedAt`), written when the screen goes or the page goes to the background, so entries do not leave while the owner reads them.
- **At the foot**: each host's two meters (V0.167; the name only with several hosts, V0.140) and every provider's quota line (V0.44); the desktop adds its main keys.
The screen shows what the inbox shows: its filters and quiet hours hold here too.

(4) **Phone.** The Status screen (`?status`) opens from the logo in the inbox header and from More → Status. It has the same parts; its buttons and rows are 44 px and its tiles one column (touch, a 360 px screen); it names no keys (no keyboard).

(5) **Tests.** `web/test/home.test.ts` (the order of Your move, Merge only with one green PR, the order of the tiles and their line, Since you looked and its window, the summary line, the URL after a close, the phone's URL); layout e2e "V0.status-home desktop" (the page starts on the status screen with no row marked; Esc in the list and a click on the list's empty space close the thread; the thread opened again is in the URL), the phone screen "status (V0.status-home)" in the layout invariants, and "V0.145 desktop" now opens its first thread itself.
