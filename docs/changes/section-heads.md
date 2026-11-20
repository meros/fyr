# V0.section-heads: the inbox's section headers stand out and show that they fold

→ V0.186, V0.your-move. *Amends the phone's fold arrow, which only some sections had.* 

(1) **The title.** A section header shows a chevron, the section's state icon (the rail's and the strip's), the name and the count. A section with rows has its name in full ink and semi-bold and its count in a pill. An empty section steps back: faint, no pill.

(2) **The fold.** The chevron points right when the section is folded and down when it is open, on every section that has rows. An empty section has nothing to fold and shows none; Pinned and Waiting to run do not fold and show none. The chevron's place stays, so the icons line up.

(3) **Your move.** With rows in it, its name, chevron and icon are amber and its pill is filled amber. Empty, it looks like any empty section.

(4) **Both fronts.** One component (`SectionTitle` in `row-ui.tsx`). The phone's type and icon are a step larger, for the finger.
