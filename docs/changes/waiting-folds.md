# V0.waiting-folds: Waiting to run folds

→ V0.137, V0.163, V0.186, V0.192. *Amends V0.137 (1) ("it has no collapse").* (owner, 19 Nov: "Make waiting to run collapsible".)

(1) **The fold.** The header of Waiting to run folds and unfolds the section, like every other section's header (click, tap, Enter or Space). It is open by default, and the fold is remembered per device with the others (V0.186). It does not close by itself (V0.196 is for Snoozed and Settled).

(2) **Folded.** The header keeps its count. The rows, the "The next waits" line and Fyr's own tests are not drawn; the current thread's row is, alone, in its place (V0.192). j/k passes the folded section. The phone's Reorder button shows only while the section is open.

(3) **Both fronts.** One component (`slot-queue-ui.tsx`) draws the section on the desktop and the phone.
