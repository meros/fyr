// Iteration 3: the final set. Part A is every screen (iteration 2 with the changes listed in the note, plus the
// model chooser and the project order the owner added). Part B is the one reorder control. Part C is the states:
// loading, errors, a missing key, discovery, long lists, empty lists, refusals, notifications, touch targets.
// Loads after i2.js and reuses its helpers and pages.

const GRIP = '<span class="grip" role="button" tabindex="0" aria-label="Move. Alt and the arrow keys move it.">⠿</span>';
const UPDOWN = (n) => `<span class="sr-e mv"><button class="xb" aria-label="Move ${n} up">↑</button><button class="xb" aria-label="Move ${n} down">↓</button></span>`;
const strip = (s) => s.replaceAll(' <span class="end saved" aria-live="polite">Saved</span>', "");

/** One model row. o: {name, id, on, star, tag, note, mode: "" | "reorder" | "off" | "lift" | "drop", hand} */
const mrow = (o) => {
  const tag = o.tag === "new" ? '<span class="tag new">New</span>' : o.tag === "gone" ? '<span class="tag gone">Gone</span>' : o.hand ? '<span class="tag">Added by hand</span>' : "";
  const end = o.mode === "reorder" ? UPDOWN(o.id) : `<span class="sr-e">${o.hand && !o.on ? `<button class="xb" aria-label="Remove ${o.id}">×</button>` : ""}${swt(o.on, `Offer ${o.id}`, o.star)}</span>`;
  const lead = o.on ? (o.mode === "reorder" ? "" : GRIP) + `<button class="star" aria-pressed="${!!o.star}" aria-label="${o.star ? "The default model" : `Make ${o.id} the default`}">${o.star ? "★" : "☆"}</button>` : "";
  return `<div class="sr${o.on ? "" : " off"}${o.mode === "lift" ? " lift" : o.mode === "drop" ? " drop" : ""}">${lead}<span class="sr-b"><span class="sr-t">${o.name}${tag}</span><span class="sr-d mono">${o.id}</span>${o.note ? `<span class="sr-d">${o.note}</span>` : ""}</span>${end}</div>`;
};
const ON = [
  { name: "Opus 5.5", id: "claude-opus-5-5", on: true, star: true },
  { name: "Sonnet 5.5", id: "claude-sonnet-5-5", on: true },
  { name: "Haiku 5.5", id: "claude-haiku-5-5", on: true },
  { name: "Opus 5.4", id: "claude-opus-5-4", on: true, tag: "gone", note: "The provider no longer lists it. Threads on it keep running." },
];
const OFF = [
  { name: "Fable 5.2", id: "claude-fable-5-2", tag: "new" },
  { name: "Sonnet 5.4", id: "claude-sonnet-5-4" },
  { name: "Haiku 5.4", id: "claude-haiku-5-4" },
];
/** The Models group. o: {mode, status, statusErr, search, on, off, more, manual} */
const models = (o = {}) => {
  const on = o.on || ON,
    off = o.off || OFF;
  const hd = `Models <span class="end">${o.status || "Checked 12 min ago"}<button class="lnk">Refresh</button>${o.mode === "reorder" ? '<button class="lnk"><b>Done</b></button>' : o.search ? "" : '<button class="lnk reo">Reorder</button>'}</span>`;
  return group(
    hd,
    (o.search !== undefined ? `<div class="sr srch"><input class="pq" type="search" placeholder="Search models" value="${o.search}" aria-label="Search models"></div>` : "") +
      (on.length ? `<div class="sg-sub">On · offered in every model picker, in this order</div>` + on.map((m, i) => mrow({ ...m, mode: o.mode === "reorder" ? "reorder" : o.lift === i ? "lift" : o.drop === i ? "drop" : "" })).join("") : "") +
      (o.mode === "reorder" ? "" : (off.length ? `<div class="sg-sub">Off · ${off.length + (o.more || 0)}</div>` + off.map((m) => mrow({ ...m, on: false })).join("") : "") + (o.more ? `<button class="more">Show ${o.more} more</button>` : "") + `<div class="sr"><span class="inl"><input placeholder="Add a model id by hand" aria-label="Model id to add" class="mono"></span><span class="sr-e"><button class="btn" disabled>Add</button></span></div>`) +
      (o.mode === "reorder" ? "" : row("Default effort", "", sel("CLI default", ["low", "medium", "high"]))),
    o.statusErr ? `<span class="sr-d err">${o.statusErr}</span>` : "★ is the default for a new thread. Fyr asks the provider for its models when you open this page and once a day.",
  );
};

