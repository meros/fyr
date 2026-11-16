// Iteration 2: the flows, on the structure the owner chose after iteration 1 (Hosts is its own page; a row opens the
// host's page; Devices joins This device in slice 5; the front sends each host's address now). i3.js reuses these
// pages (window.FINAL stops the render here).

// ---- the pairing panel ----
const me = (name, id) => `<span class="pair-who"><small>This install</small><span class="mono"><b>${name}</b></span>${idf(id)}</span>`;
const seg2 = (on) => `<span class="seg" role="radiogroup" aria-label="Which side"><button${on === 0 ? ' class="on"' : ""}>Show a code</button><button${on === 1 ? ' class="on"' : ""}>Enter a code</button></span>`;
const card = (inner, foot) => `<div class="sg"><div class="sg-c"><div class="pair">${inner}</div></div>${foot ? `<div class="sg-f">${foot}</div>` : ""}</div>`;
const STEPS = (code) =>
  `<ol class="steps"><li>On the other machine, open Fyr: Settings, Hosts, Pair, <b>Enter a code</b>.${window.FINAL ? " Type the digits, or scan the picture with its camera." : ""}</li><li>If it has no screen, run this there:<div class="cmd"><code>fyrd pair ${code}</code>${lnk("Copy")}</div></li></ol>`;

