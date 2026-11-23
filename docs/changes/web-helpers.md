# V0.web-helpers: the web client shortens text and home paths one way, and reads stored JSON through one decoder

→ V0.24 (`decode.ts` is the one place JSON becomes client types), V0.readable-approvals, V0.row-calm. *Amends them: the copies of the text helpers and the hand-written parses of stored values are gone.*

Why: five files each had their own `oneLine`, `clip` or `middleTruncate`, and they disagreed (one left a space before the "…", one flattened white space and one did not). The home folder was written as ~ by two regexes besides `tildePath`, and only the approval card knew `/Users/<u>`: a macOS path read `~/…` there and `/Users/u/…` everywhere else, and `/root` nowhere. Seven places parsed stored JSON by hand, one with an unchecked `as string[]`, so a corrupt value could reach the screen with the wrong type.

(1) `format.ts` holds `clip` (cut at the end, no space before the "…"), `oneLine` (white space flattened, then clipped), `firstLine` (the first line that is not blank) and `middleTruncate`. A cut never splits an emoji's surrogate pair. Every caller keeps its limit; the approval card's subject is still cut in the middle. The run card's own `firstLine` (the first line of a command, " …" when there are more) does something else and is renamed `firstCommandLine`.

(2) `paths.ts` `tildePath` writes `/home/<u>`, `/Users/<u>` and `/root` as ~, and is the only code that does: `relPath` and the approval card use it.

(3) `decode.ts` gains `parseJson(raw, decode, fallback)` and the decoders `asObject`, `strings` and `flags`; `prefs.ts` gains `storedJson(key, decode, fallback)` on top of `storedGet`. The fallback comes back for a missing key, text that does not parse, a shape the decoder refuses (it returns undefined), a decoder that throws, and a storage or storage accessor that throws. The hosts off, hidden records, section folds, settings folds, collapsed views, new-thread defaults, push preferences and the push payload read through them.
