// Iteration 3: the final set. Part A is what to build now, on V0.124's data plus hosts[].url. Part B is every
// later screen, as the slices of decision 0026 fill it. Part C is the states: removal, rotation, empty, errors.
// Loads after i1.js and i2.js and reuses their pages. Build from this one, with SPEC.md.

const NOW = sl(1);
const s = (n) => sl(n);
const ask = (cap, title, text, ok, danger) =>
  `<div class="bd-f"><span class="bd-cap">${cap}</span><div class="dlgx one" style="width:400px"><div class="dh">${title}</div><div class="stp"><p class="lede" style="color:var(--ink)">${text}</p><div class="setbar">${btn("Cancel")}${btn(ok, danger ? "danger" : "p")}</div></div></div></div>`;

// ================= A. build now =================
const R = {
  desk: hrow({ name: "desk", self: 1, line: `online since 08:12 · this machine · id ${sid(ID.desk)}` }),
  laptop: hrow({ name: "host-a", line: `online since 08:31 · tailnet · id ${sid(ID.laptop)}` }),
  server: hrow({ name: "host-b", state: "off", line: `offline since 19:40 · tailnet · id ${sid(ID.server)}` }),
  lab: hrow({ name: "lab", state: "warn", err: 1, line: `Another install answers as lab: id ${sid(ID.labNew)}, it was ${sid(ID.labOld)}.`, badge: "Needs you", badgeCls: "needs-login" }),
  labOk: hrow({ name: "lab", line: `online since 20:12 · tailnet · id ${sid(ID.labNew)}` }),
};
const FOOT = "Fyr finds your other installs on the tailnet by itself. A host it cannot see goes in fyrd's config (<span class=\"mono\">hosts</span>).";
const aHosts = { nav: "hosts", navset: "need", title: "Hosts", body: group("", R.desk + R.laptop + R.server + R.lab, FOOT) };
const TRUST_NOW = row("Trust", "Through your tailnet: Fyr follows it because it is a node of your Tailscale login, not because of its key.");
const ID_NOW = "Fyr remembers this id and tells you when another install answers under this name.";
const hostPage = (o) => ({ nav: "hosts", navset: o.navset || "now", parent: "Hosts", title: o.name, mono: 1, end: o.end || pst("Online"), saved: o.saved, body: o.body });
const aHost = hostPage({
  name: "host-a",
  body:
    group("Identity", idrow(ID.laptop, ID_NOW) + TRUST_NOW) +
    group("Connection", row("Online since 08:31", "") + row("Address", '<span class="mono">http://100.64.0.2:7778</span> · tailnet') + row("Found on the tailnet", "Fyr found it by itself. It leaves this list 30 days after its last answer.")),
});
const aStates = [
  ["Offline", group("Connection", row("Offline since 19:40", '<span class="err">Unreachable.</span> Its threads show as last read and cannot be changed.') + row("Address", '<span class="mono">http://100.64.0.9:7778</span> · tailnet') + row("Found on the tailnet", "Fyr found it by itself. It leaves this list 30 days after its last answer."))],
  ["Updating Fyr; set in the config", group("Connection", row("Online since Mon 06:00", "Updating Fyr: running the tests") + row("Address", '<span class="mono">http://build-box.lan:7778</span> · LAN') + row("Set in this front's config", 'It stays on this list until it is taken out of <span class="mono">hosts</span> there.'))],
  ["A Fyr older than V0.124", group("Identity", row("Id", "None. This host runs a Fyr older than V0.124, which has no install key. It shows an id after its next update.") + TRUST_NOW)],
  ["This install", group("Identity", idrow(ID.desk, "The id of the Fyr you are using. Your other installs show this id for desk.")) + group("Connection", row("Online since 08:12", "") + row("Address", '<span class="mono">http://127.0.0.1:7777</span> · this machine'))],
];
const aChanged = hostPage({
  name: "lab",
  navset: "need",
  end: pst("Needs you", "needs-login"),
  body:
    note("<b>Another install answers as lab.</b> That is normal after you reinstalled or replaced lab. It is wrong if you did not.", "", "bad") +
    group(
      "Identity",
      `<div class="sr"><span class="sr-b"><span class="sr-t">It was</span>${idf(ID.labOld, true)}<span class="sr-d">The id Fyr first saw for lab.</span></span></div><div class="sr bad"><span class="sr-b"><span class="sr-t">It is now</span>${idf(ID.labNew, true)}<span class="sr-d">To check, open Fyr on lab: its Hosts page shows its own id.</span></span><span class="sr-e">${btn("Copy")}</span></div><div class="sr"><span class="sr-b"><span class="sr-t">Trust ${sid(ID.labNew)} as lab</span><span class="sr-d">Fyr then remembers the new id and stops warning.</span></span><span class="sr-e">${btn(`Trust ${sid(ID.labNew)}`)}</span></div>`,
      "Fyr still follows lab meanwhile: today it trusts a host through your tailnet login, not its key. If you do not know this install, take that machine off your tailnet.",
    ) +
    group("Connection", row("Online since 20:12", "") + row("Address", '<span class="mono">http://100.64.0.7:7778</span> · tailnet') + row("Set in this front's config", 'It stays on this list until it is taken out of <span class="mono">hosts</span> there.')),
});
const aTrusted = hostPage({ name: "lab", saved: 1, body: group("Identity", idrow(ID.labNew, ID_NOW) + TRUST_NOW) + group("Connection", row("Online since 20:12", "") + row("Address", '<span class="mono">http://100.64.0.7:7778</span> · tailnet')) });
const aRefused = group(
  "Identity",
  `<div class="sr bad"><span class="sr-b"><span class="sr-t">Trust ${sid(ID.labNew)} as lab</span><span class="sr-d err">Not trusted: desk answered “lab has reported no id”. Nothing changed.</span></span><span class="sr-e">${btn(`Trust ${sid(ID.labNew)}`)}</span></div>`,
);
const aOne = { nav: "hosts", navset: "one", title: "Hosts", body: group("", hrow({ name: "desk", self: 1, line: `online since 08:12 · this machine · id ${sid(ID.desk)}` }), "This is your only Fyr install. " + FOOT) };
const aOffline = { ...aHosts, banner: "No connection to desk. Settings show what was last read and cannot be changed until it is back.", body: `<div class="ro">${aHosts.body}</div>` };
const aDevice = (how) => ({
  nav: "device",
  navset: "now",
  title: "This device",
  body: group(
    "",
    row("Notifications", "When a thread asks you something or fails.", swt(false, "Notifications")) +
      `<div class="sr stack"><span class="sr-b"><span class="sr-t">Theme</span></span><span class="sr-e"><span class="seg" role="radiogroup" aria-label="Theme"><button class="on">Auto</button><button>Light</button><button>Dark</button></span></span></div>` +
      row("Show tool details", "Every tool group open, every machine line in full.", swt(false, "Show tool details")) +
      row("How this browser gets in", how),
    "Kept in this browser. Each device has its own.",
  ),
});
const HOW = {
  serve: "Through Tailscale Serve, as owner@github. Any browser signed in to your tailnet with that login can open this Fyr.",
  local: "On this machine itself (localhost). A browser on this machine is always allowed.",
  other: "At fyr.example.net, as owner@github. What stands in front of Fyr there decides who gets in.",
};
const howStates = wide("How this browser gets in · the three texts", group("", row("How this browser gets in", HOW.serve) + row("How this browser gets in", HOW.local) + row("How this browser gets in", HOW.other), "First: the page's address ends in .ts.net. Second: localhost or a loopback address. Third: anything else."), 520);