/** Side A, "Show a code". k: wait | wrong | dead | ended | done; o.for: a found install the code is bound to */
const showCode = (k = "wait", o = {}) => {
  const dead = k === "dead" || k === "ended";
  const st =
    k === "wait"
      ? `<span class="pair-st" style="margin-right:auto">Waiting for the other install · the code ends at 21:09</span>${btn("Cancel")}`
      : k === "wrong"
        ? `<span class="pair-st err" style="margin-right:auto" role="alert">A wrong code was tried from 192.168.1.31 (1 of 3). This code still works until 21:09.</span>${btn("Cancel")}`
        : k === "dead"
          ? `<span class="pair-st err" style="margin-right:auto" role="alert">Three wrong codes were tried from 192.168.1.31, so this code is cancelled. Nothing was paired.</span>${btn("Show a new code")}`
          : `<span class="pair-st" style="margin-right:auto">The code ended at 21:09 unused.</span>${btn("Show a new code")}`;
  if (k === "done")
    return card(
      `<div class="pair-st ok"><b>Paired with meros-server</b></div><div class="pair-top"><span class="pair-who"><small>Its id</small>${idf(ID.server, true)}</span>${me("desk", ID.desk)}</div><div class="setd">meros-server shows the same two ids. Your other installs learn of it within a minute.</div><div class="setbar">${btn("Open meros-server")}${btn("Done", "p")}</div>`,
    );
  return card(
    `${o.for ? `<div class="setd">A code for <b>${o.for}</b>. No other install can use it.</div>` : seg2(0)}<div class="pair-top">${window.FINAL ? qr(5, dead) : ""}<span class="pcode${dead ? " off" : ""}" aria-label="The code">482 913</span>${me("desk", ID.desk)}</div>${dead ? "" : STEPS("482913")}<div class="setbar">${st}</div>`,
    dead ? "" : "The code works once, for 5 minutes. Three wrong tries cancel it.",
  );
};
/** Side B, "Enter a code". k: empty | busy | wrong | dead | none | gone | done */
const enterCode = (k = "empty", o = {}) => {
  const v = k === "empty" ? "" : k === "wrong" ? "482 931" : "482 913";
  const msg = {
    empty: "",
    busy: `<span class="pair-st">Looking for the install that shows this code · tried 1 of 2</span>`,
    wrong: `<span class="pair-st err" role="alert">desk did not accept that code. 2 tries left.</span>`,
    dead: `<span class="pair-st err" role="alert">That code is cancelled after three wrong tries. Show a new code on the other install.</span>`,
    none: `<span class="pair-st err" role="alert">No install that Fyr can see shows a code. A code lasts 5 minutes. If the other install is on another network, give its address.</span>`,
    gone: `<span class="pair-st err" role="alert">192.168.1.10:7778 did not answer in 3 s.</span>`,
  }[k];
  if (k === "done")
    return card(
      `<div class="pair-st ok"><b>Paired with desk</b></div><div class="pair-top"><span class="pair-who"><small>Its id</small>${idf(ID.desk, true)}</span>${me("meros-server", ID.server)}</div><div class="setd">desk shows the same two ids. It tells your other installs; they show here within a minute.</div><div class="setbar">${btn("Open desk")}${btn("Done", "p")}</div>`,
    );
  return card(
    `${o.for ? `<div class="setd">Pairing with <b>${o.for}</b>.</div>` : seg2(1)}<label class="fld"><span>The code the other install shows</span><div class="pair-top"><input class="pcode-in" inputmode="numeric" autocomplete="one-time-code" placeholder="000 000" value="${v}"${k === "busy" ? " disabled" : ""}${k === "wrong" ? ' aria-invalid="true"' : ""}>${window.FINAL && k !== "busy" ? btn("Scan…") : ""}${me("meros-server", ID.server)}</div></label>${k === "none" || k === "gone" || o.addr ? `<label class="fld"><span>Its address (only when Fyr cannot find it)</span><input placeholder="192.168.1.10:7778" value="${k === "gone" ? "192.168.1.10:7778" : ""}" class="mono"></label>` : ""}<div class="setbar"><span style="margin-right:auto">${msg}</span>${k === "busy" ? btn("Cancel") : btn("Pair", "p", k === "empty")}</div>`,
    k === "empty" ? "Fyr asks the installs it has found for the one that shows this code. A code tried on a wrong install tells it nothing." : "",
  );
};
const pairPage = (body, t = "Pair", set = "full") => ({ nav: "hosts", navset: set, parent: "Hosts", title: t, body });
const term = (lines) => `<pre class="term">${lines}</pre>`;
const TERM_OK = term(`<span class="p">meros-server $</span> <b>fyrd pair 482913</b>
This install   meros-server   ${ID.server}
Looking for the install that shows this code…
  192.168.1.31:7778   ${ID.other}   shows no code
  192.168.1.10:7778   ${ID.desk}   the code matches
<span class="ok">Paired with desk   ${ID.desk}</span>
desk shows the same two ids.`);
const TERM_BAD = term(`<span class="p">meros-server $</span> <b>fyrd pair 482931</b>
This install   meros-server   ${ID.server}
Looking for the install that shows this code…
  192.168.1.10:7778   ${ID.desk}   <span class="er">did not accept the code (2 tries left)</span>
<span class="er">Not paired.</span> Read the code again on the other install.

<span class="p">meros-server $</span> <b>fyrd pair 482913</b>
<span class="er">No install that shows a code was found on this LAN or the tailnet.</span>
Give its address:  fyrd pair 482913 192.168.1.10:7778

<span class="p">meros-server $</span> <b>fyrd pair</b>
This install   meros-server   ${ID.server}
Code   <b>731 204</b>   until 21:14. Enter it on the other install.
Waiting…  (Ctrl-C cancels the code)`);
const wide = (cap, inner, w = 620) => `<div class="bd-f"><span class="bd-cap">${cap}</span><div class="dlgx one" style="width:${w}px"><div class="stp">${inner}</div></div></div>`;
const lab = (t) => `<div class="sg-h" style="margin-bottom:-10px">${t}</div>`;

// ---- found installs ----
const candNear = crow({ title: `Fyr install ${idf(ID.other)}`, line: "192.168.1.31:7778 · found on this LAN · first seen 20:58" });
const candTag = crow({ title: `<span class="mono">nas</span> ${idf(ID.nas)}`, line: "nas.tail1234.ts.net · found on the tailnet · a tagged node, so it pairs by code" });
const candDup = crow({ title: `Fyr install ${idf(ID.vps)}`, line: "192.168.1.40:7778 on this LAN, and vps.example.net:7778 by address · one install at two addresses" });
const candSpoof = crow({ title: `Fyr install ${idf(ID.labNew)}`, line: "192.168.1.20:7778 · found on this LAN · first seen 20:12", warn: "This address was lab's (AX2C) until 20:12. Pair it only if you reinstalled lab.", acts: btn("Ignore") + btn("Review…") });
const candName = crow({ title: `<span class="mono">lab</span> ${idf(ID.labNew)}`, line: "lab.tail1234.ts.net · found on the tailnet · first seen 20:12", warn: "You already have a host named lab with another id (AX2C).", acts: btn("Ignore") + btn("Review…") });
const foundHd = `Found, not paired <span class="end">Looked 20 s ago${lnk("Look now")}</span>`;
const FOUND_FOOT = "Any Fyr on this LAN or your tailnet shows here. Being found gives it nothing: only pairing does. An install you do not know is probably someone else's: ignore it.";
const hostsRows =
  hrow({ name: "desk", self: 1, line: `online since 08:12 · id ${sid(ID.desk)}` }) +
  hrow({ name: "meros-laptop", line: `online since 08:31 · tailnet and LAN · id ${sid(ID.laptop)}` }) +
  hrow({ name: "lab", state: "off", line: `offline since 20:12 · LAN · id ${sid(ID.labOld)}` });
