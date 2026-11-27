# V0.undeclared-mock-age: the mock's undeclared work reads its age as of each load read, so V0.164's "for 50 s" holds however long the page is open

→ V0.164 (undeclared heavy work, its layout sections), V0.hover-cards, V0.fix-flakes (9b, the layout mock clocks).

A full `make test` under load failed V0.164 desktop: the undeclared card read Babysit PR #4170 "for 1 min" where the check wants seconds. It passed alone.

(1) **The cause: the mock's load aged with the page.** `mockLoad()` is built once, when the mock Api is made at page load. It stamps Babysit's undeclared work 50 s before the rail's clock (14:32), and the front measures "for N" against its own clock, which runs on from 14:32. More than 10 s between page load and the hover made 50 s read "1 min". Alone the section reaches the card in a few seconds, and busy loops beside it did not push it past 10 s (3 of 3 passed). A 12 s wait before the meter's check reproduced it every run: the load card and the undeclared card failed, light and dark (4 of 4 checks).

(2) **The fix: each load read shows the scene as of that read.** The mock Api moves every undeclared start forward by the time since it made the load (`ageUndeclared` in `web/src/mock-load.ts`, called from the mock's `load()`), so the page reads 50 s and 3 min at any page age. The front reads the load every 5 s, so the span runs on by at most that (plus the time a poll is late) before the next read sets it back. The check's regex is unchanged. After the fix, with a 12 s and then a 45 s wait before the check, every V0.164 section passed (desktop and phone, light and dark; the wait was in the desktop sections).
