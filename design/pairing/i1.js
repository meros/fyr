// Iteration 1: structure. Where hosts, pairing and devices live, what a host row and a host page hold, and how
// today's data (V0.124) and the later slices of decision 0026 fill the same frame. i2.js and i3.js reuse these pages
// (window.LATER stops the render here).

const FOOT_NOW = "Fyr finds your other installs on the tailnet by itself. A host it cannot see goes in fyrd's config (<span class=\"mono\">hosts</span>).";

// ---- the Hosts page ----
/** Today: hosts[] of V0.124 (name, online, since, error, found, id, idWas, deploy). */
const hostsNow = {
  nav: "hosts",
  navset: "need",
  title: "Hosts",
  body: group(
    "",
    hrow({ name: "desk", self: 1, line: `online since 08:12 · id ${sid(ID.desk)}` }) +
      hrow({ name: "meros-server", state: "off", line: `offline since 19:40 · unreachable · tailnet · id ${sid(ID.server)}` }) +
      hrow({ name: "lab", state: "warn", err: 1, line: `Another install answers as lab: id ${sid(ID.labNew)}, it was ${sid(ID.labOld)}.`, badge: "Needs you", badgeCls: "needs-login" }),
    FOOT_NOW,
  ),
};

const CANDS =
  crow({ title: `Fyr install ${idf(ID.other)}`, line: "192.168.1.31:7778 · found on this LAN · first seen 20:58" }) +
  crow({ title: `<span class="mono">nas</span> ${idf(ID.nas)}`, line: "nas.tail1234.ts.net · found on the tailnet · a tagged node, so it pairs by code" });
const SOURCES =
  row("Tailnet", "On · alexander@github · 5 nodes, 4 run Fyr") +
  row("This LAN", "On · 192.168.1.0/24 · 2 installs answer") +
  row("By address", "1 address · vps.example.net:7778");

/** After slice 6: the roster, the candidates, the sources. */
const hostsFull = {
  nav: "hosts",
  navset: "full",
  title: "Hosts",
  end: btn("Add by address…") + btn("Pair…"),
  body:
    group(
      "",
      hrow({ name: "desk", self: 1, line: `online since 08:12 · id ${sid(ID.desk)}` }) +
        hrow({ name: "meros-laptop", line: `online since 08:31 · tailnet and LAN · id ${sid(ID.laptop)}` }) +
        hrow({ name: "meros-server", state: "off", line: `offline since 19:40 · tailnet · id ${sid(ID.server)}` }) +
        hrow({ name: "vps", line: `online since Mon 06:00 · by address · id ${sid(ID.vps)}` }),
      "The installs you trust. Each one knows this list and refuses everything else.",
    ) +
    group(`Found, not paired <span class="end">Looked 20 s ago${lnk("Look now")}</span>`, CANDS, "Any Fyr on this LAN or your tailnet shows here. Being found gives it nothing: only pairing does.") +
    group("Where Fyr looks", SOURCES) +
    `<div class="sbar">${fold("1 ignored")}${fold("1 removed")}</div>`,
};

// ---- a host ----
const hostNow = {
  nav: "hosts",
  navset: "need",
  parent: "Hosts",
  title: "meros-server",
  mono: 1,
  end: pst("Offline"),
  body:
    group("Identity", idrow(ID.server, "Fyr remembers this id and tells you when another install answers under this name.") + row("Trust", "Through your tailnet: Fyr follows it because it is a node of your Tailscale login, not because of its key.")) +
    group("Connection", row("Offline since 19:40", "Unreachable. Its threads show as last read and cannot be changed.") + row("Found on the tailnet", "Fyr found it by itself. It leaves this list 30 days after its last answer.")),
};
const hostFull = {
  nav: "hosts",
  navset: "full",
  parent: "Hosts",
  title: "meros-laptop",
  mono: 1,
  end: pst("Online"),
  body:
    group("Identity", idrow(ID.laptop) + row("Paired automatically", "16 Nov 20:31, by desk. It is an untagged node of your Tailscale user, alexander@github.") + row("Front and host", "Build 20261116-2030")) +
    group("Addresses", arow("meros-laptop.tail1234.ts.net:7778", "Tailnet · in use · answered just now") + arow("192.168.1.23:7778", "This LAN · answered 20:58"), "Fyr dials whichever address answers with this key.") +
    group("History", hist([["16 Nov 20:31", "Paired automatically by desk (tailnet)"], ["16 Nov 20:31", "First seen on the tailnet"]])) +
    group("", row("Remove meros-laptop", "Every install refuses it once it hears. Only a new pairing by code brings it back.", btn("Remove…", "danger")), "", "danger-z"),
};

