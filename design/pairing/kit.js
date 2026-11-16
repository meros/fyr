// The kit of the pairing and discovery mockups: the nav, the rows and the panels every iteration draws.
// Loads after ../settings/mock.js (h, pi, frame, screen, render) and replaces its nav, desk and phone, so a page
// can say which nav it sits in. A page is written once and drawn in the desktop dialog and the phone screen.

// ---- the V0.119 controls (as design/settings/i2.js) ----
const swt = (on, label, dis) => `<button class="swt" role="switch" aria-checked="${on}" aria-label="${label}"${dis ? " disabled" : ""}></button>`;
const sel = (v, more = []) => `<select><option>${v}</option>${more.map((m) => `<option>${m}</option>`).join("")}</select>`;
const row = (t, d, e, cls = "") => `<div class="sr ${cls}"><span class="sr-b"><span class="sr-t">${t}</span>${d ? `<span class="sr-d">${d}</span>` : ""}</span>${e ? `<span class="sr-e">${e}</span>` : ""}</div>`;
const group = (hd, rows, foot, cls = "") => `<div class="sg ${cls}">${hd ? `<div class="sg-h">${hd}</div>` : ""}<div class="sg-c">${rows}</div>${foot ? `<div class="sg-f">${foot}</div>` : ""}</div>`;
const btn = (t, cls = "", dis) => `<button class="btn ${cls}"${dis ? " disabled" : ""}>${t}</button>`;
const lnk = (t) => `<button class="lnk">${t}</button>`;
const pst = (t, cls = "") => `<span class="pst ${cls}">${t}</span>`;
const tag = (t, cls = "") => `<span class="tag ${cls}">${t}</span>`;
const dot = (k = "") => `<span class="hdot ${k}" aria-hidden="true"></span>`;
const note = (t, e = "", cls = "") => `<div class="note ${cls}"${cls ? ' role="alert"' : ""}><span>${t}</span>${e}</div>`;
const fold = (t) => `<button class="more">${t}</button>`;

// ---- the installs of the story ----
const ID = {
  desk: "F7QK-2M9X-AB34-CD56",
  laptop: "K2VD-7HPA-M4QS-9TXE",
  server: "M3RS-QX7P-5LKA-2WNE",
  labOld: "AX2C-7RTM-8PLQ-6VYD",
  labNew: "B7LA-4QZE-9KDF-3HXW",
  nas: "N4SB-6CJW-R2UE-8GPL",
  other: "Q8WN-3TYH-V5DM-7ZKC",
  vps: "V9PS-4LQA-J7RN-2XWF",
};
const sid = (id) => id.split("-")[0];
/** An install id in full, the short form in ink. */
const idf = (id, big) => `<span class="idf${big ? " big" : ""}" aria-label="id ${id}">${id.split("-").map((g, i) => `<span${i ? "" : ' class="s"'}>${g}</span>`).join("")}</span>`;

// ---- the nav ----
const NAVS = {
  // today: Hosts is its own page (this design); This device as V0.119
  now: [
    { id: "providers", title: "Providers", sub: "3 providers" },
    { id: "automation", title: "Automation", sub: "Deploys on · merges off" },
    { id: "projects", title: "Projects", sub: "10 projects" },
    { id: "hosts", title: "Hosts", sub: "3 hosts · 1 offline" },
    { id: "device", title: "This device", sub: "Notifications off · theme auto" },
  ],
};
NAVS.need = NAVS.now.map((i) => (i.id === "hosts" ? { ...i, sub: "3 hosts · 1 needs you", warn: 0, badge: '<span class="pst needs-login">1</span>' } : i));
NAVS.full = NAVS.now.map((i) => (i.id === "hosts" ? { ...i, sub: "4 hosts · 2 found" } : i.id === "device" ? { ...i, title: "Devices", sub: "This device · 2 others" } : i));
NAVS.one = NAVS.now.map((i) => (i.id === "hosts" ? { ...i, sub: "This install only" } : i));

