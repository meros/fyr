# Pairing and discovery in Settings: implementation spec

The mockups are `design/pairing/i3.html` (final; parts A, B, C, tables D and E). The proposed CSS is part 1 of `design/pairing/pairing.css`; it builds on the V0.119 settings rows (`.sg`, `.sr`, `.pst`, `.tag`, `.note-err`). Where this spec and a mockup differ, this spec wins. Where neither says, keep today's behaviour.

The design covers all six slices of decision 0026. **Part 1 is what to build now.** Part 2 says what each later slice adds to the same pages, so a later slice only plugs in. Part 3 lists what is not drawn today.

Rules (CLAUDE.md): desktop and phone change in the same PR and share the component; a difference needs a reason written next to the code. Every difference this spec allows is listed with its reason.

## Words

| Word | Meaning | Shown as |
|---|---|---|
| install | one Fyr on one machine, known by its key | its id, `F7QK-2M9X-AB34-CD56`; short form `F7QK` |
| host | an install this front follows | a row on the Hosts page |
| device | a browser or a phone that opens this front | a row under Devices (slice 5) |
| route | the network that carries a link | one of: `this machine`, `tailnet`, `LAN`, `by address` |
| found | seen by a discovery source, not trusted | "Found, not paired" (slices 3, 4) |

