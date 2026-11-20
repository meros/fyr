# Fyr on the phone

**In short.** The phone shows the same inbox, threads and questions as the desktop, rearranged for one thumb. Answering a pushed question takes 4 taps, and starting a thread from a screenshot takes 3 or 4. Two things are not settled: how the phone queues a message (Enter versus ⌘Enter) and the breakpoint (640 or 760 px), listed under Open questions.

This folder holds the rules that turn each desktop element of the Rail client (decision 0006, `../directions/04-rail.html`) into its phone form. A future desktop feature gets its phone form from these rules, without a new design round. `mobile.html` shows the rules at 390 × 844 in light, dark and with the stress data. Open `mobile.html#<frame-id>` (for example `#q1-light`) to see one frame alone at phone width.

The phone layout applies at 760 px wide or less (V0.14, `web/src/app.css`). Everything here is a design. Nothing in `web/` changed.

## 1. Translation rules

### Layout and navigation

| Desktop element | Phone form | Rule | Why |
|---|---|---|---|
| Three columns: rail, inbox, thread | Two screens: Inbox and Thread. New and Search are screens too. Sheets slide up over a screen. | One screen at a time. A sheet never opens a second sheet; it replaces itself. | 360 to 430 px holds one column. Stacked sheets make Back ambiguous. |
| Rail (5 sections, counts, `g1`–`g5`) | Count strip under the inbox header: 5 equal segments, icon and count on line 1, label on line 2, 56 px high. | Same order, icons, counts and active style as the rail. A tap scrolls the one list to that section and expands it. It never filters. The active segment follows the scroll. | Keeps all five counts in view, the reason the owner chose Rail. Filtering would break U1 (one list) and U12 (explainable order). |
| Inbox search field, `⌘K` | Search in the bottom bar opens a Search screen with the field focused. | Search is a screen, never a field in the inbox header. | U21. A top field is out of thumb reach and opens the keyboard by accident. |
| `c` new thread | New, the centre item of the bottom bar, a labelled pill. | New is always one tap from the inbox. It uses `night`, never `beam`. | U21 and A3. Beam marks only what needs the user. |
| Bottom bar | Inbox · New · Search, 60 px plus the bottom safe area. | Shown on Inbox and Search only. Thread, New, the answer card and selection mode replace it. | One bottom bar at a time. The composer and the Answer bar need the bottom edge. |
| Back (`Esc`, `⌘[`) | The system Back gesture and a 44 px back chevron in the thread header. | Back closes the top sheet, then the peek, then the thread. A thread opened from a push gets an inbox entry under it in history, so Back never leaves the app. | Android Back and the iOS edge swipe must both work. A push is the most common entry. |
| URL | `/?open=<id>`, `/?open=<id>&tab=changes`, `/?new`, `/?search` | Each screen has a URL. A sheet adds a history entry so Back closes it. | Deep links from push (V0.15) and reloads after a new build (V0.13) return to the same place. |
| Header actions (Snooze, Settle buttons) | Thread ⋯ opens a sheet with the thread's actions. | The sheet lists every Action from the registry with its desktop label. An unavailable Action is shown disabled with its reason (U3). | Two buttons do not fit next to a 2-line title at 390 px. |
| Inbox header: brand, host, Install, bell | Same items, 52 px high: brand, host dot and name, Install (until installed), bell, ⋯. | At most three 44 px icons. Install hides once the app runs standalone. Rare items (adopt from T3, settings, push test, theme) go in ⋯. | The current first cut has four icons, one of them New, which moves to the bottom bar. |

### Inbox rows