const foundPage = {
  nav: "hosts",
  navset: "full",
  title: "Hosts",
  end: btn("Add by address…") + btn("Pair…"),
  body: group("", hostsRows) + group(foundHd, candNear + candTag + candDup + candSpoof, FOUND_FOOT) + `<div class="sbar">${fold("2 ignored")}</div>`,
};
const ignored = group(
  "Ignored",
  `<div class="sr acts">${dot("gone")}<span class="sr-b"><span class="sr-t">Fyr install ${idf("Z3KE-8MVA-Q2HD-5RNB")}</span><span class="sr-d">Ignored 14 Nov · last seen today 20:58 at 192.168.1.50</span></span><span class="sr-e">${btn("Stop ignoring")}</span></div><div class="sr acts">${dot("gone")}<span class="sr-b"><span class="sr-t">Fyr install ${idf("W6TC-2PLX-N9GA-4JYF")}</span><span class="sr-d">Blocked 9 Nov: it answered as lab · last seen 9 Nov</span></span><span class="sr-e">${btn("Stop ignoring")}</span></div>`,
  "Ignored on this install only. An ignored install is never dialled and never shown as found.",
);

// ---- the tailnet shortcut ----
const shortcutPage = {
  nav: "hosts",
  navset: "full",
  title: "Hosts",
  end: btn("Add by address…") + btn("Pair…"),
  body:
    note("<b>Fyr paired meros-laptop and lab by itself.</b> They are untagged nodes of your Tailscale user, alexander@github, so they need no code. A tagged node pairs by code.", btn("OK")) +
    group(
      "",
      hrow({ name: "desk", self: 1, line: `online since 08:12 · id ${sid(ID.desk)}` }) +
        hrow({ name: "meros-laptop", line: `online since 08:31 · tailnet · id ${sid(ID.laptop)}`, tag: tag("Paired by itself", "new") }) +
        hrow({ name: "lab", line: `online since 20:40 · tailnet · id ${sid(ID.labNew)}`, tag: tag("Paired by itself", "new") }) +
        hrow({ name: "old-nuc", line: "online since 07:02 · tailnet · not paired: its Fyr is older than pairing" }),
    ) +
    group(foundHd, crow({ title: `<span class="mono">meros-server</span> ${idf(ID.server)}`, line: "meros-server.tail1234.ts.net · found on the tailnet · a tagged node, so it pairs by code" })) +
    group("Where Fyr looks", row("Tailnet", "On · alexander@github · 5 nodes, 4 run Fyr") + row("Pair my own tailnet nodes without a code", "Untagged nodes of alexander@github. Off: they show under Found and pair by code.", swt(true, "Pair my own tailnet nodes without a code"))),
};
const shortcutHost = {
  nav: "hosts",
  navset: "full",
  parent: "Hosts",
  title: "meros-laptop",
  mono: 1,
  end: pst("Online"),
  body:
    group("Identity", idrow(ID.laptop) + row("Paired by itself", "16 Nov 20:31, by desk. It is an untagged node of your Tailscale user, alexander@github.")) +
    group("Addresses", arow("meros-laptop.tail1234.ts.net:7778", "Tailnet · in use · answered just now")) +
    group("", row("Remove meros-laptop", "The undo of the pairing. Every install refuses it once it hears, and Fyr does not pair it by itself again: only a code brings it back.", btn("Remove…", "danger")), "", "danger-z"),
};

