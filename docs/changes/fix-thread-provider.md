# fix-thread-provider

A fix thread started from an invariant card (Fix it) had no provider in its create args, so core gave it the literal
id `claude`. A host's providers carry their own ids, so the thread header found no match and showed the raw id as a
grey, unnamed chip. The thread ran, as the CLI default was used. `invariantFix` now names the host's first usable
provider.
