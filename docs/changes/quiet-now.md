# V0.quiet-now: the owner can make a project quiet now, by hand, until a time they pick

→ V0.46, decision 0014, V0.212 (the values a linked project shares), 3.15.

(1) **The cause.** Quiet hours were only a weekly schedule. To quiet a project once (a meeting, an afternoon off, a night that started early) the owner had to add a window and remember to remove it later.

(2) **The setting.** A project may carry `quietNow`, an RFC 3339 time: the end of a quiet stretch the owner started by hand. `project.update {quietNow}` sets it; `null` (or `""`) ends it at once. A time more than 7 days ahead is 400; a time already passed is stored as none. It is one of a linked project's shared values, like `quiet` (`front.projectShared`, `project.shared`), so every host of the project goes quiet together.

(3) **Quiet now.** While `quietNow` is ahead, the project is quiet exactly as in its windows (V0.46 (2)–(6), V0.114, V0.push-summary-batch): its threads hide from the inbox, pushes are held and summed up at the end. `quietUntil` is the later of `quietNow` and the windows' stretch; when the stretch started by hand ends inside a window, the window's stretch joins on (Quiet until 17:00 with the 17:00–06:00 window reads "Quiet until 06:00"). The host's tick sends the project and its threads when it ends, as it does for a window (`core.QuietUntilNow`).

(4) **The project screen** (desktop and phone, the one `QuietGroup`): the first row of "Quiet hours" is "Quiet now", "Hide its threads and hold notifications from now, once.", with "1 hour", "3 hours", "Until 08:00" (the next 08:00; "Until tomorrow 08:00" after it has passed today) and a 24-hour time pick with "Until HH:MM" (the next time the clock reads it). Each starts the stretch at once. While one runs the row says "Quiet by hand until 15:30." (tomorrow or the day named when later) and has "End now". End now ends only the stretch started by hand; a window that holds keeps the project quiet. The phone's buttons and time pick are 44 px high for touch.

Tests: `internal/core/quiet_test.go` (TestQuietUntilNow), `internal/engine/quiet_test.go` (TestQuietNowByHand), `web/test/quiet.test.ts` ("quiet now by hand").
