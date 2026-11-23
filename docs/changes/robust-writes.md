# V0.robust-writes: writes are atomic, waits and subprocesses are bounded, errors are not swallowed

Why: a review of main found writes that a crash or a reader could meet half done, sleeps that outlive a shutdown, `ps` and `git worktree` calls with no deadline, and errors dropped on the floor.

(1) **Atomic writes** (`fsutil.WriteAtomic`): an attachment's `meta.json`, a handover file, the repo's `.git/info/exclude` (its error is now returned and logged by the caller, not dropped). The `copyFile` one-liners of `claudecli` and `agycli` are `fsutil.CopyFile` calls. `agycli`'s `settings.json` is the user's file: a read or JSON error now stops the launch with that error instead of writing a fresh file over it.

(2) **`fsutil` documents** what `WriteAtomic` does and does not do: it forces the mode, replaces a symlink, and a crash leaves `.name.tmp-*`. The readers of such folders (secrets, project icons, roster, peer, front cache, deploy) key on exact names or skip hidden entries, so no startup sweep is needed.

(3) **Waits end with the host.** `Manager.startOrWait` (`quit`, closed by `StopAll`), `Engine.SetupExisting(ctx, gap)`, the two delayed starts in `cmd/fyrd` (`w.ctx`); the restart after `POST /host/rotate` is a `time.AfterFunc`, not a sleeping goroutine. `runner.Server.Close` uses one deadline context instead of resetting a timer to zero.

(4) **`cgroup.PS`** is the one `ps` call (`proc.Command`, a 10 s deadline, C locale) for `cmd/fyrd` and the macOS probe. The `git` calls on worktrees in `cgroup` (`gitRun`) have deadlines (1 minute; 5 for `worktree add`).

(5) **No `http.DefaultClient`** without a timeout in `deploy` and `quota`: `httpguard.NewClient()`. The launcher says when it has no daemon log (the daemon still starts, its output goes to the null device).

Left as it is: `identity`'s key file is created with `O_EXCL` on purpose (two starts keep one key); the launcher's child is not killed by group, since the agents' processes belong to the launcher's own runner, not to fyrd.
