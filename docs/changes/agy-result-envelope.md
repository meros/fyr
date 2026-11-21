# V0.agy-result-envelope: a tool result is the command's own output, not `agy`'s envelope

→ amends V0.agy-adapter, V0.agy-reactive-wakeup; `internal/cli/agycli`.

`agy` never hands back a tool's output as it is. Every `run_command` result arrives wrapped:

```
Created At: 2026-11-21T04:52:01+01:00
Completed At: 2026-11-21T05:58:58+01:00
Command exited with code 0.

Command output:
Working...

Creating pull request for feature-one into main in acme/app

Working...
https://github.com/acme/app/pull/502
```

and a long one arrives as a preview of the middle, behind a sentence with a `file://` path to the full text under agy's brain folder. The adapter passed the wrapped text on, but every reader of a tool result in Fyr was written against the CLI's own output:

- **PR detection** reads a `gh pr create` result for a URL on a line of its own. The example above has the URL, but the timestamps and gh's `Working...` lines put it past the point where the check is useful, and a truncated result has no URL at all — only the path.
- **The owner** read the timestamps and the exit line above the output, in the payload of every tool row, which is not what the tool said.

1. **The envelope is cut (`internal/cli/agycli`).** `unwrapToolResult` removes the two timestamp lines, the exit-code line and the `Command output:` header, and leaves everything after it — a failure's output too. A result with no envelope of that shape is returned unchanged, so a tool that writes such text itself is never cut.
2. **A preview is read from its file.** When the result says it was truncated, the full output is read from the path it names — only a regular file under a `/.system_generated/` folder of agy's, at most 16 MiB. A file that is gone (the session folder was cleaned up) leaves the preview, with the same envelope cut.
3. **One place for it.** `extractToolResult` runs every shape it can produce (`content`, `rawOutput.output`) through it.

The tests use the live `gh pr create` result above and the live truncation sentence, and one that proves a `file://` path outside a brain folder is never read.
