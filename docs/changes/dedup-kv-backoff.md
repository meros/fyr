# V0.dedup-kv-backoff: one KV interface, one Backoff, named timeouts

Review of main found the same things declared many times:

1. The key-value table interface was declared ten times (slots, front, github, heavy, auth, deploy and inline in cgroup). It is now `kvstore.Table`; `*store.Store` satisfies it and tests keep passing a map.
2. The reconnect loops of the front (500 ms to 15 s, reset after 30 s up) and of the peer mesh (1 s to 5 min with jitter, reset on success) share `httpguard.Backoff` (`Next`, `Reset`, tests).
3. Repeated literals have names saying what they are: `hostCallTimeout`, `hostReadTimeout`, `tailnetLookupTimeout` in the front, the dial, retry, accept and drain times in the mesh, and `httpguard.SmallBody` for the 4 KiB body caps. Values that only look alike (the cgroup hours, the core ten minutes) are left as they are, since they mean different things.