const claude = {
  nav: "providers",
  parent: "Providers",
  title: "Claude",
  saved: true,
  end: '<span class="pst">Ready</span>',
  body:
    group("Login", row("Logged in", '<span class="mono">~/.claude</span> · Claude account', '<button class="btn">Log in again</button>') + row("Login token", "Not set. Optional: a token is used instead of the browser sign-in.", '<button class="btn">Set…</button>')) +
    models() +
    group("Usage", row("5 h 34 % · 7 d 61 %", "Read from the Claude login · used by 9 threads")) +
    group("", row("Remove provider", "9 threads use it. Hand them over first.", '<button class="btn danger" disabled>Remove…</button>'), "", "danger-z"),
};

const prows = (mode, lift) =>
  `<div class="pls">${PROJECTS.slice(0, 7)
    .map(
      (p, i) =>
        `<div class="pli g${lift === i ? " lift" : ""}" style="--pc:var(--p-${p.i.color})">${mode === "reorder" ? "" : GRIP}${pi(p.i)}<span class="plt"><span class="ell pln">${p.n}</span><span class="ell pls2">${i === 3 ? "Quiet until 06:00 · " : ""}${p.f}</span></span>${mode === "reorder" ? UPDOWN(p.n).replace("sr-e ", "") : `<span class="plc">${p.c}</span>`}</div>`,
    )
    .join("")}</div>`;
const projects3 = (mode, lift) => ({
  nav: "projects",
  title: "Projects",
  end: mode === "reorder" ? '<button class="btn p">Done</button>' : '<button class="btn reo">Reorder</button><button class="btn">New project</button>',
  body: `<div class="sg">${prows(mode, lift)}<div class="sg-f">This order is used in New thread, the project chips and every list of projects, on all your devices.</div></div>` + (mode === "reorder" ? "" : hostsGroup([["desk", "This host · online since 08:12"]])),
});

const project3 = { ...project, saved: true, body: strip(project.body) };
const provider3 = { ...provider, body: strip(provider.body).replace(/<div class="sg "><div class="sg-h">Models[\s\S]*?<\/div><\/div>(?=<div class="sg "><div class="sg-h">Usage)/, models({ on: [{ name: "Sonnet 5.5", id: "claude-sonnet-5-5", on: true, star: true }], off: [{ name: "Haiku 5.5", id: "claude-haiku-5-5" }], status: "Not checked: needs a login" })) };

// ---- states ----
const skel = (n) => group("", Array.from({ length: n }, (_, i) => `<div class="sr"><span class="sr-b" style="gap:8px"><span class="skel" style="width:${[38, 52, 30, 44][i % 4]}%"></span><span class="skel" style="width:${[62, 48, 70, 55][i % 4]}%;height:10px"></span></span></div>`).join(""));
const loading = { nav: "providers", title: "Providers", body: `<div aria-busy="true" aria-label="Loading the providers">${skel(3)}</div><h3 class="stp-sub">System agents</h3><div aria-busy="true">${skel(2)}</div>` };
const loadErr = { nav: "providers", title: "Providers", body: `<div class="note-err" role="alert"><span>Could not read the providers: desk did not answer in 10 s.</span><button class="btn">Try again</button></div>` + sysAgents() };
const noKey = {
  nav: "providers",
  parent: "Providers",
  title: "DeepSeek",
  end: '<span class="pst needs-key">Needs a key</span>',
  body:
    `<div class="sg"><div class="sg-h">API key</div><div class="sg-c"><div class="sr col bad"><label class="fld"><span>The key is missing. Paste it to use this provider.</span><div class="setkey"><input type="password" placeholder="Paste the key" autofocus><button class="btn p" disabled>Save key</button></div></label><span class="setd">Kept in a private file on desk. It is never shown again.</span></div></div></div>` +
    models({ on: [{ name: "deepseek-chat", id: "deepseek-chat", on: true, star: true }, { name: "deepseek-reasoner", id: "deepseek-reasoner", on: true }], off: [], status: "Not checked: needs a key" }) +
    group("", row("Remove provider", "No thread uses it.", '<button class="btn danger">Remove…</button>'), "", "danger-z"),
};
const loginState = (kind) =>
  `<div class="login">${
    kind === "checking"
      ? `<div class="setd">Checking the code…</div><label class="fld"><span>Code from the sign-in page</span><div class="setkey"><input value="a81f-22c0-9b" disabled><button class="btn" disabled>Send code</button></div></label><div class="setbar"><span class="setd" style="margin-right:auto">The sign-in ends at 18:21</span><button class="btn">Cancel</button></div>`
      : kind === "failed"
        ? `<div class="seterr" role="alert">The sign-in failed: the code was not accepted.</div><div class="setbar"><button class="btn">Try again</button></div>`
        : kind === "timeout"
          ? `<div class="setd">The sign-in ended at 18:21 without a code.</div><div class="setbar"><button class="btn">Try again</button></div>`
          : `<div class="setd" style="color:var(--ink)">Logged in. This provider is ready.</div>`
  }</div>`;