// ================= B. the later slices =================
const bHosts = {
  nav: "hosts",
  navset: "full",
  title: "Hosts",
  end: btn("Add by address…") + btn("Pair…"),
  body:
    group(
      "",
      R.desk + hrow({ name: "host-a", line: `online since 08:31 · tailnet and LAN · id ${sid(ID.laptop)}` }) + R.server + hrow({ name: "vps", line: `online since Mon 06:00 · by address · id ${sid(ID.vps)}` }),
      "The installs you trust. Each one knows this list and refuses everything else.",
    ) +
    group(foundHd, candNear + candTag, FOUND_FOOT) +
    group("Where Fyr looks", row("Tailnet", "On · owner@github · 5 nodes, 4 run Fyr") + row("Pair my own tailnet nodes without a code", "Untagged nodes of owner@github. Off: they show under Found and pair by code.", swt(true, "Pair my own tailnet nodes without a code")) + row("This LAN", "On · 192.168.1.0/24 · 2 installs answer") + row("By address", "1 address · vps.example.net:7778")) +
    `<div class="sbar">${fold("1 ignored")}${fold("1 removed")}</div>`,
};
const ROSTER_OK = group("Your installs", row("4 installs, in step", "This install's list of trusted installs matches the others. Last compared with host-a just now."));
const bSelf = hostPage({
  name: "desk",
  navset: "full",
  body:
    group("Identity", idrow(ID.desk, "The id of the Fyr you are using. Your other installs show this id for desk.") + row("Front and host", "Build 20261116-2030")) +
    ROSTER_OK +
    group("Addresses", arow("desk.tail1234.ts.net:7778", "Tailnet · where your other installs reach this one") + arow("192.168.1.10:7778", "This LAN · announced to the network"), 'A new key for this install: run <span class="mono">fyrd rotate-key</span> here. The others accept it by themselves. To remove this install, do it from another one.'),
});
const scan = (k) =>
  `<div class="bd-f"><span class="bd-cap">Phone · ${k === "no" ? "the camera is refused" : "Scan…"}</span><div class="bd-phone"><div class="phx" style="min-height:0"><div class="m-hd"><button class="m-bk" aria-label="Back">‹</button><span class="m-tt">Pair</span></div><div class="m-body"><div class="stp">${card(
    k === "no"
      ? `${seg2(1)}<label class="fld"><span>The code the other install shows</span><div class="pair-top"><input class="pcode-in" inputmode="numeric" placeholder="000 000"></div></label><div class="setbar"><span class="pair-st err" style="margin-right:auto" role="alert">The browser did not allow the camera. Type the six digits.</span>${btn("Pair", "p", 1)}</div>`
      : `<div style="aspect-ratio:1;border-radius:10px;background:#0b0f13;display:grid;place-items:center"><div style="width:62%;aspect-ratio:1;border:2px solid #f6f7f8;border-radius:12px;opacity:.85"></div></div><div class="setbar"><span class="pair-st" style="margin-right:auto">Point the camera at the picture on the other install.</span>${btn("Cancel")}</div>`,
  )}</div></div></div></div></div>`;
