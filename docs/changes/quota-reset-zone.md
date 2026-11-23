# V0.quota-reset-zone: a quota notice's reset time is read in the zone it names, in more spellings

→ V0.quota-wait, V0.quota-auto-continue. *Amends V0.quota-wait (reset parsing).*

A thread stopped by "You've hit your weekly limit · resets 4am (Europe/Stockholm)" did not start waiting for the reset the first time, and the owner had to type "continue". The wait and the wake are armed only from `core.QuotaReset`; a notice it cannot read leaves the thread "Waiting for quota" with no reset and no auto-continue, and a notice it reads in the wrong zone resumes it hours early or late.

Root cause (by reading the code; see the limits below): `resetsAt` dropped the parenthesised zone and read the clock in the zone of the failure's time (the daemon's), so a daemon outside the CLI's zone got a wrong instant, and it knew only "4am", "23 Nov, 4am" and "Nov 23, 4am". "resets Nov 24 at 4am" and "resets 4 am" parsed to nothing, so no auto-continue was ever armed.

1. The zone in parentheses is loaded and used; a zone the host cannot load falls back to the failure's zone.
2. "Nov 24 at 4am", "4 am", "Nov 2 3pm" and "2 Jan 3:04pm" are read. A time alone is today's, or tomorrow's once it has passed; a date is this year's, or next year's once it has passed.

Tests: `TestQuotaReset` (`internal/core/quota_test.go`), a case per spelling.

Not verified: the change was written while the host held heavy commands, so no test ran here. The report could not be reproduced end to end (no engine simulation was run), and the other suspects (an error turn without a result, a subagent's limit reported by another thread, a non-empty outbox or pending decision blocking `QuotaCanAutoContinue`) were read, not tested.
