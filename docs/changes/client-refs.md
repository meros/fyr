# V0.client-refs: what a device keeps, a link and a push name a record by its host and a host by its install id

→ decision 0031 slice 3 (2.1, 2.5, 4.2, C11, C12, F15), V0.15, V0.29, V0.134, V0.169, V0.207. *Amends V0.169 (the hosts turned off are kept by install id, not by name).* (owner, 17 Nov, decision 0031: "All operations need to include what host to direct to"; 19 Nov: "Finish decision 0031".)

(1) **The device's keys, once.** After the first state that lists the hosts and their records, what the browser kept by a host's own id or a host's name moves, and `fyr.storage = 2` marks it done: a draft `fyr.draft.<id>` becomes `fyr.draft.<hostId>/<id>`; the project filter, New thread's project (`fyr.newThread`, which `fyr.lastProject` and `fyr.newDefaults` fold into) and the folded live views (`fyr.view.collapsed`) name refs; the hosts turned off (`fyr.hostsOff`) name install ids, a name no host has is dropped, and a host that reports no id keeps its name. A record that no single host has (none, or two with the id) keeps its old key; a draft under it is read as a fallback for one release and goes once the draft is written under the ref.

(2) **The host filter by id.** Row 1's toggles (V0.169) are keyed by the host's install id, so two hosts with one name are two toggles and a renamed host stays off. The name is what the toggle shows.

(3) **A push names its host.** The payload carries `host`, the install id of the thread's host, beside `threadId`. The notification's tag and the tap name the ref, so one thread id on two hosts is two notifications and a tap opens the thread on the host that sent it. A notification from an older front (no `host`) opens the one thread that has the id, and nothing when two have it (C12). The push service's topic is the hash of the ref.

(4) **A rotated id.** A host listed with a new id and `idWas`: the browser moves what it keeps under the old id to the new one (its keys, its stored timelines, the threads in memory, the thread on screen), and the front still answers `/api/h/<idWas>/…` for that host while it lists it so.

(5) **Already so before this change, and kept:** a deep link and Copy link name the ref (`/?open=<hostId>/<threadId>`), and a link with the id alone opens the one thread that has it; the desktop's history entries carry the ref; presence names the host; the project order stores refs and the host order ids.