const shortcutOff = wide(
  "The switch off · your own nodes wait for a code",
  group(foundHd, crow({ title: `<span class="mono">work-mac</span> ${idf("W2MC-9DHE-T4KQ-6BVN")}`, line: "work-mac.tail1234.ts.net · found on the tailnet · your own node; pairing without a code is off" })) +
    group("Where Fyr looks", row("Tailnet", "On · owner@github · 5 nodes, 4 run Fyr") + row("Pair my own tailnet nodes without a code", "Untagged nodes of owner@github. Off: they show under Found and pair by code.", swt(false, "Pair my own tailnet nodes without a code"))),
  560,
);
const changedTail = hostPage({
  name: "lab",
  navset: "need",
  end: pst("Needs you", "needs-login"),
  body:
    note("<b>Another install answers as lab, and Fyr refuses it.</b> lab's threads show as last read until you decide.", "", "bad") +
    group(
      "Identity",
      twoIds +
        `<div class="sr"><span class="sr-b"><span class="sr-t">It is lab, reinstalled</span><span class="sr-d">The tailnet says it is your own untagged node, so it needs no code. The old id (AX2C) is removed on every install and the name lab goes to the new one.</span></span><span class="sr-e">${btn(`Trust ${sid(ID.labNew)}…`)}</span></div><div class="sr"><span class="sr-b"><span class="sr-t">I do not know it</span><span class="sr-d">Fyr stops dialling ${sid(ID.labNew)} and stops showing it. lab stays offline until the real one is back.</span></span><span class="sr-e">${btn(`Block ${sid(ID.labNew)}`, "danger")}</span></div>`,
    ),
});

