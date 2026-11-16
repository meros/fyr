// Iteration 2: controls and copy, on the structure the owner chose after iteration 1: four pages (Providers with
// System agents, Automation, Projects with Hosts, This device), every change saves at once, and the phone's More
// sheet loses Notifications and Theme. i3.js reuses these pages (window.I3 stops the render here).

const NAV = [
  {
    items: [
      { id: "providers", title: "Providers", sub: "3 · 1 needs a login", badge: '<span class="pst needs-login">1</span>' },
      { id: "automation", title: "Automation", sub: "Deploys on · merges off" },
      { id: "projects", title: "Projects", sub: "10 projects · 1 host" },
      { id: "device", title: "This device", sub: "Notifications off · theme auto" },
    ],
  },
];

const swt = (on, label, dis) => `<button class="swt" role="switch" aria-checked="${on}" aria-label="${label}"${dis ? " disabled" : ""}></button>`;
const sel = (v, more = []) => `<select><option>${v}</option>${more.map((m) => `<option>${m}</option>`).join("")}</select>`;
const row = (t, d, e, cls = "") => `<div class="sr ${cls}"><span class="sr-b"><span class="sr-t">${t}</span>${d ? `<span class="sr-d">${d}</span>` : ""}</span>${e ? `<span class="sr-e">${e}</span>` : ""}</div>`;
const group = (hd, rows, foot, cls = "") => `<div class="sg ${cls}">${hd ? `<div class="sg-h">${hd}</div>` : ""}<div class="sg-c">${rows}</div>${foot ? `<div class="sg-f">${foot}</div>` : ""}</div>`;
const prow = (o) =>
  `<button class="sr"><span class="pdot" style="background:${o.dot}"></span><span class="sr-b"><span class="sr-t ell">${o.name}</span><span class="sr-d${o.err ? " err" : ""}">${o.line}</span>${o.quota ? `<span class="sr-d">${o.quota}</span>` : ""}</span><span class="sr-e"><span class="pst ${o.cls || ""}">${o.status}</span><span class="chev">›</span></span></button>`;

const PROVS = [
  { dot: "#d97757", name: "Claude", line: "Claude account · ~/.claude · 3 models", quota: "5 h 34 % · 7 d 61 %", status: "Ready" },
  { dot: "#2f6fb0", name: "Claude (work account)", line: "Claude account · token login · 1 model", quota: '<span class="hot">5 h 86 %</span> · 7 d 40 %', status: "Needs a login", cls: "needs-login" },
  { dot: "#7d8894", name: "DeepSeek", line: "Endpoint · api.deepseek.com · 2 models", status: "Ready" },
];
const found = group(
  "Found on this host",
  `<div class="sr"><span class="sr-b"><span class="sr-t">OpenRouter</span><span class="sr-d">Endpoint · key from OPENROUTER_API_KEY</span></span><span class="sr-e"><button class="btn">Import</button></span></div>
<div class="sr"><span class="sr-b"><span class="sr-t">Claude (old)</span><span class="sr-d">Claude account · ~/.claude-old</span></span><span class="sr-e"><button class="btn">Import</button></span></div>`,
  "Logins and keys Fyr found here. Import makes one a provider; nothing is copied off this host.",
);
const sysAgents = (o = {}) =>
  `<h3 class="stp-sub">System agents</h3>` +
  group(
    "",
    (o.hosts ? row("Host", "Each host runs these on its own providers.", sel(o.hosts[0], o.hosts.slice(1))) : "") +
      `<div class="sr stack"><span class="sr-b"><span class="sr-t">Default</span><span class="sr-d">For every job that has no choice of its own.</span></span><span class="sr-e two">${sel("Claude (host default)")}${sel("Haiku (claude-haiku-5-5)")}</span></div>` +
      `<div class="sr stack"><span class="sr-b"><span class="sr-t">Thread titles</span><span class="sr-d">Runs on Claude · claude-haiku-5-5</span></span><span class="sr-e">${sel("Same as default")}</span></div>` +
      (o.badJob
        ? `<div class="sr stack bad"><span class="sr-b"><span class="sr-t">Project setup</span><span class="sr-d err">Z.ai has no Haiku model: pick a model here.</span></span><span class="sr-e two">${sel("Z.ai")}${sel("Pick a model")}</span></div>`
        : `<div class="sr stack"><span class="sr-b"><span class="sr-t">Project setup</span><span class="sr-d">Runs on Claude · claude-haiku-5-5</span></span><span class="sr-e">${sel("Same as default")}</span></div>`),
    "The model calls Fyr makes itself. A job that cannot run fails and says why; it never moves to a bigger model.",
  );

