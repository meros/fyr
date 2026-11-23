# V0.state-paging: a device that was away for long syncs newest threads first, the rest in the background

*Amends V0.38 (a stored copy asks each host only for the threads changed since).*

**Why.** After a long absence nearly every thread has changed, so `GET /state?since=REV` answered with all of them. The client waited for the whole answer and meanwhile drew its stored rows, which by then were stale: the start was slow and showed old states.

1. **`GET /api/state` takes `limit=N` and `before=REV`.** It answers the newest N threads (changed after `since`, when given, and with a rev below `before`, when given), newest first. When older ones remain the answer has `more: true` and `cursor`, the oldest rev in the page as a string (a rev is above 2^53, so a number would round). The next page is `?limit=N&before=CURSOR`. Without `limit` nothing changes. The front's cached state for a host (`/api/h/<id>/state`) answers the same way.
2. **The client asks each host for its newest 60 threads first** and shows them at once, then fetches older pages of 120 in the background, 250 ms apart, and shows each as it arrives. A thread's newer copy from the stream is never replaced by an older page (the store keeps the higher rev).
3. **The stored state moves on only when a host's last page is in**, so a sync that is cut off (the tab closes, the stream drops) asks again from the same `since` and loses no thread.
