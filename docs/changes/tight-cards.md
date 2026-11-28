# V0.tight-cards: the tight cards and strips show their whole information on both fronts: titles keep their ellipsis, host meters line up, the Status foot is in rows, and the phone's quota figure names its window

→ V0.166, V0.167, V0.quota-in-thread, V0.host-tabs, V0.wakeups-via-watch, V0.row-calm.

A visual review of the small surfaces (inbox rows, the load strip and sheet, the rail's meters, the Status page, the thread header) at 360 and 390 px and on the desktop found places where information was cut, misaligned or ambiguous.

(1) **Status cards.** A title on a "Your move" card or a running tile sat as bare text in a flex box, so a long title was cut off without its ellipsis ("Area tree production sw"). The title now has its own box and ends in "…".

(2) **Status foot.** Host meters, quota lines, versions and keys shared one wrapping line, so a quota could land under a host's second meter. The foot now has three rows: hosts (top-aligned), quotas, then versions and keys. On the phone the hosts sit in an even grid, so a host with one meter lines up with a host that has two.

(3) **Phone section strip.** At 360 px, seven sections left 2 px between "Waiting", "Snoozed" and "Settled", and the labels read as one word. Under 400 px the labels are 11 px, and a label that does not fit ends in "…".

(4) **Phone load strip.** A host with two meters and a host with one were centred vertically, so their names sat at different heights. They now start at the same top.

(5) **Phone load sheet.** With several hosts the tab already names the host, so the head drops the repeated name and the meters take the full width.

(6) **Phone thread header quota.** The single figure ("61 %") did not say what it counts. It now names the busiest window ("7 d 61 %").

(7) **Desktop finished rows.** A wide chip (a wake time) cut the row's line short ("acme/ops · Clau…"). Below 180 px of text, the chips now wrap to their own line, aligned right.

(8) **Desktop rail.** A host's undeclared count and its leftover count were two pills stacked under its meters. They now share one row.

No behaviour changes: nothing here appears by itself or moves the focus, and all of it is layout.