| Desktop element | Phone form | Rule | Why |
|---|---|---|---|
| Needs-you row, 52 px, 2 lines | 68 px, 2 lines: 32 px tile, title 15 px, age, meta 13 px, PR badge. | Fixed height. Title and meta end in one-line ellipsis. Age and PR badge never shrink. | U16 and the long-text rules. The whole row is the tap target. |
| One-line row, 30 px | 48 px: icon, title 15 px, one mono meta, PR badge, age. | The desktop "N sub" column joins the meta ("2 sub · yarn vitest"). The title keeps its width first. | Four columns do not fit next to a readable title at 360 px. |
| Section header, 30 px | 44 px; 56 px when it holds a button. | A header with a bulk action shows it as a 44 px button ("Settle merged"). A collapsed section shows ▸, an expanded one ▾. | Section headers become tap targets on touch. |
| Away strip "Since 14:02" | Same strip, 44 px, with a 44 px Dismiss. | Tap filters to those rows, as on desktop (U13). | Same behaviour, bigger target. |
| Hover | None. | Nothing on the phone depends on hover. Desktop `title` tooltips move into the long-press sheet. | Touch has no hover. |

### Thread

| Desktop element | Phone form | Rule | Why |
|---|---|---|---|
| Title, 20 px, 2-line clamp | 17 px, 2-line clamp, between back and ⋯. | Wraps anywhere. With the keyboard open it drops to one line. | Long titles stay readable without pushing content down. |
| Header chips: state, project, branch, model, PR | One 44 px row: state, project, branch, PR. | State and PR never shrink. Project caps at 36 %. Branch takes the rest with a 48 px minimum. Model moves to the details sheet. A tap on the row opens details with every value in full and copyable. | The 04-rail chip rules, one chip fewer. A scrolling chip row would hide values. |
| Tabs Conversation · Changes · PR | 44 px tabs, 15 px. | Tab counts (`+42 −18`) end in ellipsis first. Tabs hide while the keyboard is open. | Three tabs fit at 360 px; the count is the least important part. |
| Grouped tool card, 26 px lines | 32 px lines, same 4 columns (icon, name 40 px, detail, duration 52 px). | Up to 4 lines show; more become a 44 px "+ N more tools" row. A line that opens something (Task, a diff) is 44 px. Plain lines are not tap targets; the card opens a tools sheet with full commands. | 32 px lines keep density; only real targets pay the 44 px cost. |
| Subagent fold (Task ▸) | Task line 44 px; expands in place with 12 px indent and a left rule. | Each subagent is one 30 px line: icon, name, last tool or "done". | Same folding as desktop, a smaller indent at phone width. |
| Composer, 46 px, Enter steers | 54 px pill: attach (44), field (16 px text, up to 5 lines), send (44). | The send button becomes Stop while a run is live and the field is empty (Esc interrupt). | 16 px stops iOS zoom. Stop needs a visible target on touch. |
| Docked question card (13.2) | The same card, docked over the thread below the header, filling down to the Answer bar. A chevron collapses it to its 48 px header. | One question per step. Options one column. The commit button is sticky. | U22. The card is the main phone loop (median answer 1.0 min). |
| `visualViewport` | The app height follows `visualViewport.height`; the composer or the Answer bar sits on its bottom edge. | With the keyboard open: header to one 44 px row, tabs hidden, bottom safe area 0. | iOS Safari does not resize the layout viewport for the keyboard. |
| Safe areas | `env(safe-area-inset-*)` on the shell. Mocks use 47 px top and 34 px bottom. | Bars extend their background into the safe area; their targets do not. | Notch, home indicator and rounded corners. |

### Decisions

