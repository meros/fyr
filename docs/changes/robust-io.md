# V0.robust-io: file and body handling that survives a crash and an oversize body

Review of main found small gaps, fixed together:

1. `identity.WriteNext` writes `identity.key.next` atomically; `LoadNext` removes a file that is no key and reports none (the roster is told of a new key only after the file is written, so nothing rests on a broken one), instead of failing every start.
2. `fsutil.SymlinkAtomic` uses a unique temporary name, so concurrent callers do not race; `fyr install` sets `previous` with it (no window without a link, and an error is returned, not swallowed).
3. `fsutil` syncs the parent folder after the rename, best effort.
4. The project-link call reads at most 2 MiB and reports an answer it cannot decode.
5. The peer handlers read bodies with `httpguard.DecodeJSON`: 413 when over the cap, 400 otherwise.
6. The peer sync loop waits its 50 ms settle time with the context.
7. The front's shared HTTP client has no Timeout on purpose (long-lived streams); commented.
