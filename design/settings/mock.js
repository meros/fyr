// Board helpers for the settings mockups. A page is written once and drawn in both frames, as the app's shared
// Settings body is: desktop = the dialog with the nav column, phone = a screen with Back.

const h = (s, ...v) => String.raw({ raw: s }, ...v);

/** A project icon: {img} | {emoji} | {mono}, in a palette colour. */
function pi(o, size = 28) {
  const st = `--pc:var(--p-${o.color || "slate"});width:${size}px;height:${size}px;font-size:${Math.round(size * (o.emoji ? 0.7 : 0.46))}px`;
  if (o.img) return `<span class="pi img" style="${st}"><img src="${o.img}" alt=""></span>`;
  return `<span class="pi${o.emoji ? " emoji" : ""}" style="${st}">${o.emoji || o.mono}</span>`;
}

/** The settings nav: the dialog's left column on desktop, the Settings screen itself on the phone. */
function nav(active) {
  return `<nav class="stn" aria-label="Settings">${NAV.map(
    (g) =>
      `${g.group ? `<div class="stn-g">${g.group}</div>` : ""}<div class="stn-c">${g.items
        .map(
          (i) =>
            `<button class="stn-i${i.id === active ? " on" : ""}"${i.id === active ? ' aria-current="page"' : ""}><span class="stn-t">${i.title}</span><span class="stn-s${i.warn ? " hot" : ""}">${i.sub}</span><span class="stn-a">${i.badge || ""}<span class="chev">›</span></span></button>`,
        )
        .join("")}</div>`,
  ).join("")}</nav>`;
}

function head(p) {
  const crumb = p.parent ? `<button class="crumb">‹ ${p.parent}</button><span class="sep">/</span>` : "";
  return `<div class="stp-h nav">${crumb}<h2 class="ell">${p.title}</h2>${p.end || p.saved ? `<span class="end">${p.saved ? '<span class="saved" aria-live="polite">Saved</span>' : ""}${p.end || ""}</span>` : ""}</div>`;
}

function desk(p) {
  return `<div class="dlgx" role="dialog" aria-label="Settings"><div class="dh">Settings<button class="x" aria-label="Close">×</button></div><div class="stx">${nav(p.nav)}<div class="stp">${p.banner ? `<div class="m-ban d">${p.banner}</div>` : ""}${head(p)}${p.lede ? `<p class="stp-lede">${p.lede}</p>` : ""}${p.body}</div></div></div>`;
}

function phone(p) {
  const body = p.index ? nav(null) : `<div class="stp">${p.end ? `<div class="sbar">${p.end}</div>` : ""}${p.lede ? `<p class="stp-lede" style="margin:0">${p.lede}</p>` : ""}${p.body}</div>`;
  return `<div class="phx"><div class="m-hd"><button class="m-bk" aria-label="Back">‹</button><span class="m-tt ell">${p.index ? "Settings" : p.title}</span>${p.saved ? '<span class="saved" style="padding-right:10px" aria-live="polite">Saved</span>' : ""}</div>${p.banner ? `<div class="m-ban">${p.banner}</div>` : ""}<div class="m-body">${body}</div></div>`;
}

function frame(cap, html, cls, callout) {
  return `<div class="bd-f"><span class="bd-cap">${cap}</span><div class="${cls}">${html}</div>${callout ? `<div class="bd-callout">${callout}</div>` : ""}</div>`;
}

/** s: {h, why, page, phonePage?, desktop?: false, raw?: html, deskCap?, phoneCap?, callout?, cls?} */
function screen(s) {
  const frames = s.raw
    ? s.raw
    : (s.desktop === false ? "" : frame(s.deskCap || "Desktop · dialog", desk(s.page), "bd-desk")) + frame(s.phoneCap || "Phone · screen", phone(s.phonePage || s.page), "bd-phone", s.callout);
  return `<section class="bd-s${s.cls ? " " + s.cls : ""}"><h2>${s.h}</h2><p>${s.why}</p><div class="bd-row">${frames}</div></section>`;
}

function render(b) {
  document.title = b.title;
  document.body.innerHTML = `<main class="bd"><div class="bd-top"><h1>${b.title}</h1><span class="setd">${b.sub}</span><span class="seg bd-th" role="radiogroup" aria-label="Theme"><button class="on" data-t="light">Light</button><button data-t="dark">Dark</button></span></div><div class="bd-note">${b.note}</div>${b.screens.map(screen).join("")}</main>`;
  for (const x of document.querySelectorAll(".bd-th button"))
    x.onclick = () => {
      document.documentElement.dataset.theme = x.dataset.t;
      for (const y of document.querySelectorAll(".bd-th button")) y.classList.toggle("on", y === x);
    };
  if (new URLSearchParams(location.search).get("theme") === "dark") document.querySelector('.bd-th [data-t="dark"]').click();
}