// ================= C. states =================
const removedNote = note("<b>host-a is removed.</b> 2 of your 3 other installs have heard. host-b is offline and learns it when it is back; until then it still lets host-a in.", btn("OK"));
const removedGroup = (line) => group("Removed", hrow({ name: "host-a", state: "gone", line, badge: "Removed" }), "A removed install is refused by every install that has heard. Only a new pairing by code brings it back.");
const cRemoved = {
  nav: "hosts",
  navset: "full",
  title: "Hosts",
  end: btn("Add by address…") + btn("Pair…"),
  body: removedNote + group("", R.desk + R.server + hrow({ name: "vps", line: `online since Mon 06:00 · by address · id ${sid(ID.vps)}` })) + removedGroup(`removed today 21:30 on desk · told 2 of 3 · id ${sid(ID.laptop)}`),
};
const cRemovedHost = hostPage({
  name: "host-a",
  navset: "full",
  end: pst("Removed"),
  body:
    group("Identity", idrow(ID.laptop) + row("Removed today 21:30, on desk", "desk and vps refuse it. host-b has not heard yet: it is offline since 19:40.")) +
    group("History", hist([["16 Nov 21:30", "Removed on desk"], ["16 Nov 20:31", "Paired by itself by desk (tailnet)"]])) +
    group("", row("Its threads", "They show as last read for 30 days and cannot be changed.") + row("Bring it back", "Pair it again by code. Fyr never pairs a removed install by itself.", btn("Pair again…"))),
});
const cElsewhere = wide(
  "On another install (vps) · it heard of the removal",
  note("<b>desk removed host-a</b> today at 21:30. This install refuses it now.", btn("OK")) + removedGroup(`removed today 21:30 on desk · id ${sid(ID.laptop)}`),
  560,
);
const cSelfRemoved = {
  nav: "hosts",
  navset: [...NAVS.now.map((i) => (i.id === "hosts" ? { ...i, sub: "This install was removed", badge: '<span class="pst needs-login">1</span>' } : i))],
  title: "Hosts",
  end: btn("Pair…", "p"),
  body:
    note("<b>This install was removed.</b> desk removed it today at 21:30, so your other installs refuse it. Threads on this machine keep running. To bring it back, pair it again by code with any of them.", "", "bad") +
    group(
      "",
      hrow({ name: "host-a", self: 1, line: `online since 08:31 · this machine · id ${sid(ID.laptop)}` }) + hrow({ name: "desk", state: "off", line: `refuses this install since 21:30 · tailnet · id ${sid(ID.desk)}` }) + hrow({ name: "vps", state: "off", line: `refuses this install since 21:31 · by address · id ${sid(ID.vps)}` }),
    ),
};
const TERM_ROT = term(`<span class="p">lab $</span> <b>fyrd rotate-key</b>
This install   lab
Old id   ${ID.labNew}
New id   K9RT-5WQD-H3NA-8LXE
<span class="ok">The change is signed with the old key. 3 of 3 installs have it.</span>`);
const cRotHost = hostPage({
  name: "lab",
  navset: "full",
  body:
    group("Identity", idrow("K9RT-5WQD-H3NA-8LXE", `A new key since 2 Dec 09:10. lab made it itself and signed the change with its old key (${sid(ID.labNew)}), so nothing is asked of you.`) + row("Paired by code", "16 Nov 21:04, on desk.")) +
    group("History", hist([["2 Dec 09:10", `Key changed by lab: ${sid(ID.labNew)} to K9RT`], ["16 Nov 21:04", "Paired by code on desk"], ["16 Nov 20:58", "First seen on this LAN"]])),
});
const cRotRows = wide(
  "The row for a week after, and the terminal on lab",
  group("", hrow({ name: "lab", line: "online since 20:40 · LAN · id K9RT", tag: tag("New key", "new") })) + lab("fyrd rotate-key, on lab") + TERM_ROT,
  560,
);
const bEmpty = {
  nav: "hosts",
  navset: "one",
  title: "Hosts",
  end: btn("Add by address…") + btn("Pair…", "p"),
  body:
    group("", R.desk + `<div class="empty"><b>No other install</b>Start Fyr on another machine. On this LAN or your tailnet it shows below by itself; then pair it.</div>`) +
    group(`Found, not paired <span class="end">Looked 20 s ago${lnk("Look now")}</span>`, `<div class="empty">Nothing found.</div>`) +
    group("Where Fyr looks", row("Tailnet", "Off: tailscaled does not answer on this machine.") + row("This LAN", "On · 192.168.1.0/24 · no install answers") + row("By address", "No address", btn("Add…"))),
};
const cSources = wide(
  "Where Fyr looks · every state of a source",
  group(
    "Tailnet",
    row("Tailnet", "On · owner@github · 5 nodes, 4 run Fyr") +
      row("Tailnet", "Off: tailscaled does not answer on this machine. Fyr looks on the LAN and at the addresses you gave.") +
      row("Tailnet", "Signed out: Tailscale runs here but is not logged in.") +
      row("Tailnet", '<span class="err">Could not ask tailscaled: permission denied on its socket. Last answer 20:12.</span>', "", "bad"),
  ) +
    group(
      "This LAN",
      row("This LAN", "On · 192.168.1.0/24 · 2 installs answer") +
        row("This LAN", "On · 192.168.1.0/24 · no install answers") +
        row("This LAN", '<span class="err">Blocked: Fyr\'s own announcement does not come back, so this network drops multicast. That is common on guest and office Wi-Fi and between VLANs. Add the other install by address.</span>', btn("Add by address…"), "bad acts") +
        row("This LAN", "No network: this machine has no LAN address."),
    ) +
    group("By address", row("vps.example.net:7778", "Answers · vps (V9PS)", `<button class="xb" aria-label="Forget vps.example.net:7778">×</button>`) + row("10.8.0.4:7778", '<span class="err">No answer since Sun 22:15</span>', `<button class="xb" aria-label="Forget 10.8.0.4:7778">×</button>`) + row("Add an address", "", btn("Add…")), "× forgets an address. It does not remove a host you paired."),
  620,
);
const cAway = hostPage({
  name: "vps",
  navset: "full",
  end: pst("Offline"),
  body:
    note("<b>No address of vps answers from this network.</b> It may be up and only out of reach from here. Add an address that works from this network, or wait until this machine is back on a network that reaches it.", btn("Add by address…"), "warn") +
    group("Identity", idrow(ID.vps) + row("Paired by code", "9 Nov 14:20, on host-a. desk learned it from host-a.")) +
    group("Addresses", arow("vps.example.net:7778", "By address · no answer since 19:40") + arow("192.168.40.12:7778", "A LAN this machine is not on · last answered 12 Nov"), "Fyr dials whichever address answers with this key."),
});
const cStale = hostPage({
  name: "desk",
  navset: "full",
  body:
    group("Identity", idrow(ID.desk, "The id of the Fyr you are using.")) +
    group("Your installs", `<div class="sr bad"><span class="sr-b"><span class="sr-t">4 installs, last in step 3 days ago</span><span class="sr-d err">This install has reached none of the others since 13 Nov 18:02. A pairing or a removal made on another install since then is not known here.</span></span></div>`, "It catches up by itself as soon as it reaches any one of them."),
});
const cStaleNote = wide(
  "The Hosts page of an install that is behind, and of one that waits for it",
  note("<b>This install's list may be old.</b> It has reached none of your other installs for 3 days. A removal made elsewhere is not known here.", "", "warn") +
    group("", R.desk + hrow({ name: "host-a", state: "off", line: `offline since 13 Nov 18:02 · tailnet and LAN · id ${sid(ID.laptop)}` }) + hrow({ name: "vps", state: "off", line: `offline since 13 Nov 18:02 · by address · id ${sid(ID.vps)}` })) +
    lab("On desk, after a removal that host-b has not heard") +
    group("", hrow({ name: "host-b", state: "off", line: `offline since 19:40 · has not heard that host-a is removed · id ${sid(ID.server)}` })),
  560,
);
const cMany = {
  nav: "hosts",
  navset: "full",
  title: "Hosts",
  end: btn("Add by address…") + btn("Pair…"),
  body:
    group("", R.desk + hrow({ name: "build-box-eu-north-1-with-a-long-name", line: `online since Mon 06:00 · updating Fyr · by address · id T5BX` }) + R.server) +
    group(
      `Found, not paired · 14 <span class="end">Looked 20 s ago${lnk("Look now")}</span>`,
      candNear + crow({ title: `Fyr install ${idf("D4GT-7KQM-2XPA-9RVE")}`, line: "192.168.1.44:7778 · found on this LAN · first seen 20:58" }) + crow({ title: `Fyr install ${idf("L8NC-3WYB-6HJD-5QSF")}`, line: "192.168.1.52:7778 · found on this LAN · first seen 20:59" }) + fold("Show 11 more"),
      "A shared network shows other people's Fyr installs too. They cannot reach yours. Ignore the ones that are not yours.",
    ),
};
const cLoad = wide(
  "Found and Where Fyr looks · before the first look, and a look that failed",
  group("Found, not paired", `<div class="sr"><span class="sr-b" style="gap:8px"><span class="skel" style="width:44%"></span><span class="skel" style="width:66%;height:10px"></span></span></div>`) +
    `<div class="note-err" role="alert"><span>Could not look for installs: desk did not answer in 10 s.</span>${btn("Try again")}</div>`,
  520,
);
const revokeAsk = ask("Revoke… on another device", "Revoke Pixel 9 · Chrome?", "It is signed out at once and gets no more notifications. Pair it again with Add a device.", "Revoke", 1) + ask("Revoke… on the device you are using", "Revoke this device?", "This signs out the browser you are using now. To get back in you need another paired device, or the browser on desk itself.", "Revoke this device", 1);
const removeAsk = ask("Remove… on a host's page", "Remove host-a?", `Every install refuses <span class="mono">${ID.laptop}</span> once it has heard. Its threads here show as last read for 30 days. Only a new pairing by code brings it back.`, "Remove", 1);
const trustAsk = ask("Trust B7LA… (slice 2, a reinstall on your tailnet)", "Trust B7LA as lab?", `lab was <span class="mono">${ID.labOld}</span>. It becomes <span class="mono">${ID.labNew}</span>, and the old id is removed on every install.`, "Trust B7LA");
const blockAsk = ask("Block B7LA", "Block B7LA?", "Fyr stops dialling it and stops showing it. You find it under Ignored. lab stays as it was.", "Block", 1);