const providers = {
  nav: "providers",
  title: "Providers",
  body: group("", PROVS.map(prow).join("")) + found + `<div class="sbar"><button class="btn">Add provider</button></div>` + sysAgents(),
};

const provider = {
  nav: "providers",
  parent: "Providers",
  title: "Claude (work account)",
  end: '<span class="pst needs-login">Needs a login</span>',
  body:
    h`<div class="sg"><div class="sg-h">Login</div>
<div class="login"><div class="setd"><b>1.</b> Open the sign-in page and sign in. <b>2.</b> Paste the code it shows.</div>
<div><a class="btn p">Open the sign-in page</a></div>
<label class="fld"><span>Code from the sign-in page</span><div class="setkey"><input placeholder="Paste the code"><button class="btn" disabled>Send code</button></div></label>
<div class="setbar"><span class="setd" style="margin-right:auto">The sign-in ends at 18:21</span><button class="btn">Cancel</button></div></div>
<div class="sg-c">${row("Login token", "Set · used instead of a browser sign-in", '<button class="btn">Replace…</button>')}</div></div>` +
    group(
      'Models <span class="end saved" aria-live="polite">Saved</span>',
      `<div class="sr"><span class="sr-b"><span class="sr-t">Sonnet 5.5 <span class="dflt">Default</span></span><span class="sr-d mono">claude-sonnet-5-5</span></span><span class="sr-e"><button class="xb" aria-label="Remove claude-sonnet-5-5" disabled>×</button></span></div>
<div class="sr"><span class="sr-b"><span class="sr-t">Haiku 5.5</span><span class="sr-d mono">claude-haiku-5-5</span></span><span class="sr-e"><button class="btn">Make default</button><button class="xb" aria-label="Remove claude-haiku-5-5">×</button></span></div>
<div class="sr"><span class="inl"><input placeholder="Add a model id" aria-label="Model id to add" class="mono"></span><span class="sr-e"><button class="btn" disabled>Add</button></span></div>` + row("Default effort", "", sel("CLI default", ["low", "medium", "high"])),
    ) +
    group("Usage", row('<span class="hot">5 h 86 %</span> · 7 d 40 %', "Read from the Claude login · used by 2 threads")) +
    group("", row("Remove provider", "2 threads use it. Hand them over first.", '<button class="btn danger" disabled>Remove…</button>'), "", "danger-z"),
};

const addProvider = {
  nav: "providers",
  parent: "Providers",
  title: "Add provider",
  body: h`<div class="sg"><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t">Kind</span></span><span class="sr-e"><span class="seg" role="radiogroup" aria-label="Kind"><button>Claude account</button><button class="on">Endpoint</button></span></span></div>
<div class="sr col"><label class="fld"><span>Name</span><input placeholder="OpenRouter"></label></div>
<div class="sr col"><label class="fld"><span>Base URL</span><input placeholder="https://openrouter.ai/api"></label></div>
<div class="sr col"><label class="fld"><span>API key</span><input type="password" placeholder="Paste the key"></label><span class="setd">Kept in a private file on this host.</span></div>
<div class="sr col"><label class="fld"><span>Model ids, one per line</span><textarea rows="3" class="mono"></textarea></label></div>
</div></div>
<div class="sbar"><button class="btn end">Cancel</button><button class="btn p">Add provider</button></div>`,
};

const automation = {
  nav: "automation",
  title: "Automation",
  lede: "What Fyr does without your press. Only you change these; an agent cannot.",
  body:
    group("", `<label class="sr"><span class="sr-b"><span class="sr-t">Deploy Fyr without asking</span><span class="sr-d">When an agent asks to deploy Fyr and the checkout is clean and on origin's default branch. The build and the tests still decide the switch.</span></span><span class="sr-e">${swt(true, "Deploy Fyr without asking")}</span></label>`) +
    group(
      "Merge without asking",
      `<label class="sr"><span class="sr-b"><span class="sr-t mono">meros/fyr</span></span><span class="sr-e">${swt(false, "Merge without asking in meros/fyr")}</span></label>
<label class="sr"><span class="sr-b"><span class="sr-t mono">norban/web</span></span><span class="sr-e">${swt(true, "Merge without asking in norban/web")}</span></label>
<div class="sr"><span class="inl"><input placeholder="owner/repo" aria-label="Repo to add" class="mono"></span><span class="sr-e"><button class="btn" disabled>Add</button></span></div>`,
      "In a repo that is on, an agent's merge ask merges once the checks pass on the head it asked for. A repo shows here once a thread has a pull request in it.",
    ),
};

