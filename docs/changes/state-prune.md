# V0.state-prune: a device drops the stored threads the host no longer lists

*Amends V0.38 and V0.state-paging.*

**Why.** A partial answer (`?since=`) only adds and replaces threads, so a thread the host has since dropped stayed in the device's stored copy for ever and took up room in the list.

1. **The first page of a partial or paged `GET /api/state`** (`since` or `limit` given, no `before`) carries `ids`: every thread id the host has now. The front's cached host state does the same. Later pages and whole answers have no `ids`.
2. **The device keeps only the stored threads on that list** as soon as the first page is in, before the older pages arrive. An answer without `ids` removes nothing.