const PLACE = `<table class="bd-tbl"><tr><th>Not shown today</th><th>What stands there instead, and says the truth</th><th>Arrives with</th></tr>
<tr><td>Pair…</td><td>Nothing. The foot of the list: “Fyr finds your other installs on the tailnet by itself. A host it cannot see goes in fyrd's config (hosts).”</td><td>slice 3</td></tr>
<tr><td>Add by address…</td><td>The same foot line.</td><td>slice 3</td></tr>
<tr><td>Found, not paired</td><td>Nothing: today every install Fyr finds is followed at once, and its row says “tailnet”.</td><td>slices 3, 4</td></tr>
<tr><td>Where Fyr looks, the switch</td><td>Nothing.</td><td>slices 2 to 4</td></tr>
<tr><td>Remove…</td><td>The row “Found on the tailnet: it leaves this list 30 days after its last answer” or “Set in this front's config: it stays until it is taken out of hosts there”.</td><td>slice 6</td></tr>
<tr><td>Block, Pair again…</td><td>The foot of a changed id: “Fyr still follows lab meanwhile… If you do not know this install, take that machine off your tailnet.”</td><td>slices 2, 3</td></tr>
<tr><td>How it was paired</td><td>The row “Trust: through your tailnet…, not because of its key.”</td><td>slice 2</td></tr>
<tr><td>Addresses (several), History</td><td>One row “Address” with the URL this front dials and its route.</td><td>slices 2, 4</td></tr>
<tr><td>The device list, Add a device, Revoke</td><td>The row “How this browser gets in”.</td><td>slice 5</td></tr></table>`;