const PROJECTS = [
  { n: "Bokföring åt Övik", f: "~/ekonomi", c: 0, i: { mono: "BÅ", color: "brown" } },
  { n: "dotfiles", f: "~/dotfiles", c: 1, i: { mono: "›_", color: "olive" } },
  { n: "Fyr", f: "~/git/fyr", c: 0, i: { img: "fyr.png", color: "brown" } },
  { n: "Hemmet", f: "No folder: filed by hand", c: 1, i: { emoji: "🏡", color: "green" } },
  { n: "Map perf lab", f: "~/src/maplab", c: 0, i: { mono: "M", color: "blue" } },
  { n: "norban-platform-infrastructure-terraform-modules", f: "~/src/norban-platform-infrastructure-terraform-modules", c: 3, i: { mono: "NP", color: "indigo" } },
  { n: "norban/api", f: "~/src/norban/api", c: 0, i: { mono: "NA", color: "sky" } },
  { n: "norban/infra", f: "~/src/norban/infra", c: 1, i: { mono: "NI", color: "slate" } },
  { n: "norban/ops", f: "~/src/norban/ops", c: 2, i: { emoji: "🚀", color: "rose" } },
  { n: "norban/web", f: "~/src/norban/web", c: 9, i: { mono: "NW", color: "teal" } },
];
const plist = (list, cls = "") =>
  `<div class="pls ${cls}">${list.map((p) => `<button class="pli" style="--pc:var(--p-${p.i.color})">${pi(p.i)}<span class="plt"><span class="ell pln">${p.n}</span><span class="ell pls2">${p.f}</span></span><span class="plc" title="${p.c} threads">${p.c}</span></button>`).join("")}</div>`;
const hostsGroup = (list) => `<h3 class="stp-sub">Hosts</h3>` + group("", list.map((x) => row(`<span class="mono">${x[0]}</span>`, `<span class="${x[2] ? "err" : ""}">${x[1]}</span>`)).join(""));

const projects = {
  nav: "projects",
  title: "Projects",
  end: '<button class="btn">New project</button>',
  body: plist(PROJECTS) + hostsGroup([["desk", "This host · online since 08:12"]]),
};

const days = (on) => `<div class="qd" role="group" aria-label="Days">${["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((d, i) => `<button aria-pressed="${on.includes(i)}" class="${on.includes(i) ? "on" : ""}">${d}</button>`).join("")}</div>`;
const time = (a, b) => `<span class="qt">${sel(a)}:${sel(b)}</span>`;
const qwin = (from, to, on, all) =>
  `<div class="sr col"><div class="qw-top">${all ? "" : `${time(...from)} <span>–</span> ${time(...to)}`}<label class="allday"${all ? ' style="margin-left:0"' : ""}>All day ${swt(!!all, "All day")}</label><button class="xb" aria-label="Remove these quiet hours">×</button></div>${days(on)}</div>`;
const swatches = (cur) => `<div class="swatches" role="radiogroup" aria-label="Colour">${["slate", "blue", "sky", "teal", "green", "olive", "violet", "indigo", "rose", "brown"].map((c) => `<button class="sw${c === cur ? " on" : ""}" role="radio" aria-checked="${c === cur}" style="--pc:var(--p-${c})" aria-label="${c}"></button>`).join("")}</div>`;
const SETBY = '<em class="setby">Set by agent</em>';