// ---- add by address ----
const addForm = (v, res) =>
  group("", `<div class="sr col${res && res.bad ? " bad" : ""}"><label class="fld"><span>Address</span><div class="setkey"><input class="mono" placeholder="vps.example.net:7778" value="${v || ""}"><button class="btn${res ? "" : " p"}"${v ? "" : " disabled"}>Look up</button></div></label>${res ? res.html : ""}</div>`, res ? "" : "A name or an IP with the port of the other install's host listener. For a host on another network, behind a VPN or on a VPS, where Fyr cannot look.");
const addPage = (v, res) => ({ nav: "hosts", navset: "full", parent: "Hosts", title: "Add by address", body: addForm(v, res) });
const ADD_FOUND = { html: `<div class="pair-top"><span class="pair-who"><small>A Fyr install answers there</small>${idf(ID.vps, true)}</span></div><div class="setbar"><span class="setd" style="margin-right:auto">It is not paired. The address is kept either way; it shows under Found.</span>${btn("Pair…", "p")}</div>` };
const ADD_STATES = [
  ["Already paired", { html: `<div class="setbar"><span class="setd" style="margin-right:auto;color:var(--ink)">That is <span class="mono">meros-laptop</span> (K2VD), which you already trust. Fyr now also dials it there.</span>${btn("Open meros-laptop")}</div>` }, "10.8.0.4:7778"],
  ["No answer", { bad: 1, html: `<span class="sr-d err" role="alert">Nothing answered at vps.example.net:7778 in 3 s. Check the port, and that fyrd listens there for other installs (<span class="mono">hostListen</span>).</span>` }, "vps.example.net:7778"],
  ["Not Fyr", { bad: 1, html: `<span class="sr-d err" role="alert">Something answers at 192.168.1.1:80, but it is not Fyr.</span>` }, "192.168.1.1:80"],
  ["Removed earlier", { html: `<div class="setbar"><span class="setd" style="margin-right:auto;color:var(--ink)">That is <span class="mono">old-laptop</span> (H2OL), which you removed on 12 Nov. A new pairing by code brings it back.</span>${btn("Pair…")}</div>` }, "192.168.1.77:7778"],
];

// ---- a changed id ----
const twoIds = `<div class="sr"><span class="sr-b"><span class="sr-t">It was</span>${idf(ID.labOld, true)}<span class="sr-d">First seen 3 Nov · last seen today 20:12</span></span></div><div class="sr bad"><span class="sr-b"><span class="sr-t">It is now</span>${idf(ID.labNew, true)}<span class="sr-d">Since today 20:12 · to check, open Fyr on lab: its Hosts page shows its own id</span></span><span class="sr-e">${btn("Copy")}</span></div>`;
const changedNow = {
  nav: "hosts",
  navset: "need",
  parent: "Hosts",
  title: "lab",
  mono: 1,
  end: pst("Needs you", "needs-login"),
  body:
    note("<b>Another install answers as lab.</b> That is normal after you reinstalled or replaced lab. It is wrong if you did not.", "", "bad") +
    group("Identity", twoIds + `<div class="sr"><span class="sr-b"><span class="sr-t">Trust ${sid(ID.labNew)} as lab</span><span class="sr-d">Fyr then remembers the new id and stops warning.</span></span><span class="sr-e">${btn(`Trust ${sid(ID.labNew)}`)}</span></div>`, "Fyr still follows lab meanwhile: today it trusts a host through your tailnet login, not its key. If you do not know this install, take that machine off your tailnet.") +
    group("Connection", row("Online since 20:12", "") + row("Address", '<span class="mono">http://100.64.0.7:7778</span> · tailnet · set in this front\'s config')),
};
const changedLater = {
  nav: "hosts",
  navset: "need",
  parent: "Hosts",
  title: "lab",
  mono: 1,
  end: pst("Needs you", "needs-login"),
  body:
    note("<b>Another install answers as lab, and Fyr refuses it.</b> lab's threads show as last read until you decide.", "", "bad") +
    group(
      "Identity",
      twoIds +
        `<div class="sr"><span class="sr-b"><span class="sr-t">It is lab, reinstalled</span><span class="sr-d">Pair it by code. The old id (AX2C) is removed on every install and the name lab goes to the new one.</span></span><span class="sr-e">${btn("Pair again…")}</span></div><div class="sr"><span class="sr-b"><span class="sr-t">I do not know it</span><span class="sr-d">Fyr stops dialling ${sid(ID.labNew)} and stops showing it. lab stays offline until the real one is back.</span></span><span class="sr-e">${btn(`Block ${sid(ID.labNew)}`, "danger")}</span></div>`,
    ) +
    group("Addresses", arow("192.168.1.20:7778", `This LAN · answers with ${sid(ID.labNew)}, refused`) + arow("lab.tail1234.ts.net:7778", `Tailnet · answers with ${sid(ID.labNew)}, refused`)),
};

