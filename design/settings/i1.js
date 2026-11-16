// Iteration 1: structure. Navigation, grouping and order. The controls are today's (On/Off buttons, selects,
// the models textarea); iteration 2 changes them.

const NAV = [
  {
    group: "Agents",
    items: [
      { id: "providers", title: "Providers", sub: "3 · 1 needs a login", badge: '<span class="pst needs-login">1</span>' },
      { id: "system", title: "System agents", sub: "Haiku on Claude" },
      { id: "automation", title: "Automation", sub: "Deploys on · merges off" },
    ],
  },
  {
    group: "Work",
    items: [
      { id: "projects", title: "Projects", sub: "10 projects" },
      { id: "hosts", title: "Hosts", sub: "desk" },
    ],
  },
  {
    group: "This device",
    items: [
      { id: "notifications", title: "Notifications", sub: "Off" },
      { id: "appearance", title: "Appearance", sub: "Auto theme" },
    ],
  },
];

const onoff = (on, label) => `<button class="btn onoff${on ? " on" : ""}" aria-pressed="${on}" aria-label="${label}">${on ? "On" : "Off"}</button>`;
const sel = (v, w) => `<select${w ? ` style="width:${w}px"` : ""}><option>${v}</option></select>`;
const prow = (o) =>
  `<button class="sr"><span class="pdot" style="background:${o.dot}"></span><span class="sr-b"><span class="sr-t">${o.name}</span><span class="sr-d">${o.line}</span>${o.quota ? `<span class="sr-d">${o.quota}</span>` : ""}</span><span class="sr-e"><span class="pst ${o.cls || ""}">${o.status}</span><span class="chev">›</span></span></button>`;

const providers = {
  nav: "providers",
  title: "Providers",
  body: h`
<div class="sg"><div class="sg-c">
${prow({ dot: "#d97757", name: "Claude", line: "Claude account · ~/.claude · Opus 5.5, Sonnet 5.5 + 1", quota: "5 h 34 % · 7 d 61 %", status: "Ready" })}
${prow({ dot: "#2f6fb0", name: "Claude (work account)", line: "Claude account · token login · Sonnet 5.5", quota: '<span class="hot">5 h 86 %</span> · 7 d 40 %', status: "Needs a login", cls: "needs-login" })}
${prow({ dot: "#7d8894", name: "DeepSeek", line: "Endpoint · api.deepseek.com · deepseek-chat, deepseek-reasoner", status: "Ready" })}
</div></div>
<div class="sg"><div class="sg-h">Found on this host</div><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t">OpenRouter</span><span class="sr-d">Endpoint · key OPENROUTER_API_KEY in fyrd's environment</span><span class="sr-d">Add the model ids this endpoint should offer.</span></span><span class="sr-e"><button class="btn">Import</button></span></div>
<div class="sr"><span class="sr-b"><span class="sr-t">Claude (old)</span><span class="sr-d">Claude account · ~/.claude-old</span></span><span class="sr-e"><button class="btn">Import</button></span></div>
</div></div>
<div class="sbar"><button class="btn">Add provider</button></div>`,
};

const provider = {
  nav: "providers",
  parent: "Providers",
  title: "Claude (work account)",
  end: '<span class="pst needs-login">Needs a login</span>',
  body: h`
<div class="sg"><div class="sg-h">Login</div>
<div class="login"><div class="setd">Open the sign-in page, sign in, then paste the code it shows here.</div>
<div><a class="btn p">Open the sign-in page</a></div>
<label class="fld"><span>Code from the sign-in page</span><div class="setkey"><input placeholder="Paste the code"><button class="btn" disabled>Send code</button></div></label>
<div class="setbar"><span class="setd" style="margin-right:auto">Ends at 18:21</span><button class="btn">Cancel</button></div></div>
<label class="fld"><span>Login token</span><div class="setkey"><input type="password" placeholder="Paste a new key to replace it"><button class="btn" disabled>Save key</button></div></label>
</div>
<div class="sg"><div class="sg-h">Models</div>
<label class="fld"><span>Model ids (one per line)</span><textarea rows="2">claude-sonnet-5-5</textarea></label>
<div class="row2"><label class="fld"><span>Default model</span>${sel("Sonnet 5.5")}</label><label class="fld"><span>Default effort</span>${sel("CLI default")}</label></div>
<div class="setbar"><button class="btn" disabled>Save models</button></div>
</div>
<div class="sg"><div class="sg-h">Usage</div><div class="sg-c"><div class="sr"><span class="sr-b"><span class="sr-t"><span class="hot">5 h 86 %</span> · 7 d 40 %</span><span class="sr-d">Quota: anthropic-oauth · 2 threads</span></span></div></div></div>
<div class="sg"><div class="sbar"><button class="btn danger" disabled>Remove</button><span class="setd">Threads use it; hand them over first.</span></div></div>`,
};

