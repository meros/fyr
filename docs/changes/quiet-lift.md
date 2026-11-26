# V0.quiet-lift: the quiet of a project is lifted on its page, for every surface and device, by holding a time from a list for 30 s

→ V0.46, V0.74, V0.114, V0.quiet-now, decision 0034 (the values a linked project shares), 3.15. *Replaces the per-device unlock of V0.74 (2) and V0.114: the 30 s hold rows under New thread, in the project filter's gate, in search and on a quiet thread are gone.*

(1) **The cause.** The 30 s hold that showed quiet projects for 30 minutes sat in several places (New thread, the project filter, search, a gated thread), so it was hard to find. It was also an unlock of the device, kept in browser storage: another device, another host of a linked project, the inbox's own hiding and the held pushes did not follow it, so a lift seemed not to work in some places.

(2) **The setting.** A project may carry `quietLift`, an RFC 3339 time: the end of a lift of its quiet. `project.update {quietLift}` sets it; `null` (or `""`) ends it. A time more than 24 hours ahead is 400 (a lift is a pause, not a way to turn quiet hours off); a time passed is stored as none. It is one of a linked project's shared values, like `quietNow`, so every host lifts together.

(3) **Lifted.** While `quietLift` is ahead the project is not quiet, whatever its windows and `quietNow` say (`core.QuietUntilLifted`): the project and its threads carry no `quietUntil`, so the inbox, the filter, search, New thread, every link into a thread and the pushes treat it as any other project, on every device. The host sends the project and its threads at once, and its tick sends them again when the lift ends and the quiet holds. Starting quiet by hand (`quietNow`) ends a lift. Pushes held before the lift keep their time and are summed up when the quiet they were held for ends (V0.push-summary-batch); a push during the lift goes out as usual.

(4) **The project page** (desktop and phone, the one `QuietGroup`), the first row of "Quiet hours", one place for both directions, each a list of times like snooze's (the label, then when it ends):
  - not quiet: "Quiet now", "1 hour", "3 hours", "Until 08:00", and "Until HH:MM" with a 24-hour time pick, one tap each (V0.quiet-now);
  - quiet: "Lift the quiet", with what holds it ("Quiet until 06:00." or "Quiet by hand until 15:30.") and "30 minutes", "1 hour", "3 hours", "Until the quiet ends" (when that is past 3 hours and at most a day ahead); a time longer than the quiet left is not offered. Each is held 30 s (finger or mouse button); the row fills and says "Keep holding · 12 s"; lifting or moving off starts over. No key and no tap, on purpose. End now still ends a stretch started by hand at a tap;
  - lifted: "Quiet lifted", "Its threads show and notify as usual, on every device, until 15:30.", and Quiet again, which ends the lift at a tap.
  The phone's rows are 48 px high for a finger that holds.

(5) **Elsewhere** a quiet project says where its quiet is lifted, and opens nothing by itself: New thread's line reads "2 quiet projects hidden. To start a thread in one, lift its quiet on its project page."; the gate of a quiet thread or of the project filter says "Lift the quiet on its project page." with a Project page button, which opens the page only from the owner's press; search says "Lift a project's quiet on its page to search its threads."

Tests: `internal/core/quiet_test.go` (TestQuietLift), `internal/engine/quietlift_test.go` (TestQuietLift), `web/test/quiet.test.ts` ("lift the quiet"), `web/test/quiet-gate.test.ts`.