// ---- the states of a row ----
const rowStates = `<div class="bd-f"><span class="bd-cap">A host row · every state</span><div class="dlgx one"><div class="stp">${group(
  "Today (V0.124 data)",
  hrow({ name: "desk", self: 1, line: `online since 08:12 · id ${sid(ID.desk)}` }) +
    hrow({ name: "meros-laptop", line: `online since 08:31 · tailnet · id ${sid(ID.laptop)}` }) +
    hrow({ name: "build-box", line: "online since Mon 06:00 · updating Fyr · configured · id T5BX" }) +
    hrow({ name: "meros-server", state: "off", line: `offline since 19:40 · unreachable · tailnet · id ${sid(ID.server)}` }) +
    hrow({ name: "old-nuc", line: "online since 07:02 · configured · no id: its Fyr is older than V0.124" }) +
    hrow({ name: "lab", state: "warn", err: 1, line: `Another install answers as lab: id ${sid(ID.labNew)}, it was ${sid(ID.labOld)}.`, badge: "Needs you", badgeCls: "needs-login" }),
)}${group(
  "Added by the later slices",
  hrow({ name: "meros-laptop", line: `online since 08:31 · tailnet and LAN · id ${sid(ID.laptop)}` }) +
    hrow({ name: "vps", state: "off", line: `offline since 19:40 · by address · no address answers from this network · id ${sid(ID.vps)}` }) +
    hrow({ name: "lab", line: `online since 20:40 · LAN · id K9RT`, tag: tag("New key", "new") }) +
    hrow({ name: "old-laptop", state: "gone", line: `removed 12 Nov on desk · id H2OL`, badge: "Removed" }),
)}</div></div></div>`;

// ---- pairing, first cut ----
const pairCut = {
  nav: "hosts",
  navset: "full",
  parent: "Hosts",
  title: "Pair",
  body:
    `<div class="sg"><div class="sg-c"><div class="pair"><span class="seg" role="radiogroup" aria-label="Which side"><button class="on">Show a code</button><button>Enter a code</button></span><div class="pair-top"><span class="pcode">482 913</span><span class="pair-who"><small>This install</small><span class="mono"><b>desk</b></span>${idf(ID.desk)}</span></div><ol class="steps"><li>On the other machine, open Fyr: Settings, Hosts, Pair, <b>Enter a code</b>.</li><li>On a machine with no screen:<div class="cmd"><code>fyrd pair 482913</code>${lnk("Copy")}</div></li></ol><div class="setbar"><span class="pair-st" style="margin-right:auto">Waiting for the other install · the code ends at 21:09</span>${btn("Cancel")}</div></div></div></div>`,
};

