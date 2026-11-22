# V0.web-logic-cleanups

Pure-logic cleanups in the web client, no change a person sees but for the rounding below.

- One span/age formatter (`ago`, `span`, `daysBetween`, `minuteOf` in `format.ts`) replaces six copies. Spans now always
  round down (two copies rounded to nearest, so "1 min 31 s" read as "2 min"); a "checked" note past a day says days.
- One `paths.ts` for `tildePath`, `shortenPath`, `shortFolder`, `shortArg`; `projects.shortPath` is gone (it hard-coded
  `/home/`; `tildePath` takes the host's home).
- One tolerant `storedGet`/`storedSet`/`browserStorage` in `prefs.ts`; `storedSet` reports a blocked write.
- `signout.ts` and the handover dialog no longer call `fetch`; the Api does.
- `load.ts` is `host-load.ts`; `mock.ts` is split (`mock-changes.ts`, `mock-load.ts`, `mock-util.ts`), still reached
  only by `import()` from the mock Api.
- A pool's `held` and `waiting` lists may arrive as null (Go nil slice); the client reads them as empty
  (`withLists`, `stampSlots`). Found by `test/contract-dto.test.ts`, which removes each optional field of the quota,
  load and changes DTOs in turn.
- Exports nothing imports are no longer exported; `modelsText`, `useHasCPU`, `useThreadJob` are deleted.
