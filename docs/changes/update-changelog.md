# V0.update-changelog: each update in Settings says what it brought, the commits since what ran before

→ V0.126 (#151), V0.173, decision 0027. 

(1) **The record.** When an update starts, Fyr notes in its record (kv `update_log`) the commit it counted from (`from`: the commit of the build that runs) and the commits the new build has that one lacks (`changes`: sha and the first line of the message, newest first, at most 40) and how many there are in all (`changesAll`). Best effort: a host where nothing ran before, or a list that cannot be read, gives a record without a changelog, and the update goes on.
- **A release:** one GitHub compare (`repos/<repo>/compare/<from>...<sha>`), once per update, counted with fyrd's other gh calls (V0.173).
- **A local build:** `git log <from>..<HEAD>` in the checkout; no API call. A checkout that does not hold `from` gives none.

(2) **The row.** A record with a changelog says "N changes" after its time, before its logs; the press opens the list under the row and closes it again (one open at a time). Each line is the commit's title, linked to the commit; a trailing "(#N)" becomes a link to that PR. Past the 40 kept, the last line "and N older" links to the whole compare. `/api/updates` carries `repo` (owner/name) for the links.

(3) **Fronts.** One component on both (`UpdatesGroup`). On the phone the toggle keeps `.lnk`'s 44 px touch height and each line gets a finger's height (each is a link).
