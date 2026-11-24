Fyr is a calm control room for coding agents. Like a lighthouse, it watches everything and shines only when someone must act. The interface stays quiet, neutral and dense; one clear light, `beam`, marks what needs the user. Everything else is warm paper and espresso ink in light, warm charcoal and cream in dark: warm without a yellow cast.

## Voice

- Say what happened and what the user can do: "Checks failed on head", "2 questions", "Settled". No exclamation marks, no emoji, no "Oops".
- Name things by what the user recognises: thread, question, pull request, host. Never internal names (projection, Rule, eseq).
- Times are relative under a day ("9 min", "2 h"), absolute after ("Mon 06:00"), always Europe/Stockholm.
- Sentence case everywhere, except section headers (`label`, uppercase).
- Swedish text keeps å, ä, ö. The name is always written Fyr.

## Colour

- `surface` is the page; `surface-raised` holds cards, the list pane and the thread header; `surface-sunken` is for wells (the rail, search, code). A row's tile takes the wash of its state (`beam-wash`, `harbour-wash`, a red wash).
- `ink` for text, `ink-muted` for metadata, `ink-faint` for icons and dividers only.
- `beam` is reserved. It fills the asking icon and the Needs you count, and `beam-wash` marks the Decision card header. Never use beam for buttons, links, brand decoration or charts.
- State colours carry meaning only with their icon: `signal-ink` (failed), `harbour` (blocked, links, focus), `calm` (settled, passing). Working, waiting, seen and snoozed are `ink-muted` on purpose.
- `night` fills the one primary button per view; text on it is `on-night`.
- Every text pair is at least 4.5:1 in both themes; icons and borders at least 3:1. The focus ring is a 2px `harbour` outline with 2px offset.

## Type

- Geist for the interface, numbers included (tabular figures); Geist Mono for commands, paths, branches, code and output.
- Four sizes only: `meta` and `label` 12px, `row` 13px, `body` 15px, `title` 20px. `code` is 12px mono.
- One `title` per view. Row titles are `row` (500); done rows go to 600, nothing else changes weight.
- Use `font-variant-numeric: tabular-nums` for times and counts.

## Space and shape

- A 4px grid: `space-1` to `space-8`. Rows use `space-3` vertical and `space-4` horizontal padding.
- `radius-sm` for keys, `radius-md` for controls, `radius-lg` for rows, cards, the palette and toasts, `radius-xl` for the Decision card, the composer and dialogs. Chips and badges are pills.
- Surfaces have an order: the rail lies under the list, the list over the page, cards over both. A card shows it with `lift-1` (a ring, a contact shadow, an ambient one) and no border; the Decision card and the composer with `lift-2`; dialogs, menus and toasts with `shadow-overlay`.
- Controls have an edge: a secondary button has `edge-control`, the primary `edge-primary`. What sits in (search, a disabled primary) has `well`. Rows in the list stay flat; only the selected one is tinted.
- Shadows are warm brown in light and black, with a faint light on the top edge, in dark. Never grey.

## Motion and calm

- Nothing moves under the pointer or the keyboard focus. The inbox reorders only when the pointer leaves the list, with a 150 ms move.
- Working shows one static icon per row; the only spinner is in the thread header of a running thread.
- Respect `prefers-reduced-motion`: no movement at all.
- Notifications only when something enters Needs you. Sound is off by default.

## Layout

- Desktop: the inbox (360–420px) on the left, the thread on the right, Decisions inline at the top of the thread.
- Phone (below 640px): one column; the inbox, then the thread; touch targets at least 44px; the Decision card works from a push notification.
- Every row explains its position in its reason text. Order is never a black box.

## Icons

- The nine state icons are part of the bundle (`StatusBadge`); they are geometric so they read at 14px: disc (asking), diamond (failed), ring with centre (blocked), dot (done), quarter arc (working), dashed ring (waiting), open ring (seen), crescent (snoozed), check (settled).
- The logo is an isometric lighthouse rendered with Blender: `design/logo/lighthouse.py` builds the scene, `design/logo/lighthouse-512.png` is the master, `web/assets/logo.png` (128 px) is what the desktop rail shows at 32 px. Render again with `nix shell nixpkgs#blender --command blender -b -P design/logo/lighthouse.py -- OUT 128 256`. The app icons are cut from the same render by `design/logo/icons.sh` (V0.110). Where the name is set as text, use Geist 600.