const project = {
  nav: "projects",
  parent: "Projects",
  title: "Fyr",
  body:
    `<div class="sbar" style="--pc:var(--p-brown)">${pi({ img: "fyr.png", color: "brown" }, 44)}<span class="plt"><span class="pln big">Fyr</span><span class="pls2">~/git/fyr · 4 threads</span></span></div>` +
    group(
      'Identity <span class="end saved" aria-live="polite">Saved</span>',
      `<div class="sr stack"><span class="sr-b"><span class="sr-t">Name ${SETBY}</span></span><span class="sr-e wide"><span class="inl"><input value="Fyr" aria-label="Name"></span></span></div>
<div class="sr stack"><span class="sr-b"><span class="sr-t">Colour ${SETBY}</span></span><span class="sr-e">${swatches("brown")}</span></div>
<div class="sr col"><div class="sbar"><span class="sr-t">Icon ${SETBY}</span><span class="seg end" role="radiogroup" aria-label="Icon"><button class="on">Image</button><button>Letters</button><button>Emoji</button><button>Symbol</button></span></div>
<div class="thumb">${pi({ img: "fyr.png" }, 44)}<button class="btn">Replace…</button><button class="btn">Remove</button><span class="setd">PNG, JPEG or GIF, at most 2 MB.</span></div></div>` +
        row("Set up automatically", "An agent finds the logo and the real name and fills in what you have not set. Last run: today 09:14.", '<button class="btn">Run again</button>'),
    ) +
    group(
      "Threads",
      row("Folder", '<span class="mono">~/git/fyr</span> · threads started here or below land in this project.', '<button class="btn">Change…</button>') +
        `<div class="sr stack"><span class="sr-b"><span class="sr-t">Default provider and model</span><span class="sr-d">What a new thread here starts with.</span></span><span class="sr-e two">${sel("Claude")}${sel("The provider's default")}</span></div>`,
    ) +
    group(
      'Quiet hours <span class="end">Quiet now, until 06:00</span>',
      qwin(["17", "00"], ["06", "00"], [0, 1, 2, 3, 4]) + qwin(0, 0, [5, 6], true) + `<div class="sr"><div class="chips"><button class="btn">Add quiet hours</button></div></div>`,
      "In quiet hours this project's threads are hidden and send no notifications: weekdays 17:00–06:00, and all weekend.",
    ) +
    group("", row("Delete project", "Its 4 threads stay, without a project.", '<button class="btn danger">Delete…</button>'), "", "danger-z"),
};

const device = (push = "off") => ({
  nav: "device",
  title: "This device",
  lede: "These are kept in this browser. Each device has its own.",
  body: group(
    "",
    (push === "on"
      ? `<label class="sr"><span class="sr-b"><span class="sr-t">Notifications</span><span class="sr-d">When a thread asks you something or fails.</span></span><span class="sr-e"><button class="btn">Send test</button>${swt(true, "Notifications")}</span></label>`
      : push === "denied"
        ? `<div class="sr"><span class="sr-b"><span class="sr-t">Notifications</span><span class="sr-d err">Blocked for this site. Allow them in the browser's site settings, then reload Fyr.</span></span><span class="sr-e">${swt(false, "Notifications", true)}</span></div>`
        : push === "ios"
          ? `<div class="sr"><span class="sr-b"><span class="sr-t">Notifications</span><span class="sr-d">On an iPhone, add Fyr to the Home Screen first: Share, then Add to Home Screen.</span></span><span class="sr-e">${swt(false, "Notifications", true)}</span></div>`
          : `<label class="sr"><span class="sr-b"><span class="sr-t">Notifications</span><span class="sr-d">When a thread asks you something or fails.</span></span><span class="sr-e">${swt(false, "Notifications")}</span></label>`) +
      `<div class="sr stack"><span class="sr-b"><span class="sr-t">Theme</span></span><span class="sr-e"><span class="seg" role="radiogroup" aria-label="Theme"><button class="on">Auto</button><button>Light</button><button>Dark</button></span></span></div>` +
      `<label class="sr"><span class="sr-b"><span class="sr-t">Show tool details</span><span class="sr-d">Every tool group open, every machine line in full.</span></span><span class="sr-e">${swt(false, "Show tool details")}</span></label>` +
      row("Install Fyr", "As an app with its own window.", '<button class="btn">Install</button>'),
  ),
});

const moreSheet = `<div class="phx sheet" style="min-height:420px"><div class="m-sheet">
<div class="m-it">Select threads</div>
<div class="m-it"><span>Changes<small>Every change across threads, by stage</small></span><span class="r">2 need you</span></div>
<div class="m-it"><span>Settings<small>Providers, automation, projects, this device</small></span><span class="r">1 needs a login</span></div>
<div class="m-it">Filter by project…<span class="r">all</span></div>
<div class="m-it">Adopt from T3</div>
<div class="m-it"><span>Show tool details<small>Every tool group open, every machine line in full</small></span><span class="r">off</span></div>
</div></div>`;