const system = {
  nav: "system",
  title: "System agents",
  lede: "The model calls Fyr makes itself, such as thread titles and project setup. A job that cannot run fails and shows why; it never moves to a bigger model.",
  body: h`
<div class="sg"><div class="sg-c"><div class="sr"><span class="sr-b"><span class="sr-t">Host</span><span class="sr-d">Providers stay on their host, so each host has its own setting.</span></span><span class="sr-e">${sel("desk")}</span></div></div></div>
<div class="sg"><div class="sg-h">All jobs</div><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t">Provider</span></span><span class="sr-e">${sel("Default (Claude)")}</span></div>
<div class="sr"><span class="sr-b"><span class="sr-t">Model</span></span><span class="sr-e">${sel("Haiku (claude-haiku-5-5)")}</span></div>
</div></div>
<div class="sg"><div class="sg-h">Per job</div><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t">Thread titles</span><span class="sr-d">Runs on Claude · claude-haiku-5-5</span></span><span class="sr-e">${sel("Same as above")}</span></div>
<div class="sr"><span class="sr-b"><span class="sr-t">Project setup</span><span class="sr-d">Runs on Claude · claude-haiku-5-5</span></span><span class="sr-e">${sel("Same as above")}</span></div>
</div></div>`,
};

const automation = {
  nav: "automation",
  title: "Automation",
  lede: "What Fyr does without your press. Only you change these; no agent tool can.",
  body: h`
<div class="sg"><div class="sg-h">Auto-deploy</div><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t">Approve Fyr deploys</span><span class="sr-d">An agent's Fyr deploy runs without your press when the checkout has no uncommitted change and its commit is on origin's default branch. One deploy runs at a time; the build and the tests still decide the switch.</span></span><span class="sr-e">${onoff(true, "Auto-approve Fyr deploys")}</span></div>
</div></div>
<div class="sg"><div class="sg-h">Auto-merge</div><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t mono">meros/fyr</span></span><span class="sr-e">${onoff(false, "Auto-merge meros/fyr")}</span></div>
<div class="sr"><input class="in" placeholder="owner/repo" aria-label="Repo to turn on"><span class="sr-e"><button class="btn" disabled>Turn on</button></span></div>
</div><div class="sg-f">An agent's merge ask in a repo turned on here merges without your press, once the checks pass on the head it asked for.</div></div>`,
};

const PROJECTS = [
  { n: "Bokföring åt Åsa", f: "~/ekonomi", c: 0, i: { mono: "BÅ", color: "brown" } },
  { n: "dotfiles", f: "~/dotfiles", c: 1, i: { mono: "›_", color: "olive" } },
  { n: "Fyr", f: "~/git/fyr", c: 0, i: { img: "fyr.png", color: "brown" } },
  { n: "Garden", f: "No folder: filed by hand", c: 1, i: { emoji: "🏡", color: "green" } },
  { n: "Map perf lab", f: "~/src/maplab", c: 0, i: { mono: "M", color: "blue" } },
  { n: "acme-platform-infrastructure-terraform-modules", f: "~/src/acme-platform-infrastructure-terraform-modules", c: 3, i: { mono: "NP", color: "indigo" } },
  { n: "acme/api", f: "~/src/acme/api", c: 0, i: { mono: "NA", color: "sky" } },
  { n: "acme/infra", f: "~/src/acme/infra", c: 1, i: { mono: "NI", color: "slate" } },
  { n: "acme/ops", f: "~/src/acme/ops", c: 2, i: { emoji: "🚀", color: "rose" } },
  { n: "acme/web", f: "~/src/acme/web", c: 9, i: { mono: "NW", color: "teal" } },
];
const plist = (list) =>
  `<div class="pls">${list.map((p) => `<button class="pli" style="--pc:var(--p-${p.i.color})">${pi(p.i)}<span class="plt"><span class="ell pln">${p.n}</span><span class="ell pls2">${p.f}</span></span><span class="plc">${p.c}</span></button>`).join("")}</div>`;