// ---- devices ----
const accessNow = {
  nav: "device",
  navset: "now",
  title: "This device",
  body: group(
    "",
    row("Notifications", "When a thread asks you something or fails.", swt(false, "Notifications")) +
      `<div class="sr stack"><span class="sr-b"><span class="sr-t">Theme</span></span><span class="sr-e"><span class="seg" role="radiogroup" aria-label="Theme"><button class="on">Auto</button><button>Light</button><button>Dark</button></span></span></div>` +
      row("Show tool details", "Every tool group open, every machine line in full.", swt(false, "Show tool details")) +
      row("How this browser gets in", "Through Tailscale Serve, as alexander@github. Any browser signed in to your tailnet with that login can open this Fyr."),
    "Kept in this browser. Each device has its own.",
  ),
};
const devRows = (o = {}) =>
  row(`Pixel 9 · Chrome${o.me === "pixel" ? tag("This device") : ""}`, o.me === "pixel" ? "Paired just now" : "Paired 3 Nov · last used today 20:41", btn("Revoke…", "danger")) + row("iPad · Safari", "Paired 28 Oct · last used 9 Nov", btn("Revoke…", "danger"));
const devicesPage = (o = {}) => ({
  nav: "device",
  navset: "full",
  title: "Devices",
  body:
    group(
      "This device",
      row("Notifications", "When a thread asks you something or fails.", swt(o.me === "pixel", "Notifications")) +
        `<div class="sr stack"><span class="sr-b"><span class="sr-t">Theme</span></span><span class="sr-e"><span class="seg" role="radiogroup" aria-label="Theme"><button class="on">Auto</button><button>Light</button><button>Dark</button></span></span></div>` +
        row("Show tool details", "Every tool group open, every machine line in full.", swt(false, "Show tool details")),
      "Kept in this browser. Each device has its own.",
    ) +
    group(
      "Devices that can open this Fyr",
      (o.me === "pixel" ? "" : row(`This browser${tag("This device")}`, "On desk itself (localhost) · always allowed, never listed as a device")) + devRows(o) + (o.serve ? row("Anyone on your tailnet as alexander@github", "Through Tailscale Serve · turned on in this install's config, not here") : ""),
      "A paired device that is not used for 90 days is signed out.",
    ) +
    `<div class="sbar">${btn("Add a device…")}</div>`,
});
/** k: wait | done | ended */
const addDevice = (k = "wait") => ({
  nav: "device",
  navset: "full",
  parent: "Devices",
  title: "Add a device",
  body: card(
    k === "done"
      ? `<div class="pair-st ok"><b>Pixel 9 · Chrome was added</b></div><div class="setd">It can open this Fyr now. Revoke it under Devices at any time.</div><div class="setbar">${btn("Done", "p")}</div>`
      : `<div class="pair-top">${qr(11, k === "ended")}<ol class="steps" style="flex:1;min-width:200px"><li>Open the camera on the phone or tablet and point it at the code.</li><li>No camera? Open <span class="mono">https://desk.home.arpa:7443/pair</span> there and type <b class="mono">7K4M-2QXD</b>.</li></ol></div><div class="setbar">${k === "ended" ? `<span class="pair-st" style="margin-right:auto">The code ended at 21:09 unused.</span>${btn("Show a new code")}` : `<span class="pair-st" style="margin-right:auto">Waiting for the device · the code ends at 21:09</span>${btn("Cancel")}`}</div>`,
    k === "wait" ? "The code works once, for 5 minutes. The device must already trust this Fyr's certificate; that is set up in the install, not here." : "",
  ),
});
const gate = (kind) =>
  ({
    ask: `<div class="gate"><img src="fyr.png" alt="" width="56" height="56" style="border-radius:14px"><h2>Pair this device with desk?</h2><p>It will be able to open your Fyr on <span class="mono">desk</span> (${sid(ID.desk)}) until you revoke it there.</p><label class="fld" style="width:100%;max-width:300px;text-align:left"><span>Name of this device</span><input value="Pixel 9 · Chrome"></label>${btn("Pair this device", "p")}</div>`,
    not: `<div class="gate"><img src="fyr.png" alt="" width="56" height="56" style="border-radius:14px"><h2>This device is not paired</h2><p>This is Fyr on <span class="mono">desk</span>. To open it here, go to a device that already can: Settings, Devices, <b>Add a device</b>. Then scan its code with this one.</p></div>`,
    old: `<div class="gate"><img src="fyr.png" alt="" width="56" height="56" style="border-radius:14px"><h2>That code no longer works</h2><p>A code lasts 5 minutes and works once. Show a new one on a paired device: Settings, Devices, <b>Add a device</b>.</p></div>`,
    out: `<div class="gate"><img src="fyr.png" alt="" width="56" height="56" style="border-radius:14px"><h2>This device was revoked</h2><p>It was signed out of Fyr on <span class="mono">desk</span> today at 21:30. Pair it again from a device that still can: Settings, Devices, <b>Add a device</b>.</p></div>`,
  })[kind];