// ---- devices (slice 5) ----
const devicesCut = {
  nav: "device",
  navset: "full",
  title: "Devices",
  body:
    group(
      "This device",
      row("Notifications", "When a thread asks you something or fails.", swt(false, "Notifications")) +
        `<div class="sr stack"><span class="sr-b"><span class="sr-t">Theme</span></span><span class="sr-e"><span class="seg" role="radiogroup" aria-label="Theme"><button class="on">Auto</button><button>Light</button><button>Dark</button></span></span></div>` +
        row("Show tool details", "Every tool group open, every machine line in full.", swt(false, "Show tool details")),
      "Kept in this browser. Each device has its own.",
    ) +
    group(
      "Devices that can open this Fyr",
      row(`MacBook · Firefox${tag("This device")}`, "This machine's own browser · always allowed") +
        row("Pixel 9 · Chrome", "Paired 3 Nov · last used today 20:41", btn("Revoke…", "danger")) +
        row("iPad · Safari", "Paired 28 Oct · last used 9 Nov", btn("Revoke…", "danger")),
      "A device that is not used for 90 days is signed out.",
    ) +
    `<div class="sbar">${btn("Add a device…")}</div>`,
};

// ---- the alternatives ----
const altNav = (cap, items, why) => `<div class="bd-f"><span class="bd-cap">${cap}</span><div class="phx" style="min-height:0"><div class="m-hd"><button class="m-bk" aria-label="Back">‹</button><span class="m-tt">Settings</span></div><div class="m-body">${nav(null, items)}</div></div><div class="bd-callout">${why}</div></div>`;
const ALT_A = altNav("A · Hosts is a page; Devices joins This device (recommended)", NAVS.full, "Two things, two places: installs are trusted by key (Hosts), browsers by a session (Devices). It is also how decision 0026 names them. Until slice 5 the last page stays “This device”.");
const ALT_B = altNav(
  "B · Hosts stays under Projects",
  NAVS.now.filter((i) => i.id !== "hosts").map((i) => (i.id === "projects" ? { ...i, sub: "10 projects · 4 hosts · 2 found" } : i.id === "device" ? { ...i, title: "Devices", sub: "This device · 2 others" } : i)),
  "Four pages, as you chose on 16 Nov. But pairing, the found installs and a warning then sit under a project list that grows without limit, and the badge would be on “Projects”.",
);
const ALT_C = altNav(
  "C · One page: Hosts and devices",
  NAVS.now.map((i) => (i.id === "hosts" ? { ...i, title: "Hosts and devices", sub: "4 hosts · 2 found · 3 devices" } : i)),
  "One place for “who can reach my Fyr”. But “This device” (theme, notifications) stays separate, so a phone's own row and its settings are on two pages, and the page is long.",
);

const SLICES = `<table class="bd-tbl"><tr><th>On the page</th><th class="now">Now (V0.124 data)</th><th>Slice 2 · roster, mTLS</th><th>Slice 3 · code, URL</th><th>Slice 4 · mDNS</th><th>Slice 5 · browser</th><th>Slice 6 · removal</th></tr>
<tr><td>Hosts page, host rows</td><td class="now">name, online, since, route (tailnet or configured), short id</td><td>trust says “paired”; both routes</td><td>“by address”</td><td>“LAN”; several addresses</td><td></td><td>“New key”, “Removed”</td></tr>
<tr><td>A host's page</td><td class="now">full id, Copy, trust in words, connection</td><td>how it was paired; History</td><td></td><td>Addresses</td><td></td><td>Remove…</td></tr>
<tr><td>A changed id</td><td class="now">warning, both ids in full, Trust</td><td>the new key is refused until you decide</td><td>Pair again…, Block</td><td></td><td></td><td></td></tr>
<tr><td>Pair…</td><td class="now">not shown</td><td>the note “paired automatically”</td><td>Show a code, Enter a code, <span class="mono">fyrd pair</span></td><td></td><td></td><td></td></tr>
<tr><td>Add by address…</td><td class="now">not shown; the foot names the config</td><td></td><td>the form</td><td></td><td></td><td></td></tr>
<tr><td>Found, not paired</td><td class="now">not shown</td><td></td><td>tagged tailnet nodes</td><td>installs on the LAN; Ignore</td><td></td><td>tagged peers from the tailnet source</td></tr>
<tr><td>Where Fyr looks</td><td class="now">not shown</td><td>Tailnet</td><td>By address</td><td>This LAN</td><td></td><td></td></tr>
<tr><td>Devices</td><td class="now">“This device” as today</td><td></td><td></td><td></td><td>the list, Add a device, Revoke</td><td></td></tr></table>`;