const projects = {
  nav: "projects",
  title: "Projects",
  end: '<button class="btn">New project</button>',
  body: plist(PROJECTS),
};

const days = (on) => `<div class="qd" role="group" aria-label="Days">${["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"].map((d, i) => `<button class="${on.includes(i) ? "on" : ""}">${d}</button>`).join("")}</div>`;
const time = (a, b) => `<span class="qt">${sel(a)}:${sel(b)}</span>`;

const project = {
  nav: "projects",
  parent: "Projects",
  title: "Garden",
  body: h`
<div class="sbar" style="--pc:var(--p-green)">${pi({ emoji: "🏡", color: "green" }, 44)}<span class="plt"><span class="pln big">Garden</span><span class="pls2">No folder: filed by hand</span></span></div>
<div class="sg"><div class="sg-h">Identity</div><div class="sg-c">
<div class="sr col"><label class="fld"><span>Name</span><input value="Garden"></label></div>
<div class="sr col"><div class="fld"><span>Colour</span><div class="swatches">${["slate", "blue", "sky", "teal", "green", "olive", "violet", "indigo", "rose", "brown"].map((c) => `<button class="sw${c === "green" ? " on" : ""}" style="--pc:var(--p-${c})" aria-label="${c}"></button>`).join("")}</div></div></div>
<div class="sr col"><div class="fld"><span>Icon</span><div class="seg"><button>Image</button><button>Letters (H)</button><button class="on">Emoji</button><button>Icon</button></div><input value="🏡" style="max-width:90px;text-align:center"></div></div>
</div><div class="sg-f">A project with a folder can be set up by an agent: it finds the logo and the real name, and fills in what you have not set.</div></div>
<div class="sg"><div class="sg-h">Threads</div><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t">Folder</span><span class="sr-d">No folder: you file threads here by hand.</span></span><span class="sr-e"><button class="btn">Choose folder…</button></span></div>
<div class="sr"><span class="sr-b"><span class="sr-t">Default provider</span></span><span class="sr-e">${sel("None")}</span></div>
<div class="sr"><span class="sr-b"><span class="sr-t">Default model</span></span><span class="sr-e">${sel("None")}</span></div>
</div></div>
<div class="sg"><div class="sg-h">Quiet hours <span class="end">Quiet until 16:30</span></div><div class="sg-c">
<div class="sr col"><div class="sbar">${time("13", "30")} – ${time("16", "30")}<label class="sbar"><input type="checkbox"> All day</label><button class="btn end" aria-label="Remove these quiet hours">×</button></div>${days([0, 1, 2, 3, 4, 5, 6])}</div>
<div class="sr col"><div class="sbar">${time("00", "30")} – ${time("01", "30")}<label class="sbar"><input type="checkbox"> All day</label><button class="btn end" aria-label="Remove these quiet hours">×</button></div>${days([5, 6])}</div>
<div class="sr"><div class="sbar"><button class="btn">+ Add quiet hours</button><button class="btn">Weeknights <span class="setd">17:00–06:00</span></button><button class="btn">All weekend <span class="setd">Sat–Sun</span></button></div></div>
</div><div class="sg-f">Threads hide and send no notifications daily 13:30–16:30 and weekends 00:30–01:30.</div></div>
<div class="sbar"><button class="btn danger">Delete project…</button><button class="btn p end">Save</button></div>`,
};

const hosts = {
  nav: "hosts",
  title: "Hosts",
  body: h`
<div class="sg"><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t mono">desk</span><span class="sr-d">this host · online since 08:12</span></span></div>
<div class="sr"><span class="sr-b"><span class="sr-t mono">host-a</span><span class="sr-d">offline since 19:40 · its threads are read-only</span></span></div>
</div></div>`,
};

const notifications = {
  nav: "notifications",
  title: "Notifications",
  body: h`
<div class="sg"><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t">Notifications are off</span><span class="sr-d">Fyr notifies only when a thread enters Needs you.</span></span><span class="sr-e"><button class="btn">Turn on</button></span></div>
</div><div class="sg-f">This is for this browser only. Each device you use has its own.</div></div>`,
};