const loginStates = `<div class="bd-f"><span class="bd-cap">Login panel · checking, failed, ended, done</span><div class="dlgx one"><div class="stp">${["checking", "failed", "timeout", "done"].map((k) => `<div class="sg"><div class="sg-h">${k === "timeout" ? "Ended" : k[0].toUpperCase() + k.slice(1)}</div>${loginState(k)}</div>`).join("")}</div></div></div>`;

const discFail = { ...claude, saved: false, title: "DeepSeek", end: '<span class="pst">Ready</span>', body: models({ on: [{ name: "deepseek-chat", id: "deepseek-chat", on: true, star: true }, { name: "deepseek-reasoner", id: "deepseek-reasoner", on: true }], off: [{ name: "my-finetune-v3", id: "my-finetune-v3", hand: true }], status: "Last checked yesterday 14:02", statusErr: "Could not list the models: api.deepseek.com/v1/models answered 401. The list below is the one from yesterday; it still works." }) };
const discBusy = { ...claude, saved: false, body: models({ status: "Checking…" }) };
const manyModels = {
  ...claude,
  saved: false,
  title: "OpenRouter",
  body: models({
    search: "son",
    on: [{ name: "anthropic/claude-sonnet-5.5", id: "anthropic/claude-sonnet-5.5", on: true, star: true }],
    off: [
      { name: "anthropic/claude-sonnet-5.4", id: "anthropic/claude-sonnet-5.4" },
      { name: "anthropic/claude-sonnet-5.5:thinking", id: "anthropic/claude-sonnet-5.5:thinking", tag: "new" },
      { name: "mistralai/mistral-sonnet-experimental-long-context-2026-11", id: "mistralai/mistral-sonnet-experimental-long-context-2026-11" },
    ],
    more: 9,
    status: "13 of 342 match · checked 2 h ago",
  }),
};
const reorderModels = { ...claude, saved: false, body: models({ mode: "reorder" }) };
const dragModels = { ...claude, saved: false, body: models({ lift: 2, drop: 1 }) };