Never "peer", "node" (except "tailnet node", Tailscale's own word), "roster" or "fingerprint" in the UI. The roster is "your installs" or "the installs you trust".

## The owner's choices

- Iteration 1 (16 Nov): **Hosts is its own Settings page**; **a host row opens the host's page**, and every action is there; **Devices joins This device**, which is renamed Devices in slice 5; **the front sends each host's address now**.
- Iteration 2 (16 Nov): the side that types a pairing code **gives only the code**; the tailnet shortcut has **a switch, on by default**; the pairing code is **six digits and a QR**; a reinstall that is the owner's own untagged tailnet node is trusted with **one press after a confirm**, never by itself.

---

## Part 1: build now (the next free V number; V0.124 is the newest when this was written)

Web, plus one field in Go. No trust changes: this is still slice 1 of 0026.

### 1.1 API: one new field

- `api.Host` gains `URL string` (`json:"url,omitempty"`): the URL the front dials for this host (`link.cfg.URL`, as configured or as discovery found it). `Front.hostLocked` fills it. Red test first in `internal/front` (a configured host and a found host both carry their URL in `hosts[]`, also while offline); add the field to `docs/API-v0.md` and the contract fixture if `hosts[]` is in it.
- The client's `HostInfo` gains `url?: string`; `decodeHosts` reads it.
- The client state keeps `hostId` (the front's own install id, already in `GET /api/state` since V0.124) so that a front with no `hosts[]` can still show this install's id. Pass it to `Settings` as `hostId`.
- The mock (`mockHosts`): add `url` to each row (`desk` `http://127.0.0.1:7777`, `meros-server` `http://100.64.0.9:7778`, `lab` `http://100.64.0.7:7778`), and one more host for the states: `old-nuc`, online, no `id`, `url` `http://old-nuc.lan:7778`. Keep `?hosts=2` as the switch.

### 1.2 The nav: a fifth page

| Page id | Title | Holds | Nav summary |
|---|---|---|---|
| `hosts` | Hosts | the host list | "3 hosts", "3 hosts · 1 offline", "3 hosts · 1 needs you"; with no `hosts[]` or one row: "This install only" |

- Order: Providers, Automation, Projects, **Hosts**, This device. Sub-page: `hosts/<name>` (the host's name, URL-encoded; `SUBS.hosts = true`).
- "Needs you" wins over "offline" in the summary. The count is the hosts with `idWas`.
- The amber badge (`.pst.needs-login`, the count) shows beside Hosts when a host needs you, as it does beside Providers. `SettingsNav` takes a badge per page instead of one number. The phone's More sheet line for Settings ("1 needs a login" / "2 need you") adds the hosts that need you to its count.
- `projectsSummary` loses the host count: "10 projects", "No projects". The Projects page loses its Hosts heading and group.
- URLs as V0.119: desktop `?dialog=settings&page=hosts` and `&page=hosts/lab`; phone `?settings=hosts` and `?settings=hosts/lab`. Esc and Back go from a host's page to the Hosts page, focus on the row it came from.

### 1.3 The Hosts page (A1, A6)

One `Group` of host rows, the front's own install first, then `hosts[]` in the front's order. No header, no buttons.

**The row** (`HostRow`, one component, the same on both fronts): a `button.sr.host` with `data-id` (the full id) and `data-host` (the name).

| Part | Content |
|---|---|
| mark | `.hdot`: online = filled `--calm`; offline = a ring; needs you = filled `--signal-ink`. `aria-hidden`; the line says the same in words |
| title | the name in mono; the tag "This install" when `name === self` |
| line | `<state> · <route> · id <short>` (below) |
| end | the badge "Needs you" (`.pst.needs-login`) when `idWas`; then the chevron |

The line, a pure function `hostLine(h, self)` in `hosts.ts` (it replaces today's):

- state: "online since 08:12"; "online since 08:12 · updating Fyr" (deploy phase busy, as today); "offline since 19:40". `since` uses `clockTime`; a day other than today reads "Mon 06:00" (use the helper the app already has for that; if `clockTime` gives only the time, extend it there).
- route: `routeOf(h.url, isSelf)`, new and pure:
  - self, or a loopback host (`localhost`, `127.0.0.0/8`, `::1`): "this machine";
  - a host in `100.64.0.0/10` or a name ending `.ts.net`: "tailnet";
  - a private address (`10/8`, `172.16/12`, `192.168/16`, `fc00::/7`, `169.254/16`) or a name ending `.local`, `.lan`, `.home.arpa`, or a name with no dot: "LAN";
  - anything else: "by address";
  - no `url` (a front older than this version): `found` gives "tailnet", else no route part.
- id: "id F7QK"; a host with no id: "no id".
- A changed id replaces the whole line: "Another install answers as lab: id B7LA, it was AX2C." in `--signal-ink`, and the row gets `.sr.bad` (the red edge). **An offline row is not red**: only a row that needs the owner is. `h.error` no longer colours the row; it shows on the host's page.
- The row no longer carries the Trust button (it is on the host's page, 1.4). `idChangedLine` loses its last sentence ("Trust it if you reinstalled…"), which moves to the page's note.

**The foot** of the group, always: "Fyr finds your other installs on the tailnet by itself. A host it cannot see goes in fyrd's config (`hosts`)." With one row it starts with "This is your only Fyr install. ".

With no connection to the front the page is read-only as every Settings page (the banner, `.ro`); the rows still open.

Phone: the same component. *Allowed difference: the row is 56 px high and the type is one step larger, from the one `.set.phone` rule set (touch).*

### 1.4 A host's page (A2, A3, A4)

`hosts/<name>`. Title: the name in mono. At the end of the title (desktop) or at the top of the body (phone, *reason: the phone's header carries only the title and Back*): the status pill, "Online", "Offline", or "Needs you" (amber). An unknown name shows the Hosts page.

**Group "Identity"**

| Row | Content |
|---|---|
| Id | the full id as four groups, the first in ink and bold (`.idf.big`), with **Copy** at the end. Under it: "Fyr remembers this id and tells you when another install answers under this name." For this install: "The id of the Fyr you are using. Your other installs show this id for <name>." |
| Id, none | "None. This host runs a Fyr older than V0.124, which has no install key. It shows an id after its next update." No Copy |
| Trust | not for this install. "Through your tailnet: Fyr follows it because it is a node of your Tailscale login, not because of its key." |

Copy writes the id with its dashes (`navigator.clipboard.writeText`); the button reads "Copied" for 2 s. Where the browser has no clipboard API the button is not rendered.

**Group "Connection"**

| Row | Content |
|---|---|
| state | title "Online since 08:31" or "Offline since 19:40". Under an offline one: `h.error` in `--signal-ink` with a capital and a full stop ("Unreachable."), then "Its threads show as last read and cannot be changed." Under an online one that updates: "Updating Fyr: <the phase in words, as the deploy row says it>" |
| Address | `h.url` in mono, " · ", the route. Not shown without `url` |
| origin | not for this install. `found`: title "Found on the tailnet", text "Fyr found it by itself. It leaves this list 30 days after its last answer." Else: title "Set in this front's config", text "It stays on this list until it is taken out of `hosts` there." |

**A changed id** (`idWas` set) (A4):

- Above Identity, a `.note.bad` (`role="alert"`): "**Another install answers as lab.** That is normal after you reinstalled or replaced lab. It is wrong if you did not."
- Identity then holds three rows in place of the Id row: "It was" + the old id in full + "The id Fyr first saw for lab."; "It is now" (`.sr.bad`) + the new id in full + Copy + "To check, open Fyr on lab: its Hosts page shows its own id."; and "Trust B7LA as lab" + "Fyr then remembers the new id and stops warning." with the button **Trust B7LA** (`front.hostId {host}`, as V0.124).
- The foot of Identity: "Fyr still follows lab meanwhile: today it trusts a host through your tailnet login, not its key. If you do not know this install, take that machine off your tailnet."
- No Trust row in place of the other Trust row: it is left out while the id is changed.
- After a Trust that answered ok: "✓ Saved" (the page's `SavedMark`), and the `hosts` stream event makes the page the ordinary one. A refused Trust shows the host's words on its row (`.sr.bad`, "Not trusted: desk answered “…”. Nothing changed.") and the button stays. Read-only: the button is disabled.
- Phone: Trust and Copy are 44 px high and inside the screen. *Reason: touch.*

### 1.5 This device: one row (A5)

A fourth row in the group, after Show tool details: title "How this browser gets in", no control. The text is a pure function of `location.hostname` and the state's `user`:

| The page's host | Text |
|---|---|
| ends in `.ts.net` | "Through Tailscale Serve, as <user>. Any browser signed in to your tailnet with that login can open this Fyr." |
| `localhost` or a loopback address | "On this machine itself (localhost). A browser on this machine is always allowed." |
| anything else | "At <hostname>, as <user>. What stands in front of Fyr there decides who gets in." (without ", as <user>" when the state has no user) |

### 1.6 Tests (red first)

- Go: `internal/front`: `hosts[].url` for a configured and a found host, online and offline. `make test-go`.
- `web/test/hosts.test.ts`: `routeOf` (every class above, IPv6 loopback, a bare name, no URL with and without `found`); `hostLine` (online, offline, updating, self, no id, a changed id, another day); `decodeHosts` reads `url`. Update the V0.124 cases that expect the old line.
- `web/test/settings-nav.test.ts`: five pages in order; `hostsSummary` (0 or 1 host, offline, needs you, both); the badge per page; `projectsSummary` without hosts; `parsePage("hosts/lab")`; `escapeTarget`.
- A unit test for the "How this browser gets in" text.
- `web/test/layout.e2e.mjs`, in `section()` blocks (the test runs 8 tabs since #142; take a slot with `mcp__fyr__slot_acquire` for the run). Replace the two V0.124 sections with, for desktop (1280 × 800) and phone (390 × 844), with `?mock=rail&hosts=2`:
  1. the nav has five pages; Hosts shows "· 1 needs you" and the badge "1"; Projects has no Hosts group;
  2. the Hosts page: every row's line (self "this machine", a tailnet one, the offline one not `.bad`, `old-nuc` "no id", lab's changed line with the badge); no button in a row; the foot; each row at least 56 px high on the phone;
  3. lab's page: the note, both ids in full, Trust B7LA inside the screen and 44 px on the phone; after the press the note is gone, the Id row shows B7LA…, the nav badge is gone;
  4. a plain host's page: the full id, Copy, the Address row with its route, the origin row; Back and Esc return to the list;
  5. This device has the new row.
  Screenshots with `shot()`: `hosts-<front>.png`, `host-<front>.png`, `host-changed-<front>.png`, `device-<front>.png`, and the desktop ones in dark.
- Save the screenshots under `design/pairing/impl/` (desktop and phone, as `design/settings/impl/after/`), and put the main ones in the PR.

### 1.7 Docs

A new V row in `docs/SPEC.md` (the next free number), which amends V0.124 (5) (the Trust button moves to the host's page; the row's line changes) and V0.119 (a fifth page). Update the Settings summary lines near the end of `docs/SPEC.md`, and `design/pairing/README.md` if a path changes.

---

## Part 2: what each later slice adds

The API names here are proposals for the slice that builds them; the pages, groups, order and copy are the design.

### The Hosts page, all slices in (B1)

Top to bottom: the page's two buttons, **Add by address…** and **Pair…** (at the end of the title on desktop; a bar at the top of the body on the phone, *reason: the phone's header has no room*); a note when Fyr did something by itself; the hosts; **Found, not paired**; **Where Fyr looks**; the folds "N ignored" and "N removed". A group with nothing to show is not rendered, except Found once any source is on (it then says "Nothing found.").

### Slice 2: roster and pinned TLS; the tailnet shortcut

- `hosts[].trust`: `{how: "auto" | "code" | "login", at, by, via?}` (`by`: the install that paired it; `via`: the install this one learned it from). The host page's Trust row becomes: "Paired by itself" + "16 Nov 20:31, by desk. It is an untagged node of your Tailscale user, alexander@github." / "Paired by code" + "16 Nov 21:04, on desk." (+ " desk learned it from meros-laptop.") / `login`: the row line ends "· not paired: its Fyr is older than pairing" and the Trust row keeps today's text.
- `hosts[].addrs`: `[{addr, via: "tailnet" | "lan" | "address" | "local", used, seen, state}]`. The single Address row becomes the group **Addresses** (one row per address: the address in mono; "Tailnet · in use · answered just now"), with the foot "Fyr dials whichever address answers with this key." The row's route joins them: "tailnet and LAN".
- **History** group on the host's page: the roster statements about this install, newest first (`.hist`).
- **The note** (B6), once per event, with OK: "**Fyr paired meros-laptop and lab by itself.** They are untagged nodes of your Tailscale user, alexander@github, so they need no code. A tagged node pairs by code." The rows carry the tag "Paired by itself" until the note is dismissed.
- **The switch** in "Where Fyr looks", under the Tailnet row: "Pair my own tailnet nodes without a code" / "Untagged nodes of alexander@github. Off: they show under Found and pair by code." On by default. Off never unpairs anything.
- **A changed id** (B8): the new key is refused. The note reads "**Another install answers as lab, and Fyr refuses it.** lab's threads show as last read until you decide." The Trust row is replaced by two rows: "It is lab, reinstalled" and "I do not know it" + **Block B7LA** (red, with a confirm; the id goes to Ignored). When the tailnet says the new install is the owner's own untagged node, the first row's button is **Trust B7LA…** and a confirm names both ids in full (B8b); the old id is removed. A taken name is never given to a new id without that press.
- This install's page gains **Your installs**: "4 installs, in step" + "This install's list of trusted installs matches the others. Last compared with meros-laptop just now."

### Slice 3: pairing by code; add by address; `fyrd pair`

- **Pair…** opens `hosts/pair` (B3). One page, a segmented control: **Show a code** | **Enter a code**.
  - *Show*: the QR and the six digits as `482 913` (`.pcode`), beside them "This install", its name and full id. Two steps: "On the other machine, open Fyr: Settings, Hosts, Pair, **Enter a code**. Type the digits, or scan the picture with its camera." and "If it has no screen, run this there: `fyrd pair 482913`" with Copy. Status line and **Cancel**: "Waiting for the other install · the code ends at 21:09" (a time, not a countdown: nothing moves on its own). Foot: "The code works once, for 5 minutes. Three wrong tries cancel it."
  - *Enter*: one field (`inputmode="numeric"`, `autocomplete="one-time-code"`, shown as `000 000`), **Scan…** where the browser has a camera (`navigator.mediaDevices`; *the one allowed difference: it depends on the device, not on the layout*), "This install" with its id, and **Pair** (primary, disabled until six digits). Foot: "Fyr asks the installs it has found for the one that shows this code. A code tried on a wrong install tells it nothing." The address field ("Its address (only when Fyr cannot find it)") appears only after "no install shows a code".
  - The QR holds the code, the showing install's id and its addresses, so a scan needs no search.
  - Leaving the page while a code is shown cancels the code, after a confirm.
- **States** (B3b, B4). Showing side: one wrong try ("A wrong code was tried from 192.168.1.31 (1 of 3). This code still works until 21:09."); three ("Three wrong codes were tried from …, so this code is cancelled. Nothing was paired." + **Show a new code**, the digits struck through); ended ("The code ended at 21:09 unused."); done ("**Paired with meros-server**", its id large, this install's id, "meros-server shows the same two ids. Your other installs learn of it within a minute.", **Open meros-server**, **Done**). Typing side: checking (field locked, "Looking for the install that shows this code · tried 1 of 2", **Cancel**); wrong ("desk did not accept that code. 2 tries left."); cancelled; none found; an address that does not answer; the other side cancelled ("The other install cancelled."); done (the mirror of the showing side).
- **The fingerprint check** is the done state: both sides show the same two full ids. While waiting, each side shows its own. The copy never asks the owner to compare before typing: the code already binds both ids.
- **`fyrd pair`** (B3b, B4): `fyrd pair <code> [address]` types a code; `fyrd pair` with no code shows one and waits (two machines without screens). It prints "This install <name> <id>", each install it asks, then "Paired with desk <id>" and "desk shows the same two ids."; errors in the words of the page.
- **Add by address…** opens `hosts/add` (B7): one field and **Look up**. Results: a Fyr install answers (its id large; "It is not paired. The address is kept either way; it shows under Found."; **Pair…**, which opens `hosts/pair` with the code bound to that id); already paired ("That is meros-laptop (K2VD), which you already trust. Fyr now also dials it there."); no answer; not Fyr; this install; removed earlier.
- **Pair… on a found install** (B5b): the Show side with "A code for **Fyr install Q8WN-… at 192.168.1.31**. No other install can use it." and no segmented control.
- Found shows tagged tailnet nodes ("a tagged node, so it pairs by code").
- A changed id's first row gets **Pair again…** (the bound pairing page; on success the old id is removed everywhere).
- Empty Hosts (C4): Pair… is the page's primary button; the list ends with "**No other install** Start Fyr on another machine. On this LAN or your tailnet it shows below by itself; then pair it."

### Slice 4: mDNS; found installs; Ignore

- **Found, not paired** (B5): header "Found, not paired" with, at its end, "Looked 20 s ago" and **Look now**. A row (`.sr.acts`): the mark `.hdot.new`; title "Fyr install " + the full id (an unpaired install has no name; a tailnet one is titled by its tailnet name in mono, then the id); line "<address> · found on this LAN · first seen 20:58"; **Ignore** and **Pair…**. Phone: the two buttons go under the text (*reason: two 44 px buttons do not fit beside it*).
  - One id at several addresses is one row; the line lists them and ends "· one install at two addresses".
  - An install at a paired host's address, or with a paired host's tailnet name, is `.sr.bad` with the reason ("This address was lab's (AX2C) until 20:12. Pair it only if you reinstalled lab." / "You already have a host named lab with another id (AX2C).") and **Review…** in place of Pair…, which opens that host's page (the changed id).
  - More than three: three rows and "Show 11 more". On a shared network the foot adds: "A shared network shows other people's Fyr installs too. They cannot reach yours."
  - Foot: "Any Fyr on this LAN or your tailnet shows here. Being found gives it nothing: only pairing does. An install you do not know is probably someone else's: ignore it."
- **Ignored** (fold, B5b): per install on this front only; a row has "Ignored 14 Nov · last seen today 20:58 at 192.168.1.50" or "Blocked 9 Nov: it answered as lab" and **Stop ignoring**. Foot: "Ignored on this install only. An ignored install is never dialled and never shown as found."
- **Where Fyr looks** (B1, C5): one row per source with its state in words. Tailnet: "On · alexander@github · 5 nodes, 4 run Fyr" / "Off: tailscaled does not answer on this machine. Fyr looks on the LAN and at the addresses you gave." / "Signed out: …" / an error. This LAN: "On · 192.168.1.0/24 · 2 installs answer" / "Blocked: Fyr's own announcement does not come back, so this network drops multicast. That is common on guest and office Wi-Fi and between VLANs. Add the other install by address." with **Add by address…** / "No network: …". By address: each address is a row ("Answers · vps (V9PS)" / "No answer since Sun 22:15") with × ("Forget <address>"; it never removes a paired host).
- **A host on another network** (C6): the row reads "offline since 19:40 · by address · no address answers from this network"; its page opens with a `.note.warn` and lists each address with its last answer.
- Loading and a failed look (C9): still skeleton rows; a `LoadError` with Try again that leaves the host list alone.

### Slice 5: the browser off the tailnet

- "This device" becomes **Devices** (`device` keeps its page id). Group "This device" (today's rows, without "How this browser gets in"). Group **Devices that can open this Fyr**: this browser first with the tag "This device"; each paired device "Pixel 9 · Chrome" + "Paired 3 Nov · last used today 20:41" + **Revoke…** (red, confirm); the ways in that need no pairing are listed without a button ("This browser · On desk itself (localhost) · always allowed, never listed as a device"; "Anyone on your tailnet as alexander@github · Through Tailscale Serve · turned on in this install's config, not here"). Foot: "A paired device that is not used for 90 days is signed out." Then **Add a device…**. Nav summary: "This device · 2 others".
- **Add a device** (`device/add`, B9): the QR (this front's origin and a one-time token), "No camera? Open `<origin>/pair` there and type `7K4M-2QXD`.", the status line with the end time, Cancel. Foot: "The code works once, for 5 minutes. The device must already trust this Fyr's certificate; that is set up in the install, not here." Done: "**Pixel 9 · Chrome was added**".
- **Pages outside the app** (B9b), no data behind them: "Pair this device with desk?" (the name field, **Pair this device**); "That code no longer works"; "This device is not paired"; "This device was revoked".
- Revoking the device in use says so in its confirm.

### Slice 6: removal and rotation through the roster

- **Remove…** on a host's page (last group, danger): "Remove meros-laptop" / "Every install refuses it once it hears. Only a new pairing by code brings it back." For an install paired by itself the text starts "The undo of the pairing." and adds that Fyr does not pair it by itself again. Not on this install's own page ("To remove this install, do it from another one.").
- After Remove (C1): a note with how far it spread ("**meros-laptop is removed.** 2 of your 3 other installs have heard. meros-server is offline and learns it when it is back; until then it still lets meros-laptop in."); the host moves to the group **Removed** (struck name, "removed today 21:30 on desk · told 2 of 3 · id K2VD"); its page shows who has not heard, its history, "Its threads … 30 days", and **Pair again…**. A host that has not heard says so on its row.
- On the other installs (C2): the note "**desk removed meros-laptop** today at 21:30. This install refuses it now."
- On the removed machine (C2): a `.note.bad` at the top of Hosts, the nav summary "This install was removed" with the badge, the other hosts as "refuses this install since 21:30", and Pair… as the primary button.
- **A new key** (C3): calm. The row has the tag "New key" for 7 days; the Id row says "A new key since 2 Dec 09:10. lab made it itself and signed the change with its old key (B7LA), so nothing is asked of you."; History has the line. No badge, no note.
- **A list that is behind** (C7): this install's "Your installs" row turns `.sr.bad`: "4 installs, last in step 3 days ago" / "This install has reached none of the others since 13 Nov 18:02. A pairing or a removal made on another install since then is not known here."; the Hosts page opens with a `.note.warn`.

---

## Part 3: what is not drawn in the first build

No disabled buttons and no "coming soon". A control appears with the slice that makes it work. What stands in its place:

| Not shown now | What says the truth instead | Slice |
|---|---|---|
| Pair…, Add by address… | the list's foot: "Fyr finds your other installs on the tailnet by itself. A host it cannot see goes in fyrd's config (`hosts`)." | 3 |
| Found, not paired; Ignore; Ignored | nothing: today every install Fyr finds is followed at once and its row says "tailnet" | 3, 4 |
| Where Fyr looks; the shortcut switch | nothing | 2 to 4 |
| Remove…; Removed | the origin row: "Found on the tailnet … leaves this list 30 days after its last answer" or "Set in this front's config … until it is taken out of `hosts` there" | 6 |
| Block; Pair again… | the foot of a changed id: "Fyr still follows lab meanwhile … take that machine off your tailnet." | 2, 3 |
| how it was paired | the Trust row: "Through your tailnet … not because of its key." | 2 |
| Addresses (several); History; Your installs | one Address row | 2, 4, 6 |
| the device list; Add a device; Revoke | the row "How this browser gets in" | 5 |

## New CSS (copy from `pairing.css` part 1)

Now: `.hdot` (+ `.off`, `.warn`), `.idf` (+ `.big`), `.sr.host`, `.note` (+ `.bad`), and the phone sizes beside them. Later slices: `.hdot.new`, `.hdot.gone`, `.sr.gone`, `.sr.acts`, `.note.warn`, `.pst.bad`, `.pair*`, `.pcode*`, `.steps`, `.cmd`, `.qr`, `.hist`, `.gate`. `.term` is for the mockups only.