| Desktop element | Phone form | Rule | Why |
|---|---|---|---|
| 2 × 2 keyed option tiles, 70 px, 2-line clamp | Full-width tiles, at least 56 px, a radio mark (single) or check mark (multi) instead of the key. | Labels never clamp. Descriptions clamp to 2 lines. The selected tile uses `harbour`, as on desktop. | U8. A 140-character label needs its full text before someone picks it. |
| `1`–`4` pick, pick moves on | Tap picks. A single-select pick moves to the next question after 250 ms. | Multi-select never moves on by itself. The last question never sends by itself. | Keeps the 13.2 flow and adds one deliberate commit. |
| `0` Other, composer as answer field | Other opens an answer field inside the card, above the keyboard. The bar shows "Next question" or "Answer". | One text field at a time; the thread composer hides while a card is open. | U15: one Enter target. |
| Tab, "Question 1 of 2" | Step dots in the card header. An answered question shows as a 44 px summary row with Change. | Dots show up to 5 questions; beyond that, "1 of 9999". | Going back must not need a key. |
| Enter answers | Sticky Answer bar, 52 px. Disabled label says what is missing ("Answer · 1 of 2 left"). | The commit button always names the result: "Answer 2 questions", "Answer 1, skip 1", "Cancel question", "Allow", "Approve", "Deny". | Disabled with a reason (U3). |
| `⇧Tab` / `←` back, Tab next (V0.ask-cancel) | The previous questions are already rows with Change; a 44 px Back under the options names the step. | The step needs no key, and Back stops at the first question. | Going back must not need a key. |
| Skip a question (V0.ask-cancel) | A 44 px Skip under the options; a skipped question shows "Skipped" in its summary row and as a done step dot, and the same control says Unskip. | A skipped question is sent as skipped, never as an answer. A card with no answer at all sends nothing, so its bar says "Cancel question". | One question must not hide the ones the owner cares about. |
| Cancel the card (V0.ask-cancel) | A 44 px ✕ in the card head, beside the chevron that collapses it. | The card closes without an answer and the agent is told it was cancelled, so it does not wait. | The card is the phone's main loop; its way out must not be hidden. |
| Long question | Clamps to 6 lines with a 44 px "Read all". | When options sit below the fold, a "↓ N more options" band sits above the Answer bar. | The options must stay findable. |
| Approval and Fyr action cards, `1` allow, `2` deny | Same card: pick a tile, then the commit button. | Every Decision on the phone is one pick plus one commit. Commands show in a mono block of at most 8 lines that wraps anywhere, then scrolls inside. | One pattern for every Decision. A stray tap on Allow does nothing until the commit. |

### Visual system

| Desktop | Phone | Rule |
|---|---|---|
| Tokens (`surface`, `raised`, `beam`, `harbour`, ...) | Unchanged, light and dark. | Copy `.t-light` / `.t-dark` of 04-rail. |
| Nine state icons | Unchanged shapes; 16 px in rows, 14 px in chips and tool lines. | Grayscale-readable by shape (U18). The waiting icon is the dashed ring. |
| Beam | Unchanged rule. | Amber marks only the asking icon, the Needs count (strip and Inbox badge) and the question header wash. Never New, toasts, buttons or notifications. |
| Geist 10 to 20 px | Geist 12 to 20 px: 12 (mono meta, ages), 13 (meta lines, chips), 15 (row titles, bubbles, buttons), 16 (prose, fields, options), 17 (thread title, sheet title), 18 (question), 20 (brand). | Nothing under 12 px. Fields 16 px. Text never uses `ink-faint`. |
| Targets 30 px | At least 44 × 44 px for anything tappable; 8 px between neighbouring targets or a shared edge. | Small visuals (a 28 px chip, a 22 px check) sit inside a 44 px hit area. |
| `kbd` hints and the key bar | Hidden. | A hardware keyboard on a tablet keeps the desktop layout above 760 px. |

## 2. Gesture map

Every key in 13.1 and in `web/src/keys.ts` has a touch form.

