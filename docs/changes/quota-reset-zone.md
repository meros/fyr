# V0.quota-reset-zone: a quota notice's reset time is read in the zone it names, in more spellings

→ V0.quota-wait, V0.quota-auto-continue. *Amends V0.quota-wait (reset parsing).*

A thread stopped by "You've hit your weekly limit · resets 4am (Europe/Stockholm)" did not start waiting for the reset the first time, and the owner had to type "continue". The wait and the wake are armed only from `core.QuotaReset`; a notice it cannot read leaves the thread "Waiting for quota" with no reset and no auto-continue, and a notice it reads in the wrong zone resumes it hours early or late.

Root cause: two things kept a first limit from arming a wait.

1. `QuotaCanAutoContinue` refused any thread with an unread message in the outbox. The message that met the limit is refused by the provider and never reported read, so on a first limit the outbox was never empty and the thread stayed "Waiting for quota" with no continue armed (reproduced by `TestQuotaFirstLimitArmsWaitAndContinues/unread_message`). A later limit, after a delivered message, had an empty outbox and did continue. The outbox no longer blocks: the next process takes the unread message.
2. `resetsAt` dropped the parenthesised zone and read the clock in the daemon's zone, and knew only "4am", "23 Nov, 4am" and "Nov 23, 4am"; "resets Nov 24 at 4am" and "resets 4 am" parsed to nothing, so no continue was armed at all.

Fix: the outbox does not block; the zone in parentheses is loaded and used (a zone the host cannot load falls back to the failure's zone); "Nov 24 at 4am", "4 am", "Nov 2 3pm" and "2 Jan 3:04pm" are read. A time alone is today's, or tomorrow's once passed; a date is this year's, or next year's once passed.

Suspects cleared by tests: an error turn without a result and a clean-result notice are classified (`TestQuotaFailureFromAnErroredResult`, `TestQuotaFailureFromACleanResult`, `TestQuotaFailureMidTurnAfterEarlierProseAndTool`); a subagent's or prose-after-tool mention is not a stop (`TestSubagentOrProseAfterToolIsNotAQuotaStop`); open decisions, handover, settled, archived and running threads still do not auto-continue by design.

Tests: `TestQuotaReset`, `TestQuotaAutoContinueRespectsLifecycle` (`internal/core/quota_test.go`), `TestQuotaFirstLimitArmsWaitAndContinues` (`internal/engine/quota_wake_test.go`).