if (!window.LATER)
  render({
    title: "Pairing and discovery in Settings · iteration 1: structure",
    sub: "where it lives and what a host is, before the flows",
    note: h`<ul>
<li><b>A new page, Hosts.</b> Today Hosts is the last group of Projects. Decision 0026 gives it an id per host, a trust state, the found installs, pairing, adding by address and removal. That is a page of its own (A1). Projects loses the group; its nav line becomes “10 projects”.</li>
<li><b>A host row opens the host's page</b> (A4), as a provider row does. The row carries one line: state, route, short id. Everything that needs care (the full id, a changed id, Remove) is on the host's page, where both ids can be read in full before a press.</li>
<li><b>The same frame today and later.</b> A2 and A4 are what today's data (V0.124) can fill. A3 is the page after slice 6. A group that has no data is not shown: no dead buttons. The table at the end says which slice adds what.</li>
<li><b>Words.</b> An <b>install</b> is one Fyr on one machine, known by its <b>id</b> (<span class="mono">F7QK-2M9X-AB34-CD56</span>, short <span class="mono">F7QK</span>). A <b>host</b> is an install this Fyr follows. A <b>device</b> is a browser or a phone that opens this Fyr. The route is one of: tailnet, LAN, by address.</li>
<li><b>One nav badge.</b> “Hosts” counts what needs you (a changed id, a name taken by another install), as Providers does.</li>
<li>Iteration 2 draws the flows (pairing by code on both sides, the found installs, the tailnet shortcut, a changed id, devices). Iteration 3 draws every state.</li></ul>`,
    screens: [
      { h: `A1. Where it lives: Hosts is a page`, why: "Desktop: the page as it will be when all six slices are in. Phone: the way in, five rows.", page: hostsFull, phonePage: { index: true, navset: "full" } },
      { h: `A2. Hosts today ${sl(1)}`, why: "What ships first, on today's API. The same rows; no buttons, because today nothing can be paired, added or removed from here. The foot says how a host gets on the list now. lab has a changed id, so the nav counts it.", page: hostsNow },
      { h: "A3. Hosts when all slices are in", why: "Four groups, in the order you need them: the installs you trust, the ones Fyr found, where it looks, and what you turned away. Pair… and Add by address… are the page's two actions.", page: hostsFull, desktop: false },
      { h: `A4. A host: today ${sl(1)}, and when all slices are in`, why: "Left: today. The full id with Copy, what trust rests on today, and the connection. Right: later the same page gains how it was paired, its addresses, its history and Remove.", raw: frame("Desktop · today", desk(hostNow), "bd-desk") + frame("Phone · today", phone({ ...hostNow, short: 1 }), "bd-phone") + frame("Desktop · all slices", desk(hostFull), "bd-desk") + frame("Phone · all slices", phone(hostFull), "bd-phone") },
      { h: "A5. A host row, every state", why: "One line: state, route, short id. A mark and words, never colour alone. A row that needs you is red-edged and carries the amber badge.", raw: rowStates },
      { h: `A6. Pair: a sub-page, first cut <span class="slice">Slice 3</span>`, why: "Pair… opens a page under Hosts, as Add provider does. The flows come in iteration 2.", page: pairCut },
      { h: `A7. Devices <span class="slice">Slice 5</span>`, why: "When device pairing arrives, “This device” becomes “Devices”: this device's own settings first, then every device that can open this Fyr. Your own setup (Tailscale Serve and this machine's browser) needs no pairing and lists no devices.", page: devicesCut },
      { h: "B. Three ways to place it", why: "The phone's Settings screen for each. A is drawn above.", raw: ALT_A + ALT_B + ALT_C },
      { h: "C. Which slice fills what", why: "", raw: SLICES },
    ],
  });