| Key | Touch form | Safety and discoverability |
|---|---|---|
| `j` / `k` | Scroll. | No focus cursor on touch. |
| `Enter` | Tap the row. | The whole row is the target. |
| `Space` peek | Long press (400 ms) opens the peek sheet: title, project, branch, PR, last message (3 lines), open question, then the row's actions. | The sheet says "Peek does not mark the thread seen." A haptic tick on open. |
| `e` settle | Swipe right on a row. The reveal names the exact Action: "Settle", "Settle when done", "Settle when merged". | Commits at 50 % of the width; below that it snaps back. 5 s Undo toast (U7). |
| `s` snooze | Swipe left opens the snooze sheet. | Never commits on the swipe alone, because snooze needs a time. |
| unavailable swipe | The reveal shows the reason ("Answer first") and snaps back. | An asking row cannot be settled (the desktop disables Settle while a question is open). |
| `#` archive, `u` unseen, `r` retry | Long-press sheet; Retry also in the thread header of a failed thread. | Rare Actions have no gesture. |
| `x` select | "Select" in the long-press sheet starts selection mode; then a tap toggles a row. | The header shows "N selected" with Cancel and Select all. |
| `Shift+E` | "Settle N" in the selection bar, or "Settle merged" on the Seen header. | One toast, one Undo (A5). |
| `n` / `N` | The Needs segment of the strip. After Answer, the Answer bar turns into "Next: <title> ›" for 5 s. | The desktop opens the next item by itself; the phone offers it, so a push answer does not drop the user into a second thread. |
| `c` | New in the bottom bar. | |
| `g1`–`g5` | Count strip segments. | |
| `⌘Z` | Undo in the toast, 44 px, for 5 s. | The toast sits above the bottom bar and never covers the Answer bar. |
| `Esc` | Back. In a running thread, Stop in the composer. | Interrupt has a visible button and never maps to Back. |
| `⌘K` | Search screen. | |
| `⌘1`–`⌘9` | The Working section (g2). | No stable-slot gesture on the phone. |
| `⌘Enter` queue | Long press on Send opens "Send now" and "Queue after this run". | Open question 1. |
| `1`–`4`, `0`, `Tab` | Tap a tile, tap Other, tap a step dot or Change. | |

**Swipe limits.** One Action per direction, never two. A swipe starts only after 12 px of horizontal movement at an angle under 30°, so vertical scrolling wins. The left 24 px stay free for the iOS Back edge gesture. No full-swipe for anything that cannot be undone.

**Pull to refresh: no.** The stream (SSE) keeps the list live, so a pull would do nothing. The list sets `overscroll-behavior: contain` so a pull does not reload the PWA. Connection state shows in the header (section 5).

**Discoverability.** The long-press sheet shows each Action's swipe ("swipe →"), so the sheet teaches the gestures. The first time the inbox shows a row with a settle Action, that row slides 40 px once to show "Settle", then never again on that device.

## 3. Flows and tap counts

### A2: answer from the push (target ≤ 4 taps)

| Case | Taps | Frames |
|---|---|---|
| 2 questions, both single-select | 1 push · 2 option Q1 (moves on) · 3 option Q2 · 4 Answer | `push-light`, `q1-light`, `q2-dark` |
| 2 questions, Q2 multi-select with one pick | 1 push · 2 option Q1 · 3 toggle Q2 · 4 Answer | `q2-dark` |
| 1 question | 1 push · 2 option · 3 Answer | |
| Approval or Fyr action | 1 push · 2 Allow or Approve · 3 commit | `approval-light`, `effect-dark` |
| Each extra multi-select pick | +1 | |
| Multi-select that is not the last question | +1 (Next question) | |
| Other with text | +1 (field) plus typing | `q-other` |

A2 holds for single-select questions and for one pick per multi-select question. It fails, by one tap per extra pick, when a multi-select question needs two or more picks. With the phone locked, unlocking comes before tap 1 and is not counted.

### A3: start a thread from a screenshot (target ≤ 4 taps)

| Path | Taps |
|---|---|
| iOS PWA, paste chip | 1 New · 2 Paste screenshot · 3 the system Paste bubble · 4 Start |
| Android PWA, paste chip | 1 New · 2 Paste screenshot · 3 Start (Chrome asks for clipboard access once per site, then never) |
| Photo picker | 1 New · 2 Photos · 3 the photo · 4 Start |
| Android share target | 1 Share in the screenshot preview · 2 Fyr · 3 Start |
| iOS Shortcut (11.4) | 1 Share · 2 the Fyr Shortcut; the Shortcut posts to the capture endpoint. 2 taps. |