const MANYP = ["Anthropic (team)", "Claude", "Claude (old laptop login, kept for the archive)", "Claude (work account)", "DeepSeek", "Groq", "Kimi", "LM Studio (desk)", "Mistral", "Ollama (desk)", "OpenRouter", "Z.ai"];
const manyProviders = {
  nav: "providers",
  title: "Providers",
  body:
    group(
      "",
      MANYP.map((n, i) => prow({ dot: ["#d97757", "#2f6fb0", "#7d8894", "#1d7a74"][i % 4], name: n, line: i === 3 ? "Claude account · token login · 1 model" : i === 11 ? "Endpoint · api.z.ai · the key is missing" : `${i < 4 ? "Claude account" : "Endpoint"} · ${i % 3 + 2} models${i === 10 ? " · 4 new" : ""}`, status: i === 3 ? "Needs a login" : i === 11 ? "Needs a key" : "Ready", cls: i === 3 ? "needs-login" : i === 11 ? "needs-key" : "" })).join(""),
    ) + `<div class="sbar"><button class="btn">Add provider</button></div>`,
};
const emptyProviders = {
  nav: "providers",
  title: "Providers",
  body: group("", `<div class="empty"><b>No providers yet</b>A provider is a Claude login or an API endpoint that threads run on.</div>`) + found + `<div class="sbar"><button class="btn p">Add provider</button></div><h3 class="stp-sub">System agents</h3>` + group("", `<div class="empty">Add a provider first. System agents run on one of them.</div>`),
};
const manyProjects = {
  nav: "projects",
  title: "Projects",
  end: '<button class="btn">New project</button>',
  body:
    `<div class="sg"><input class="pq" type="search" placeholder="Search 43 projects" value="acme" aria-label="Search projects">${plist(PROJECTS.filter((p) => p.n.startsWith("acme")))}<div class="sg-f">5 of 43 match. Reorder is off while you search.</div></div>` +
    hostsGroup([["desk", "This host · online since 08:12"], ["host-a", "Offline since 19:40 · its threads are read-only"], ["build-box-eu-north-1-with-a-long-name.tail1234.ts.net", "Online since Mon 06:00 · updating Fyr: running the tests"], ["nas", "Offline since Sun 22:15 · connection refused", 1]]),
};
const emptyProjects = { nav: "projects", title: "Projects", end: '<button class="btn p">New project</button>', body: group("", `<div class="empty"><b>No projects yet</b>A project gathers the threads of one folder. Fyr makes one when you start a thread in a new folder.</div>`) + hostsGroup([["desk", "This host · online since 08:12"]]) };
const emptyAuto = {
  ...automation,
  body:
    group("", `<div class="sr bad"><span class="sr-b"><span class="sr-t">Deploy Fyr without asking</span><span class="sr-d err">Not saved: desk answered “only the owner may change this”. It is still on.</span></span><span class="sr-e">${swt(true, "Deploy Fyr without asking")}</span></div>`) +
    group("Merge without asking", `<div class="empty">No repos yet. A repo shows here once a thread has a pull request in it.</div><div class="sr"><span class="inl"><input placeholder="owner/repo" aria-label="Repo to add" class="mono" value="meros/fyr/extra"></span><span class="sr-e"><button class="btn" disabled>Add</button></span></div>`, '<span class="sr-d err">Write it as owner/repo.</span>'),
};
const offline = { ...automation, banner: "No connection to desk. Settings show what was last read and cannot be changed until it is back.", body: `<div class="ro">${automation.body}</div>` };
const sysMulti = { nav: "providers", title: "Providers", body: `<div class="setd">(the providers above)</div>` + sysAgents({ hosts: ["desk", "host-a", "build-box-eu-north-1"], badJob: true }) };
const projEdge = {
  nav: "projects",
  parent: "Projects",
  title: "acme-platform-infrastructure-terraform-modules",
  body:
    group(
      "Identity",
      `<div class="sr stack bad"><span class="sr-b"><span class="sr-t">Name</span><span class="sr-d err">A project needs a name.</span></span><span class="sr-e wide"><span class="inl"><input value="" aria-label="Name" aria-invalid="true"><button class="btn p" disabled>Save</button></span></span></div>
<div class="sr col bad"><div class="sbar"><span class="sr-t">Icon</span><span class="seg end" role="radiogroup" aria-label="Icon"><button class="on">Image</button><button>Letters</button><button>Emoji</button><button>Symbol</button></span></div><div class="thumb">${pi({ mono: "NP", color: "indigo" }, 44)}<button class="btn">Upload…</button><span class="sr-d err">logo-print.png is 6.1 MB. An image is at most 2 MB.</span></div></div>` +
        row("Set up automatically", '<span class="err">Automatic setup could not start: Z.ai has no Haiku model. Pick a model in Providers, System agents.</span>', '<button class="btn">Run again</button>'),
    ) +
    group("Threads", row("Folder", "No folder: you file threads here by hand. Automatic setup needs a folder.", '<button class="btn">Choose…</button>') + `<div class="sr stack"><span class="sr-b"><span class="sr-t">Default provider and model</span><span class="sr-d err">Opus 5.4 is turned off in Claude. New threads here start on Claude's default until you pick another.</span></span><span class="sr-e two">${sel("Claude")}${sel("Opus 5.4 (off)")}</span></div>`) +
    group("Quiet hours", `<div class="sr"><div class="chips"><button class="btn">Add quiet hours</button><button class="btn">Weeknights <span class="setd">17:00–06:00</span></button><button class="btn">All weekend</button></div></div>`, "Off: this project's threads show and notify as usual."),
};
const pushStates = `<div class="bd-f"><span class="bd-cap">Notifications row · on, blocked, iPhone not installed</span><div class="phx" style="min-height:0"><div class="m-body">${["on", "denied", "ios"].map((k) => device(k).body.replace(/<div class="sr stack">[\s\S]*$/, "</div></div>")).join("")}</div></div></div>`;
const offPicker = `<div class="bd-f"><span class="bd-cap">Any other model picker (New thread, Hand over, System agents, project default)</span><div class="dlgx one" style="width:340px"><div class="stp"><div class="fld"><span>Model</span></div><div class="fakesel"><div>Opus 5.5<span class="r">default</span></div><div>Sonnet 5.5</div><div>Haiku 5.5</div><div class="cur offm">Opus 5.4<span class="r">off</span></div></div><div class="setd">The pickers keep their control. They list the models that are on, in your order. A thread, a job or a project that already uses a model that is now off still shows it, marked off, until you pick another; it is not offered anywhere else.</div></div></div></div>`;
const keys = `<div class="bd-f" style="max-width:520px"><span class="bd-cap">Keyboard and focus (desktop)</span><div class="dlgx one" style="width:520px"><div class="stp">${group(
  "",
  row("<kbd>↑</kbd> <kbd>↓</kbd> in the nav", "Moves between the four pages; the page changes with the focus.") +
    row("<kbd>Esc</kbd>", "On a provider or a project: back to its list, focus on the row you came from. On a page: closes Settings. In a changed text field: puts the text back first.") +
    row("<kbd>Enter</kbd> in a text field", "Saves it.") +
    row("<kbd>Alt</kbd> <kbd>↑</kbd> <kbd>↓</kbd> on a grip", "Moves the row. The grip is a focus stop; a screen reader hears “Opus 5.5, 1 of 4”.") +
    row("Opening a page", "Focus goes to its heading. The dialog keeps one size; the pane scrolls, the nav does not."),
)}</div></div></div>`;

