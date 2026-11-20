# V0.notification-polish: a notification says what the inbox row says, arrives once, and each device chooses whether finished threads notify

→ V0.15, V0.20, V0.54, V0.119, V0.140, V0.207, V0.210, V0.your-move, design/mobile rule 12. *Amends V0.15 (the body's words, the test, when a notification closes) and V0.119 (5) (the Notifications row's line).* 

**What they say.**

(1) **The row's words.** The body is the inbox row's bold word and line (`rowState`, `rowMeta`), built in `internal/front/notify.go` `AttentionDetail`. An effect ask no longer shows its action id: "Fyr action · approve pr.merge" is "Merge #12 · " and the agent's reason, or "Merge pull request #12" when it gave none; an update of Fyr is "Update Fyr · …". An approval names the command: "Approval · Allow Bash git push". A question beside a merge ask leads, as in Your move's Answer group.

(2) **No "Blocked".** A blocked thread's cause is its own sentence and stands alone, as on the row: "Add login: Merge", "Run: install the driver", "Agent not responding since 08:58: …". A question Fyr read in the agent's text says so: "Question in text · Shall I also fix the phone?".

(3) **One plain line.** The marks of bold and code are removed and line breaks become spaces, since a notification prints them as they are. A thread that finished with no last line says "Done", not "Done · ". With several hosts the host still comes first (V0.140).

**When they arrive.**

(4) **Once.** A thread that finishes, starts again and finishes inside the 20 s wait sent one push per finish. Every change of state now takes a number, and a waiting push goes out only while its number is the newest.

(5) **It stays until it is out of date.** A notification carries the state it was shown for, and a client closes it when the thread leaves that state: a question answered elsewhere, a failure retried, a finished thread read or started again. Before, any message about a thread that was still done or blocked closed its notification on a device with Fyr open, often within seconds. A notification from an older build keeps the old rule.

(6) **A device that was away gets the newest.** Each push carries a topic made from its thread, so the push service keeps only the last undelivered one per thread. A finished thread is sent with urgency normal, so it does not wake a phone in battery saving; everything that needs the owner stays high.

(7) Unchanged: asking and failed at once, blocked and done after 20 s if still true, nothing for a thread a device shows, nothing in a project's quiet hours (decision 0014), nothing for an auto-merge (V0.200), nothing for what a device hides (V0.207).

**The settings** (Settings → This device, one component for the desktop and the phone).

(8) **The line was wrong.** "When a thread asks you something or fails." left out blocked and done. It reads "When a thread needs you: a question, an approval, a failure."

(9) **Finished threads.** While notifications are on, a second switch: "Finished threads · Also when a thread finishes. Nothing arrives for the thread on screen or in a project's quiet hours." On by default, so nothing changes until the owner turns it off. It is this device's choice (`fyr.push.prefs` in the browser, `POST /api/push/prefs {endpoint, finished}` to the front, sent again when push turns on and on a reconnect, like the hidden list); the front leaves that subscription out of `done` pushes.

(10) **Send test reaches this device only.** `POST /api/push/test {endpoint}`; it used to buzz every device. The line then says "Sent to this device. It should arrive in a few seconds.", and when the push service takes none, "The push service did not take the test. Turn Notifications off and on again." A client from before names no endpoint and reaches every device, as before.

Tests: `internal/front/notifytext_test.go` (`TestPushSaysWhatTheRowSays`, `TestFrontPushesOnceForATurnThatEndsTwice`), `internal/front/browser_test.go` (`/push/prefs`, the test to one device), `internal/push/push_test.go` (`TestPayloadForIsOnePlainLine`, `TestFinishedIsAChoicePerDevice`), `web/test/push.test.ts` (`notificationIsStale`, `parsePushPrefs`, the row's lines).