const controls = `<div class="bd-f" style="max-width:820px"><span class="bd-cap">The control set</span><div class="dlgx one"><div class="stp">
${group(
  "One control per kind of choice",
  row("On or off", "A switch. The row is its label; on the phone the whole row is the target.", swt(true, "Example") + swt(false, "Example")) +
    row("One of two to four", "A segmented control, the same one everywhere (theme, provider kind, icon kind).", '<span class="seg"><button class="on">Auto</button><button>Light</button><button>Dark</button></span>') +
    row("One of many", "A select.", sel("Claude")) +
    row("Several of seven", "Day chips, filled when on.", "") +
    `<div class="sr">${days([0, 1, 2, 3, 4])}</div>` +
    `<div class="sr"><span class="sr-b"><span class="sr-t">Text</span><span class="sr-d">Saves on Enter. Save shows only while the text differs; Escape puts it back.</span></span><span class="sr-e wide"><span class="inl"><input value="Fyr lighthouse"><button class="btn p">Save</button></span></span></div>` +
    row("A page", "A row with a chevron opens the next page.", '<span class="chev">›</span>') +
    row("An action", "A button at the end of the row it acts on. Red text for one that removes.", '<button class="btn">Import</button><button class="btn danger">Remove…</button>'),
  "Help text sits under the label of the row it explains, never above a section. A group's foot says what the whole group does.",
)}
${group('After a change <span class="end saved">Saved</span>', row("Saved", "The group's header shows Saved for two seconds once the host has answered.", "") + `<div class="sr bad"><span class="sr-b"><span class="sr-t">Refused</span><span class="sr-d err">The control goes back and the row says why, in the host's words.</span></span><span class="sr-e">${swt(false, "Example")}</span></div>`)}
</div></div></div>`;

const NOTE2 = h`<ul>
<li><b>Your three choices are in.</b> Four pages: Providers (with System agents), Automation, Projects (with Hosts), This device. Every change saves at once. The More sheet loses Notifications and Theme.</li>
<li><b>One control per kind of choice.</b> A switch for on or off (today a button that reads "On" or "Off", which looks like an action). One segmented control (today there are three looks). A text field shows Save beside it only while it differs.</li>
<li><b>No Save button at the bottom of a project.</b> Each group header shows "Saved" for two seconds after the host answers; a refused change puts the control back and says why on that row.</li>
<li><b>Models are a list, not a text box.</b> One row per model with Make default and ×, and a field to add an id. Today it is a text area "one id per line" with its own Save.</li>
<li><b>Help text sits under the label it explains.</b> Today it is above the section in Auto-merge, under the field for Image and beside nothing in Hosts.</li>
<li><b>Words.</b> "Deploy Fyr without asking" and "Merge without asking" for the two automations. "Same as default" for a job (was "Same as above"). "Symbol" for the line icons (was "Icon" inside Icon). Found on this host says the key once, not twice. "Quota: anthropic-oauth" becomes "Read from the Claude login".</li>
<li><b>Desktop and phone</b> draw the same page. The differences are size only: 44 px controls and a 56 px row on the phone, and a row whose control is wide stacks under its label there.</li></ul>`;

const SCREENS2 = [
  { h: "0. The controls", why: "The whole set, once. Everything after uses only these.", raw: controls },
  { h: "1. Settings: the way in", why: "Four pages. Desktop opens on Providers; the phone opens on the list, and each row says its state.", page: providers, phonePage: { index: true } },
  { h: "2. Providers, with System agents", why: "Providers first, then what Fyr found, then System agents: they are a choice among these providers, so they sit under them.", page: providers, desktop: false },
  { h: "3. A provider", why: "Login first while it needs one. Models as rows. Remove is last, in a red-edged group, and says why it is off.", page: provider },
  { h: "4. Add provider", why: "A page of its own, not a form that unfolds under the list. The one page in Settings with a primary button, because nothing exists to save until it is added.", page: addProvider },
  { h: "5. Automation", why: "Two switches of the same kind, named by what they let Fyr do. Adding a repo is a field in the list's last row.", page: automation },
  { h: "6. Projects, with Hosts", why: "The projects with New project at the top. Hosts follow: with one host it is one line.", page: projects },
  { h: "7. A project", why: "Identity, Threads, Quiet hours, Delete. The image is one of the four icon kinds, not a field of its own; the setup run is a row in Identity and says when it last ran.", page: project },
  { h: "8. This device", why: "Notifications is a switch; Send test shows beside it once it is on. The desktop rail's bell opens this page.", page: device("off") },
  { h: "9. The phone's More sheet", why: "Six rows. Settings shows the count that needs you.", raw: frame("Phone · More sheet", moreSheet, "bd-phone") },
];

if (!window.I3) render({ title: "Settings redesign · iteration 2: controls and copy", sub: "on the four-page structure", note: NOTE2, screens: SCREENS2 });