render({
  title: "Settings redesign · iteration 3: polish, edge cases, and the two additions",
  sub: "the final set for the hand-off",
  note: h`<ul>
<li><b>Added on your request: the model chooser</b> (A3, B, C4–C6). In a provider's Models group Fyr lists what the provider reports: every model has a switch, the ones that are on can be reordered, ★ is the default. New and Gone are marked. Refresh on demand; Fyr also checks once a day. Search shows from nine models. Every other model picker keeps its control and lists only what is on, in your order.</li>
<li><b>Added on your request: project order</b> (A6, B). You set one order on the Projects page. New thread, the project chips and every project list use it.</li>
<li><b>One reorder control for both.</b> Desktop: drag the grip (or Alt and the arrow keys). Phone: Reorder turns the list into a mode with ↑ and ↓ and Done. The reason for the difference: touch has no hover and a drag fights the scroll.</li>
<li><b>Changed since iteration 2:</b> "Saved" shows once per page, beside the title (some groups have no header). The switch is ink when on, not blue (blue means a link or focus). The provider count in the nav is one short line.</li>
<li><b>States:</b> loading is still rows without movement; a failed read has Try again and leaves the rest of the page alone; a refused change puts the control back and says why on its row; with no connection the page is read-only and says so.</li>
<li><b>Touch:</b> every control on the phone is 44 px high or more, a row is 56 px, and a switch row is one target (C12 draws the outlines at 360 px).</li></ul>`,
  screens: [
    { h: "A1. Settings: the way in", why: "Unchanged from iteration 2.", page: providers, phonePage: { index: true } },
    { h: "A2. Providers, with System agents", why: "Unchanged from iteration 2.", page: providers, desktop: false },
    { h: "A3. A provider, with the model chooser", why: "Models has two parts. On: what the pickers offer, in your order, with ★ on the default. Off: everything else the provider reports. The default's switch is locked: star another model first.", page: claude },
    { h: "A4. A provider that needs a login", why: "Login comes first. Fyr cannot ask the provider for its models before the login, and says so.", page: provider3 },
    { h: "A5. Add provider", why: "Unchanged. After adding an endpoint, Fyr asks it for its models, so the model ids field is optional now.", page: { ...addProvider, body: addProvider.body.replace("Model ids, one per line", "Model ids, one per line (optional: Fyr asks the endpoint for its models)") } },
    { h: "A6. Automation", why: "Unchanged from iteration 2.", page: automation },
    { h: "A7. Projects, in your order", why: "A grip on each row on desktop; Reorder on the phone. A quiet project says so here (this list is where you change its hours); it stays hidden everywhere else.", page: projects3() },
    { h: "A8. A project", why: "As iteration 2, with Saved beside the title.", page: project3 },
    { h: "A9. This device", why: "Unchanged from iteration 2.", page: device("off") },
    { h: "B1. Reorder: models", why: "Desktop: Haiku 5.5 is lifted and the line shows where it lands. Phone: Reorder mode shows only the models that are on, each with ↑ and ↓; Done leaves the mode.", page: dragModels, phonePage: reorderModels, deskCap: "Desktop · dragging", phoneCap: "Phone · reorder mode" },
    { h: "B2. Reorder: projects", why: "The same control. The order saves on each drop or press.", page: projects3("", 2), phonePage: projects3("reorder"), deskCap: "Desktop · dragging", phoneCap: "Phone · reorder mode" },
    { h: "B3. Keyboard, and a model that is off", why: "", raw: keys + offPicker },
    { h: "C1. Loading", why: "Still rows, no shimmer (the brand rule: nothing moves on its own). Each part of a page loads by itself.", page: loading },
    { h: "C2. A read that failed", why: "The part that failed says why and offers Try again. System agents below still loaded.", page: loadErr },
    { h: "C3. A missing key", why: "The page opens with the key field first and focused; it is the one thing to do here.", page: noKey },
    { h: "C4. Login states, and discovery in progress", why: "The login panel's four other states. Right: while Fyr asks the provider, the header says Checking… and the list stays usable.", raw: loginStates + frame("Phone · checking the models", phone(discBusy), "bd-phone") },
    { h: "C5. Discovery failed, and a model added by hand", why: "The last list still works. An endpoint with no models endpoint is run entirely with models added by hand; only those have ×.", page: discFail },
    { h: "C6. A provider with 342 models", why: "Search filters both parts. Off shows the first matches and Show more. Long ids wrap; nothing is cut.", page: manyModels },
    { h: "C7. Twelve providers", why: "The list is the page; the pane scrolls. A long name ends in an ellipsis. Rows that need you carry the amber badge, and the nav counts them.", page: manyProviders },
    { h: "C8. No providers", why: "The one time Add provider is the primary button.", page: emptyProviders },
    { h: "C9. Forty-three projects and four hosts", why: "Search shows from nine projects. A host that is offline says since when and why; one that is updating says the step.", page: manyProjects },
    { h: "C10. No projects; a refused change; a bad repo name", why: "", page: emptyAuto, phonePage: emptyProjects, deskCap: "Desktop · Automation: refused, empty, invalid", phoneCap: "Phone · no projects" },
    { h: "C11. No connection; several hosts and a job that cannot run", why: "Left: read-only with the reason at the top. Right: the Host picker appears with more than one host, and the job that cannot run is marked on its row.", page: offline, phonePage: sysMulti, deskCap: "Desktop · no connection", phoneCap: "Phone · System agents, three hosts" },
    { h: "C12. A project's edge cases, with the 44 px outlines", why: "An empty name, an image that is too large, a setup run that could not start, no folder, a default model that was turned off. The dashed lines mark every touch target on the phone, at 360 px wide.", page: projEdge, cls: "t44 w360" },
    { h: "C13. Notifications", why: "On (with Send test), blocked, and on an iPhone before Fyr is on the Home Screen.", raw: pushStates },
  ],
});