const ph = (cap, inner) => `<div class="bd-f"><span class="bd-cap">${cap}</span><div class="bd-phone"><div class="phx" style="min-height:520px">${inner}</div></div></div>`;

if (!window.FINAL)
  render({
    title: "Pairing and discovery in Settings · iteration 2: the flows",
    sub: "on the structure you chose",
    note: h`<ul>
<li><b>Your choices from iteration 1 are in:</b> Hosts is its own page; a host row opens the host's page; Devices joins “This device” in slice 5; the front sends each host's address now.</li>
<li><b>Pairing is one page with two sides</b> (F1 to F4). One install shows a 6-digit code; the other types it, in its Settings or with <span class="mono">fyrd pair</span>. Whoever types it gives only the code: Fyr asks the installs it has found for the one that shows it.</li>
<li><b>How you check the fingerprints.</b> The code already proves both sides (a wrong install cannot finish). The ids are there so you see <i>which</i> install you paired: each side shows its own id while it waits, and both ids when it is done. The two screens must show the same pair.</li>
<li><b>Found installs</b> (F5) are only addresses. Two addresses with one id are one row. An install at a known host's address or under a known host's name is not offered Pair: it says what is odd and sends you to that host's page.</li>
<li><b>The tailnet shortcut</b> (F6) tells you once what it paired, marks those rows, and has one switch. The undo is Remove on the host's page; a removed install is never paired by itself again.</li>
<li><b>A changed id</b> (F8) has one place: the host's page, with both ids in full. Today the only action is Trust, and the page says that Fyr still follows the host. From slice 2 the new key is refused until you pair it again or block it.</li>
<li><b>Build now</b> marks what ships first. Everything else is drawn so the later slices plug in.</li></ul>`,
    screens: [
      { h: `F1. Pair: this install shows a code ${sl(3)}`, why: "Hosts, Pair. The code, this install's own id beside it, and the two ways to enter it on the other machine. Cancel ends the code.", page: pairPage(showCode()) },
      { h: `F2. Pair: the other install enters the code ${sl(3)}`, why: "Left: in the other install's Settings (here meros-server). One field. Right: the same on a machine with no screen. fyrd pair with no code shows a code instead, for two machines without screens.", raw: frame("Desktop · meros-server's Settings", desk(pairPage(enterCode())), "bd-desk") + frame("Phone · the same page", phone(pairPage(enterCode())), "bd-phone") },
      { h: `F3. Progress and success, on both sides ${sl(3)}`, why: "While it looks, the field is locked and Cancel stops it. When it is done both sides show the same two ids; that is the check. The terminal prints the same.", raw: wide("meros-server · checking, then paired", lab("Checking") + enterCode("busy") + lab("Paired") + enterCode("done")) + wide("desk · paired", lab("Paired") + showCode("done") + lab("fyrd pair on a machine with no screen") + TERM_OK) },
      { h: `F4. A wrong code, three wrong codes, a code that ended, no install found ${sl(3)}`, why: "Left: the side that shows the code. It says where a wrong try came from. Right: the side that types it. Each message says what to do next.", raw: wide("desk · shows the code", lab("One wrong try") + showCode("wrong") + lab("Three wrong tries") + showCode("dead") + lab("Ended") + showCode("ended")) + wide("meros-server · enters the code", lab("Wrong code") + enterCode("wrong") + lab("Cancelled") + enterCode("dead") + lab("No install shows a code") + enterCode("none") + lab("The terminal") + TERM_BAD) },
      { h: `F5. Found installs: plain, tagged, one install at two addresses, and one at lab's address ${sl(4)}`, why: "Pair… on a found install opens the pairing page with the code bound to that id. The last row is how a spoofed answer or a reinstall looks: it cannot do anything, and it is not offered Pair.", page: foundPage },
      { h: `F5b. Pair from a found install; the ignored list; a known name with a new id ${sl(4)}`, why: "Left: the code is for that install only. Middle: Ignore is per install and can be undone. Right: the tailnet says “lab”, but the id is new.", raw: wide("Pair… on a found install", showCode("wait", { for: `Fyr install ${ID.other} at 192.168.1.31` }), 520) + wide("2 ignored, unfolded", ignored, 520) + wide("A known name, another id", group(foundHd, candName), 520) },
      { h: `F6. The tailnet shortcut ${sl(2)}`, why: "After the update that brings the roster, your untagged nodes are paired with no press. The note shows once. old-nuc runs an older Fyr: it is still followed through the tailnet login, and the row says so. meros-server is tagged, so it waits under Found for a code.", page: shortcutPage },
      { h: `F6b. Undoing it ${sl(2)}`, why: "The host's page says how it was paired. Remove is the undo, for one install; the switch on the Hosts page stops it for all.", page: shortcutHost },
      { h: `F7. Add by address ${sl(3)}`, why: "For a host no source can see. Look up asks the address who it is before anything is added; then it pairs by code like any found install.", page: addPage("vps.example.net:7778", ADD_FOUND), phonePage: addPage("", null) },
      { h: `F7b. Add by address: the other answers ${sl(3)}`, why: "", raw: wide("Look up · four other results", ADD_STATES.map((s) => lab(s[0]) + addForm(s[2], s[1])).join("")) },
      { h: `F8. A changed id, today ${sl(1)}`, why: "The V0.124 warning moves from the row to lab's page. Both ids in full, how to check, one action. The foot is honest: nothing is refused by key yet.", page: changedNow },
      { h: `F8b. A changed id, from slice 2 ${sl(2)}`, why: "Now the key decides: the new install is refused until you choose. Pair again… replaces the old id everywhere. Block puts the new id in the ignored list.", page: changedLater },
      { h: `F9. This device, today ${sl(1)}`, why: "One new row: how this browser gets in. Read from the page's own address and your login; it changes nothing. It is where the device list will grow from.", page: accessNow },
      { h: `F9b. Devices, and Add a device ${sl(5)}`, why: "Left: the list, as in iteration 1, with the always-allowed ways in listed but not revocable. Right: the QR holds this Fyr's address and a one-time token.", page: devicesPage({ serve: 1 }), phonePage: addDevice() },
      { h: `F9c. What the new device sees, and the other ends ${sl(5)}`, why: "The phone opens the link and names itself. Then: added (on the device that showed the code), a code that ended, and the page any unpaired browser gets: no app, no data.", raw: ph("New phone · after the scan", gate("ask")) + ph("New phone · the code ended", gate("old")) + ph("Any browser that is not paired", gate("not")) + wide("The first device · added, and ended", lab("Added") + addDevice("done").body + lab("Ended") + addDevice("ended").body, 520) },
    ],
  });