Text is optional; a screenshot-only start uses the image as the message (0.2 counts these). Project and profile default to the last used on this device. Changing either takes 2 taps and is remembered. Offline, Start becomes Queue and the thread goes to the outbox (U23).

## 4. Push

The host pushes when a thread enters `asking` or `failed` (V0.15). Payload `{title, body, threadId, state}`, tag = thread id.

| State | Title | Body |
|---|---|---|
| asking, questions | thread title | `2 questions · Which gesture should close the filter sheet?` or `Question · <text>` |
| asking, approval | thread title | `Approval · Allow Bash: yarn test src/map/lasso --run` |
| asking, effect | thread title | `Fyr action · Merge #4170 with squash?` |
| failed | thread title | `Failed · API error after 14 min · acme/web` |
| done, blocked (M4 batch, after 10 min away) | `Fyr` | `Since 14:02: 3 done · 1 blocked` |

Rules:

1. The body starts with the same word as the inbox meta line, so the state survives any title length. The OS clamps the title, and Fyr does not.
2. The project comes last; it is the first thing the OS cuts.
3. When the Decision is answered elsewhere, the service worker closes that notification (`getNotifications({tag})`). The open card says "Answered on desktop 14:03" (U22).
4. No emoji, no sound setting, no action buttons. iOS web push has no actions, and one behaviour on both platforms is simpler.
5. The current host payload (`internal/push/push.go`) says `Asks: ...` and `Failed: ...`. Change it to the words above. It already cuts the body by runes, so å, ä and ö stay whole.

## 5. Installed-app states

| State | Shown as |
|---|---|
| Browser tab, installable | Install icon in the header (current first cut). |
| Installed (`display-mode: standalone`) | Install hidden. |
| iOS Safari, not installed | No bell (push needs the installed app). One dismissible row at the top of the list: "Add Fyr to the Home Screen to get notifications. How". |
| Bell on / off / blocked | Filled bell / outline bell / bell with a slash. A tap opens the existing Notifications dialog as a bottom sheet. Unsupported hides the bell. |
| Live | Calm dot before the host name. No text. |
| Reconnecting (no stream for 3 s) | Dashed ring dot and a strip "Reconnecting to desk…" under the header. |
| Offline (15 s) | Hollow dot and a strip "Offline · showing 14:02 · 2 waiting to send · Outbox". Actions stay optimistic and go to the outbox. |
| New build (V0.13) | No reload while the user types or a card is open. A strip "Fyr updated. Reloads when you leave. Now", and the reload happens on `visibilitychange` to hidden. |

Connection problems are strips, never toasts, and never `signal-ink` (red is for failed threads).

## 6. Long text

Every rule above holds with the stress data of `../generators/make_directions.py`; each screen has a stress frame.

| Case | Phone rule |
|---|---|
| 3 and 140 character titles | Rows: one-line ellipsis. Thread header and peek: 2-line clamp, wrap anywhere. |
| 59 character project | Row meta: ellipsis inside the meta line. Chip: max 36 %. New screen default: ellipsis in a full-width 48 px field. |
| Long unbroken URL and branch | Prose, bubbles and the question wrap anywhere. Chips and meta lines ellipsis. Commands wrap anywhere in a mono block of max 8 lines. |
| Counts 0 and 9999 | Mono tabular numbers. The strip fits 4 digits in a beam pill at 360 px; 10000 and more show as `10k+`. A 0 section stays in the strip and the list as a collapsed header. |
| Question 6 lines on desktop (13 on the phone) | 6-line clamp with Read all. |
| 140 character option | Full label, wrapped. Description 2 lines. |
| Swedish å, ä, ö | No `text-transform`, no letter-spacing on running text; truncation by grapheme, never by byte. |
| Host name 42 characters | Header host ellipsis; Install, bell and ⋯ never shrink. |

## 7. What was checked

