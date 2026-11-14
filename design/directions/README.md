# Fyr visual directions

Five directions for the desktop app at 1280 × 800. Each file shows the same inbox, thread and question card in light and dark, then the same layout twice more with stress data. Open `index.html` to see all five side by side.

All five keep the brand rules: slate neutrals, `beam` only on the asking icon, the Needs you count and the question header, and the nine geometric state icons from `screens-v1.html` (a dashed ring replaces the hourglass for waiting). State reads in grayscale by icon shape.

The HTML is generated. Edit `design/generators/make_directions.py` and run `python3 design/generators/make_directions.py`. The stress data lives in one place there, so every direction gets the same extreme cases: a 3- and a 140-character title, a 59-character project name, a long branch and a long URL with no spaces, counts of 0 and 9999, a thread with no PR, Swedish å/ä/ö, and a 6-line question with a 140-character option.

| Direction | Idea | Type pair | Signature detail | Long text | Main risk |
|---|---|---|---|---|---|
| [01 Ledger](01-ledger.html) | The inbox is a ruled table. Project, PR and age sit in fixed columns across every section. | IBM Plex Sans Condensed + IBM Plex Mono | Faint vertical column rules run through all rows, like ledger paper. The thread header is a row of labelled fact cells. | Every column has a fixed width. Titles, projects and meta lines end in one-line ellipsis. Rows are 30 or 48 px tall, never more. | The inbox needs 500 px, above the 360 to 420 px in the brand book, so the thread gets less room. |
| [02 Console](02-console.html) | A terminal-style view in one monospace face for people who live in the keyboard. | JetBrains Mono + Inter (Claude's prose and the question only) | A modeline at the bottom shows the mode (ANSWER), the focus path and the keys that work right now. | Monospace makes widths predictable. Meta lines keep the state word first and cap the project at 45 %. Prose wraps anywhere. | Mono text is wider and tiring for long reading. It can feel like a terminal skin, not a calm control room. |
| [03 Reader](03-reader.html) | A reading view. The thread is a centred serif column, and the question is the largest text on screen. | Inter + Newsreader (+ IBM Plex Mono for code) | The inbox card quotes the open question in serif italic, and the question card is a "slip" joined to the composer. | Inbox cards have three fixed lines of 20, 22 and 16 px. The title clamps to 2 lines. Option labels and descriptions end in ellipsis. | Cards are 74 px tall, so the inbox shows fewer rows. Three typefaces are more to maintain. |
| [04 Rail](04-rail.html) | A count rail on the far left keeps all five section counts in view at once. | Geist + Geist Mono | The rail shows each section as icon, count and `g1` to `g5` key. Options form a 2 × 2 grid of keyed tiles. | Rail counts use tabular numerals and fit 4 digits in 76 px. Header chips shrink and cap the project at 36 %. Tiles have a fixed 70 px height with a 2-line clamp. | Three columns leave the least space for the thread. The 2 × 2 grid reads poorly when one option is long and the others are short. |
| [05 Spine](05-spine.html) | Time comes first. A time gutter runs down the inbox and turns into the timeline spine of the thread. | Atkinson Hyperlegible Next + Atkinson Hyperlegible Mono | The thread is a vertical spine with a node per event. It ends in the asking icon, which connects to the question card. | The gutter is 84 px with tabular numerals and fits "Mon 06:00". Rows are 58 or 34 px. In the header the branch keeps at least 96 px and the project caps at 36 %. | Times on the left push titles right. The heavy type and 2 px borders are louder than the brand's "quiet" aim. |

## What was checked

Each file was rendered in headless Chrome at 1400 px wide with the Google fonts loaded, and the screenshots were read for overflow and clipping. The first pass found time columns too narrow for "Mon 06:00", a project name that pushed the state word off the Console meta line, and header chips where a long project hid the branch. These are fixed. Tab, hover and focus states are not shown, and nothing was tested with a screen reader or at other window sizes.

## Recommendation

Use 01 Ledger for v1. Its fixed columns make long text and large counts safe by construction, and it scans fastest with the brand's own IBM Plex family. Narrow its inbox to 440 px by dropping the project column into the meta line, and borrow the modeline from 02 Console for the key hints.
