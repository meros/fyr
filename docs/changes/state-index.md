# V0.state-index: a device syncs a host by index and diff, urgent threads first

*Replaces the way of V0.38 (`?since=`) and V0.state-paging (`limit`, `before`, cursor). The state's other fields, the stream and the timelines are as they were.*

**Why.** A long absence made the old sync slow and wrong in two ways. `?since=REV` answered nearly every thread at once and the device drew its stored rows, by then stale, until all of it arrived. And "what changed since" lives on a clock: it could not say that a thread was gone, a frame overtaken by a newer one was skipped for good (found live, 16 Nov), a cut-off sync started again from nothing, and a host whose clock went back was never read right. A list of what exists, compared thread by thread, has none of these.

1. **The host lists its threads.** `GET /api/state?index=1` answers the state without threads and with `index: [{id, rev, state}]`, one entry per thread, built without building the threads. `GET /api/state?ids=a,b,c` answers those threads only, at most 100. Both are `partial`. The front's cached state of an offline host answers both.
2. **The device diffs.** Its stored threads each carry their own rev, so the stored copy is its own progress: a stored thread not in the index is dropped (a thread the host deleted does not linger), one whose rev differs from the index's, or that is not stored, is fetched. The host's rev wins also when it is older. No "since", no cursor, no watermark.
3. **Urgent first.** What is fetched is ordered by the index's state (waiting for the owner, then moving, then finished, then the rest) and, within a state, newest first. The device draws what it holds at once. A device with nothing stored draws the first batch, not an empty list.
4. **In batches, in the background.** 40 threads per read, 30 ms apart, each drawn as it arrives, never replacing a newer copy the stream brought (the store keeps the higher rev). The stored state is saved every third batch and at the end, so a sync that is cut off keeps its progress and the next diff continues from it.
5. **Content stays on demand.** A thread's timeline is read when the thread is opened (V0.38) and never ahead. The stored timelines of threads the host no longer has are deleted with the diff.
6. **A host older than this change** ignores `index` and `ids` and answers its whole state; the device takes that as it is.