`mobile.html` was rendered in headless Chrome at 390 × 844 with the Google fonts, one screenshot per frame (39 frames), and each screenshot was read for clipping and overflow. The first pass found a cramped swipe reason, the tap-count annotation class colliding with the tool-line class, a host name without ellipsis and wrong card context lines; these are fixed. A script measured every tappable element in all frames: none is under 44 × 44 px (a positive control on PR badges, which are not targets, did flag them at 20 px). Not checked: 360 px and 430 px widths (the rules are written for them, the mocks are 390 only), Android safe areas, real keyboard heights, a screen reader, and gestures on a device.

## 8. Implementation plan

Order of changes in `web/`, each a separate PR:

1. **Viewport and shell.** `index.html` viewport meta with `viewport-fit=cover, interactive-widget=resizes-content`. A `useVisualViewport` hook sets `--vvh` and a `kb-open` class. Split the phone rules out of `app.css` into one phone block with 44 px targets and the type scale above.
2. **Navigation.** A small history router for `?open`, `?tab`, `?new`, `?search` and sheets; a synthetic inbox entry under a push deep link. Bottom bar. New and adopt move out of the inbox header into the bar and the ⋯ sheet.
3. **Count strip.** A phone variant of `Rail` (`inbox.tsx`) that renders the same sections and counts horizontally and scrolls the list instead of selecting.
4. **Rows.** `NeedsRow` and `OneRow` keep their markup; the phone block changes sizes and moves "N sub" into the meta. New: a `useSwipe` hook, the long-press peek sheet and selection mode. The sheet reads Action availability and reasons from the existing registry.
5. **Thread.** `ThreadPane` header phone variant (⋯ sheet, details sheet, chip priorities), Stop in `Composer`, compact header with the keyboard open.
6. **Decision card.** `AskCard` and `DecisionCard` get the phone layout: one column, radio and check marks, step dots, the sticky commit bar, 6-line clamp. `question.ts` already moves on after a single-select pick; add the 250 ms delay in the view only.
7. **New screen.** `NewThreadDialog` renders full screen on the phone with Paste and Photos on top; remember project and profile per device.
8. **Push and states.** Wording in `internal/push/push.go`, close-on-answered in `sw.ts`, the connection and update strips, the iOS install row. The outbox (U23) is its own PR.

Carries over unchanged: `icons.tsx` (`StateIcon`, `stateLabel`, `stateClass`), `PrBadge` and `prLabel`, `effectSentence`, `question.ts`, `conversation.ts`, `markdown.ts`, `format.ts`, `seen.ts`, `store.ts`, `api.ts`, `readImages`, `useImagePaste` and `ImageStrip`, `SnoozeMenu` presets (rendered in a sheet), the `PushDialog` text, `push.ts` and `pwa.ts`. `keys.ts` stays desktop only.

## 9. Open questions

1. **Queue on the phone.** Desktop has Enter (steer) and ⌘Enter (queue). This design hides queue behind a long press on Send. If queueing is common on the phone, it needs a visible control.
2. **Breakpoint.** SPEC U8 says one column below 640 px; V0.14 and the code use 760 px. This design follows 760. A tablet between the two would get the phone layout.
3. **Type family and scale.** SPEC U9 names IBM Plex and four sizes (12, 14, 16, 20). Decision 0006 chose Geist, and the Rail desktop already uses 13 and 15. This design uses six sizes on the phone. U9 should be updated or the scale cut.
4. **Next after Answer.** The desktop opens the next item that needs the user. The phone offers it in the bar for 5 s instead. Confirm this split.
5. **Approval commit.** One pick plus one commit costs one more tap than the desktop's single key. If approvals become frequent (today 0 a day, 0.2), a direct tap with a 5 s undo could replace the commit for `pr.merge`, which Fyr runs later anyway.
6. **iOS clipboard.** The A3 count for iOS paste assumes one system Paste bubble per paste. A Shortcut avoids it; the owner's iOS version decides.