function nav(active, set = "now") {
  return `<nav class="stn" aria-label="Settings"><div class="stn-c">${(Array.isArray(set) ? set : NAVS[set])
    .map((i) => `<button class="stn-i${i.id === active ? " on" : ""}"${i.id === active ? ' aria-current="page"' : ""}><span class="stn-t">${i.title}</span><span class="stn-s">${i.sub}</span><span class="stn-a">${i.badge || ""}<span class="chev">›</span></span></button>`)
    .join("")}</div></nav>`;
}
function head(p) {
  const crumb = p.parent ? `<button class="crumb">‹ ${p.parent}</button><span class="sep">/</span>` : "";
  return `<div class="stp-h nav">${crumb}<h2 class="ell${p.mono ? " mono" : ""}">${p.title}</h2>${p.end || p.saved ? `<span class="end">${p.saved ? '<span class="saved" aria-live="polite">Saved</span>' : ""}${p.end || ""}</span>` : ""}</div>`;
}
function desk(p) {
  if (p.gate) return `<div class="dlgx" style="min-height:420px">${p.gate}</div>`;
  return `<div class="dlgx" role="dialog" aria-label="Settings"><div class="dh">Settings<button class="x" aria-label="Close">×</button></div><div class="stx">${nav(p.nav, p.navset)}<div class="stp">${p.banner ? `<div class="m-ban d">${p.banner}</div>` : ""}${head(p)}${p.lede ? `<p class="stp-lede">${p.lede}</p>` : ""}${p.body}</div></div></div>`;
}
function phone(p) {
  if (p.gate) return `<div class="phx">${p.gate}</div>`;
  const body = p.index ? nav(null, p.navset) : `<div class="stp">${p.end ? `<div class="sbar">${p.end}</div>` : ""}${p.lede ? `<p class="stp-lede" style="margin:0">${p.lede}</p>` : ""}${p.body}</div>`;
  return `<div class="phx"${p.short ? ' style="min-height:0"' : ""}><div class="m-hd"><button class="m-bk" aria-label="Back">‹</button><span class="m-tt ell${p.mono && !p.index ? " mono" : ""}">${p.index ? "Settings" : p.title}</span>${p.saved ? '<span class="saved" style="padding-right:10px" aria-live="polite">Saved</span>' : ""}</div>${p.banner ? `<div class="m-ban">${p.banner}</div>` : ""}<div class="m-body">${body}</div></div>`;
}
/** A slice mark for a section heading. */
const sl = (n) => (n === 1 ? '<span class="slice now">Build now</span>' : `<span class="slice">Slice ${n}</span>`);

// ---- rows ----
/** One host in the list. o: {name, self, state: "" | "off" | "warn" | "gone", line, err, badge, badgeCls, tag} */
const hrow = (o) =>
  `<button class="sr host${o.err ? " bad" : ""}${o.state === "gone" ? " gone" : ""}">${dot(o.state)}<span class="sr-b"><span class="sr-t mono">${o.name}${o.self ? tag("This install") : ""}${o.tag || ""}</span><span class="sr-d${o.err ? " err" : ""}">${o.line}</span></span><span class="sr-e">${o.badge ? pst(o.badge, o.badgeCls) : ""}<span class="chev">›</span></span></button>`;
/** One found install that is not paired. o: {title, line, warn, acts} */
const crow = (o) =>
  `<div class="sr acts${o.warn ? " bad" : ""}">${dot("new")}<span class="sr-b"><span class="sr-t">${o.title}</span><span class="sr-d">${o.line}</span>${o.warn ? `<span class="sr-d err">${o.warn}</span>` : ""}</span><span class="sr-e">${o.acts || btn("Ignore") + btn("Pair…")}</span></div>`;
/** One address of a host. */
const arow = (a, d, cls = "") => `<div class="sr ${cls}"><span class="sr-b"><span class="sr-t mono wrap">${a}</span><span class="sr-d">${d}</span></span></div>`;
/** A history list: [[when, what]]. */
const hist = (list) => `<div class="hist">${list.map((x) => `<time>${x[0]}</time><span>${x[1]}</span>`).join("")}</div>`;
/** The id row of a detail page. */
const idrow = (id, d) => `<div class="sr"><span class="sr-b"><span class="sr-t">Id</span>${idf(id, true)}${d ? `<span class="sr-d">${d}</span>` : ""}</span><span class="sr-e">${btn("Copy")}</span></div>`;

/** A QR code drawn from a seed (a picture only: it encodes nothing). */
function qr(seed = 7, off) {
  const n = 25;
  let s = (seed * 2654435761) >>> 0;
  const rnd = () => ((s = (Math.imul(s, 1664525) + 1013904223) >>> 0), s / 4294967296);
  const eye = (x, y) => (x < 7 && y < 7) || (x >= n - 7 && y < 7) || (x < 7 && y >= n - 7);
  let d = "";
  for (let y = 0; y < n; y++) for (let x = 0; x < n; x++) if (!eye(x, y) && !(x === 7 || y === 7 || x === n - 8) && rnd() > 0.52) d += `M${x} ${y}h1v1h-1z`;
  const e = (x, y) => `M${x} ${y}h7v7h-7zM${x + 1} ${y + 1}v5h5v-5zM${x + 2} ${y + 2}h3v3h-3z`;
  return `<span class="qr${off ? " off" : ""}" role="img" aria-label="QR code"><svg viewBox="0 0 ${n} ${n}" shape-rendering="crispEdges"><path fill="#111820" fill-rule="evenodd" d="${e(0, 0)}${e(n - 7, 0)}${e(0, n - 7)}${d}"/></svg></span>`;
}