render({
  title: "Pairing and discovery in Settings · iteration 3: the final set",
  sub: "build from this one, with SPEC.md",
  note: h`<ul>
<li><b>Your choices from iteration 2 are in:</b> the typing side gives only the code; the tailnet shortcut has a switch, on by default; the code is six digits <b>and a QR</b> (B3: Scan… on the typing side, where the browser has a camera); a reinstall on your own tailnet is one press after a confirm (B8).</li>
<li><b>Part A is what to build now</b>: the Hosts page, a page per host, the changed id on that page, the address, and one row on This device. It needs V0.124's data and one new field (<span class="mono">hosts[].url</span>). Nothing in part A is a button that does nothing: what cannot be done today is not drawn, and a sentence says how it is done instead (the table at the end).</li>
<li><b>Part B is every later screen</b>, marked with the slice of decision 0026 that fills it. Each plugs into the same groups.</li>
<li><b>Part C is the states</b>: removal and a new key as the other installs see them, the empty page, a source that is off or blocked, a host on another network, a list that is behind.</li>
<li><b>Changed since iteration 2:</b> a row's route is read from the address (this machine, tailnet, LAN, by address); an offline row is not red (only a row that needs you is); the shortcut switch sits under Tailnet in “Where Fyr looks”; each address under “By address” can be forgotten; this install has its own page with the state of your list of installs.</li></ul>`,
  screens: [
    { h: `A1. Hosts ${NOW}`, why: "A fifth Settings page. Each row: state, route, short id. A row that needs you is red-edged with the amber badge, and the nav counts it. No buttons: the foot says how a host gets here today.", page: aHosts },
    { h: `A2. A host ${NOW}`, why: "The full id with Copy, what trust rests on today, the connection, the address this front dials, and how the host got on the list.", page: aHost },
    { h: `A3. A host: the other states ${NOW}`, why: "Offline with the reason; updating; set in the config; a Fyr with no key; and this install's own page.", raw: wide("Groups that differ, by state", aStates.slice(0, 2).map((x) => lab(x[0]) + x[1]).join("")) + wide("", aStates.slice(2).map((x) => lab(x[0]) + x[1]).join("")) },
    { h: `A4. A changed id ${NOW}`, why: "V0.124's warning and its Trust button move from the list row to lab's page: both ids in full, how to check, one action. The foot says that Fyr still follows the host.", page: aChanged },
    { h: `A4b. After Trust, and a refused Trust ${NOW}`, why: "Left: Saved shows beside the title, the note and the old id are gone, the nav badge clears. Right: the host's words on the row; nothing changed.", raw: frame("Desktop · trusted", desk(aTrusted), "bd-desk") + wide("Refused", aRefused, 390) },
    { h: `A5. This device ${NOW}`, why: "One new row, read from the page's own address and your login. It changes nothing; the device list grows from it in slice 5.", page: aDevice(HOW.serve), phonePage: aDevice(HOW.local) },
    { h: `A5b. The three texts of that row ${NOW}`, why: "", raw: howStates },
    { h: `A6. One install; no connection ${NOW}`, why: "Left and middle: the only install. Right: with no connection to the front the page is read-only, as every Settings page.", raw: frame("Desktop · one install", desk(aOne), "bd-desk") + frame("Phone · one install", phone({ ...aOne, short: 1 }), "bd-phone") + frame("Phone · no connection", phone({ ...aOffline, short: 1 }), "bd-phone") },
    { h: `A7. Touch targets at 360 px ${NOW}`, why: "The dashed lines are the targets. A host row is one target, 56 px or more. Copy and Trust are 44 px high and stay inside the screen.", page: aChanged, phonePage: aHosts, desktop: false, cls: "t44 w360", raw: frame("Phone 360 · Hosts", phone({ ...aHosts, short: 1 }), "bd-phone") + frame("Phone 360 · a changed id", phone(aChanged), "bd-phone") },

    { h: `B1. Hosts, all slices in`, why: "The same page with its four groups: the installs you trust, the ones Fyr found, where it looks, what you turned away.", page: bHosts },
    { h: `B2. A host, and this install ${s(2)}`, why: "Left: how it was paired, every address with the one in use, its history, Remove. Right: this install's own page says whether its list of your installs is in step with the others.", page: hostFull, phonePage: bSelf },
    { h: `B3. Pair: show a code ${s(3)}`, why: "Six digits and a QR of the same code. This install's id sits beside it. Right: Scan… on the typing side.", page: pairPage(showCode()), phonePage: pairPage(enterCode()) },
    { h: `B3b. Scan, progress, success ${s(3)}`, why: "Scan… shows only where the browser has a camera. When pairing is done both sides show the same two ids; that is the fingerprint check.", raw: scan() + scan("no") + wide("Typing side · checking, then paired", lab("Checking") + enterCode("busy") + lab("Paired") + enterCode("done"), 520) + wide("Showing side · paired; the terminal", lab("Paired") + showCode("done") + lab("fyrd pair") + TERM_OK, 560) },
    { h: `B4. Pair: what goes wrong ${s(3)}`, why: "Left: the side that shows the code. Right: the side that types it. Cancel on either side ends the code; the other side then reads “The other install cancelled.”", raw: wide("Shows the code", lab("One wrong try") + showCode("wrong") + lab("Three wrong tries") + showCode("dead") + lab("Ended") + showCode("ended")) + wide("Types the code", lab("Wrong code") + enterCode("wrong") + lab("Cancelled") + enterCode("dead") + lab("No install shows a code") + enterCode("none") + lab("The address does not answer") + enterCode("gone") + lab("The terminal") + TERM_BAD) },
    { h: `B5. Found installs ${s(4)}`, why: "Plain; a tagged tailnet node; one install at two addresses (one row); an install at lab's address (Review… opens lab's page, B8).", page: foundPage },
    { h: `B5b. Pair from a found install; Ignored; a known name with another id ${s(4)}`, why: "", raw: wide("Pair… on a found install", showCode("wait", { for: `Fyr install ${ID.other} at 192.168.1.31` }), 520) + wide("Ignored, unfolded", ignored, 520) + wide("A known name, another id", group(foundHd, candName), 520) },
    { h: `B6. The tailnet shortcut ${s(2)}`, why: "The note shows once after Fyr paired something by itself. The switch turns the shortcut off. Right: with it off, your own nodes wait under Found like any other.", page: shortcutPage, phonePage: shortcutHost, phoneCap: "Phone · the undo for one install: Remove" },
    { h: `B6b. The switch off ${s(2)}`, why: "", raw: shortcutOff },
    { h: `B7. Add by address ${s(3)}`, why: "Look up asks the address who it is. Then it pairs by code like any found install.", raw: frame("Desktop · a Fyr install answers", desk(addPage("vps.example.net:7778", ADD_FOUND)), "bd-desk") + frame("Phone · empty", phone({ ...addPage("", null), short: 1 }), "bd-phone") + wide("The other answers", ADD_STATES.map((x) => lab(x[0]) + addForm(x[2], x[1])).join(""), 520) },
    { h: `B8. A changed id, from slice 2 ${s(2)}`, why: "The new key is refused until you choose. Left: off the tailnet or a tagged node, it pairs again by code. Right: your own untagged node, one press after the confirm.", page: changedLater, phonePage: changedTail, deskCap: "Desktop · by code", phoneCap: "Phone · on your tailnet" },
    { h: `B8b. The confirms ${s(2)}`, why: "Each names the id in full. Remove and Block are red.", raw: trustAsk + blockAsk + removeAsk },
    { h: `B9. Devices ${s(5)}`, why: "“This device” becomes “Devices”. The ways in that need no pairing are listed and cannot be revoked here.", page: devicesPage({ serve: 1 }), phonePage: addDevice() },
    { h: `B9b. The new device, the gates, Revoke ${s(5)}`, why: "After the scan the phone names itself. An unpaired or revoked browser gets a page with no app and no data.", raw: ph("New phone · after the scan", gate("ask")) + ph("Any browser that is not paired", gate("not")) + ph("A revoked device, at its next request", gate("out")) + wide("Added, and a code that ended", lab("Added") + addDevice("done").body + lab("Ended") + addDevice("ended").body, 520) + revokeAsk },

    { h: `C1. Removal: on the install that removed ${s(6)}`, why: "After Remove the page says how far the news has spread. The removed install moves to its own group.", page: cRemoved, phonePage: cRemovedHost, phoneCap: "Phone · the removed host's page" },
    { h: `C2. Removal: on the other installs, and on the removed machine ${s(6)}`, why: "Left: any other install says who removed what. Right: the removed machine says so at the top of Hosts, counts it in the nav, and offers the way back.", raw: cElsewhere + frame("Desktop · on host-a itself", desk(cSelfRemoved), "bd-desk") },
    { h: `C3. A new key ${s(6)}`, why: "fyrd rotate-key signs the change with the old key, so the others accept it with no question. It is calm: a tag for a week and a line in the history. Compare A4, where nothing vouches for the new id.", page: cRotHost, desktop: false, raw: frame("Desktop · lab after its key changed", desk(cRotHost), "bd-desk") + cRotRows },
    { h: `C4. No other install ${s(3)}`, why: "The one time Pair… is the primary button. The sources say why nothing is found.", page: bEmpty },
    { h: `C5. A source that is off, blocked or failing ${s(4)}`, why: "No tailnet, a network that drops mDNS, an address that stopped answering. Each says what Fyr does instead.", raw: cSources },
    { h: `C6. A host on another network ${s(4)}`, why: "Paired, but no address answers from here. The note says it may be fine and what helps.", page: cAway },
    { h: `C7. A list that is behind ${s(6)}`, why: "An install that reached no other for a while may miss a removal. It says so on its own page and at the top of Hosts.", raw: frame("Desktop · this install's page", desk(cStale), "bd-desk") + cStaleNote },
    { h: `C8. Many found installs; a long name ${s(4)}`, why: "Found shows three and folds the rest. A long name ends in an ellipsis on the row and wraps on its page.", page: cMany },
    { h: `C9. Before the first look; a look that failed ${s(4)}`, why: "Still rows, no shimmer. A failed read has Try again and leaves the list of hosts alone.", raw: cLoad },
    { h: "D. What is not drawn today, and what says so", why: "No fake buttons in the first build: a control appears with the slice that makes it work.", raw: PLACE },
    { h: "E. Which slice fills what", why: "", raw: SLICES },
  ],
});