const appearance = {
  nav: "appearance",
  title: "Appearance",
  body: h`
<div class="sg"><div class="sg-c">
<div class="sr"><span class="sr-b"><span class="sr-t">Theme</span></span><span class="sr-e"><span class="seg"><button class="on">Auto</button><button>Light</button><button>Dark</button></span></span></div>
<div class="sr"><span class="sr-b"><span class="sr-t">Show tool details</span><span class="sr-d">Every tool group open, every machine line in full.</span></span><span class="sr-e">${onoff(false, "Show tool details")}</span></div>
</div></div>`,
};

const moreSheet = `<div class="phx sheet" style="min-height:560px"><div class="m-sheet">
<div class="m-it">Select threads</div>
<div class="m-it"><span>Changes<small>Every change across threads, by stage</small></span><span class="r">2 need you</span></div>
<div class="m-it"><span>Settings<small>Providers, projects, notifications, appearance</small></span><span class="r">1 needs a login</span></div>
<div class="m-it">Filter by project…<span class="r">all</span></div>
<div class="m-it">Adopt from T3</div>
<div class="m-it"><span>Show tool details</span><span class="r">off</span></div>
<div class="m-it gone">Notifications<span class="r">→ Settings</span></div>
<div class="m-it gone">Theme: auto<span class="r">→ Settings</span></div>
<div class="m-it gone">Theme: light</div>
<div class="m-it gone">Theme: dark</div>
</div></div>`;

render({
  title: "Settings redesign · iteration 1: structure",
  sub: "navigation, grouping, order",
  note: h`<ul>
<li><b>One long page becomes seven short ones.</b> Today Settings is eight sections in one scroll (about 3 900 px on the phone; the desktop dialog shows a section and a half). Now: a nav with seven pages in three groups. On desktop the nav is the dialog's left column; on the phone the same nav is the Settings screen and a page is the next screen. One component, two placements; the reason for the difference is width.</li>
<li><b>Order follows use.</b> Agents (providers, system agents, automation) first, then Work (projects, hosts), then This device. Auto-merge and Auto-deploy are one page, Automation: both are "Fyr acts without your press".</li>
<li><b>A list row opens a page.</b> A provider no longer unfolds inside the list; it opens Providers / Claude. The project screen becomes Projects / Garden inside Settings on desktop too (today it is a second dialog).</li>
<li><b>The project page is grouped</b>: Identity (name, colour, icon), Threads (folder, default provider and model), Quiet hours. Today the nine fields are one column and Quiet hours sits between Name and Colour.</li>
<li><b>Each nav row says the state</b> (3 providers, 1 needs a login; notifications off), so the phone's Settings screen is also a status page.</li>
<li>Controls and words are still today's here. Iteration 2 does those.</li></ul>`,
  screens: [
    { h: "1. Settings: the way in", why: "Desktop opens on Providers. The phone opens on the nav itself. The summaries are the same text on both.", page: providers, phonePage: { index: true } },
    { h: "2. Providers", why: "The providers in one card, one row each: name, what it is, the quota line, the status. Found on this host keeps its Import. Add provider is one button at the end.", page: providers, desktop: false },
    { h: "3. A provider", why: "Its own page: Login first (it is what blocks), then Models, Usage, Remove. Today all of this unfolds inside the list row.", page: provider },
    { h: "4. System agents", why: "The Host picker shows only with more than one host. All jobs, then one row per job with what it runs on.", page: system },
    { h: "5. Automation", why: "Auto-deploy and Auto-merge on one page. The long help text moves from above the control to the row it explains.", page: automation },
    { h: "6. Projects", why: "The list alone on its page, so ten projects no longer push the other settings down. New project sits at the top.", page: projects },
    { h: "7. A project", why: "Three groups. Set up automatically belongs to Identity, because it fills the name, the colour and the image, not only the image.", page: project },
    { h: "8. Hosts", why: "The same card rows as everything else; today it is two lines of bare text.", page: hosts },
    { h: "9. Notifications", why: "A state and one action. Today the phone shows a button that reads “Notifications are off” and the desktop a bare bell.", page: notifications },
    { h: "10. Appearance", why: "Theme and Show tool details: both are this device's display choices.", page: appearance },
    { h: "11. The phone's More sheet", why: "Proposed: Notifications and the three Theme rows leave the sheet, because Settings has them. Six rows instead of ten. This is a behaviour change; say if you want them kept.", raw: frame("Phone · More sheet", moreSheet, "bd-phone") },
  ],
});
