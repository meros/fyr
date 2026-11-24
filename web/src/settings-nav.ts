// SPEC V0.119, V0.125, V0.157, V0.settings-ia: Settings is eight pages behind one nav, in two captioned groups. The
// pages, their one line of state, the one count of what needs the owner, and the page ids (`providers/<id>`,
// `hosts/<name>`, `hosts/<name>/<section>`). Pure.
import type { PushStatus } from "./push.ts";
import { updateTrouble, type HostInfo } from "./hosts.ts";
import { setupBadge, type SetupReport } from "./setup.ts";

export type PageId = "providers" | "automation" | "projects" | "hosts" | "usage" | "devices" | "device" | "setup";
export interface PageRef {
  page: PageId;
  /** a sub-page: a provider's id or "new" (providers), a project's id or "new" (projects), a host's name (hosts) */
  sub?: string;
  /** V0.127: the host of a provider's page (or Add provider) when the front has several; missing with one.
   *  V0.settings-ia: on Providers, Automation and Setup without a sub-page, the host tab the page opens on. */
  host?: string;
  /** V0.settings-ia: the section of a host's page a link lands on (HOST_SECTIONS, or "leftovers", the group inside
   *  Load): it opens and scrolls into view. Not a page: Esc and Back treat the host's page as the page. */
  focus?: string;
}

/** V0.settings-ia: the sections of a host's page, in the page's order: the ids of its links (`hosts/<host>/<id>`) and
 *  the words of its jump links. */
export const HOST_SECTIONS = [
  { id: "updates", title: "Updates" },
  { id: "load", title: "Load" },
  { id: "more", title: "Providers and setup" },
  { id: "diagnostics", title: "Diagnostics" },
  { id: "look", title: "Look" },
  { id: "about", title: "About" },
] as const;
export type HostSection = (typeof HOST_SECTIONS)[number]["id"];

/** A jump link at the top of a host's page: a section that is there, with a word only when it is not fine. `counted`:
 *  the word is something this host's tab counts, and shows in the count's colour; any other word is grey state. */
export interface HostJump {
  id: HostSection;
  title: string;
  word?: string;
  counted?: true;
}

/** The jump links of a host's page: the sections it shows, in order, each with its word ("Updates · held back").
 *  `counted` names the sections whose word, when there is one, is counted for this host. */
export function hostJumps(shown: Partial<Record<HostSection, boolean>>, words: Partial<Record<HostSection, string>>, counted: Partial<Record<HostSection, boolean>> = {}): HostJump[] {
  return HOST_SECTIONS.filter((x) => shown[x.id]).map((x) => ({ id: x.id, title: x.title, ...(words[x.id] ? { word: words[x.id], ...(counted[x.id] ? { counted: true as const } : {}) } : {}) }));
}

/** V0.settings-ia: the nav's two groups, the same on the desktop and the phone, sorted by where a setting is kept, so
 *  each caption is true of every page under it. "Only on this device": kept in this browser, each device has its own.
 *  "On your hosts": kept by a host, the same from every device. Sign-in is the second kind (the devices signed in to
 *  the host this device is connected to), so it is first there. The page id `devices` is kept for its links. */
export const NAV_GROUPS: { caption: string; pages: { id: PageId; title: string }[] }[] = [
  {
    caption: "Only on this device",
    pages: [{ id: "device", title: "This device" }],
  },
  {
    caption: "On your hosts",
    pages: [
      { id: "devices", title: "Sign-in" }, // V0.157
      { id: "providers", title: "Providers" },
      { id: "projects", title: "Projects" },
      { id: "automation", title: "Automation" },
      { id: "hosts", title: "Hosts" },
      { id: "usage", title: "Usage" }, // V0.usage-page
      { id: "setup", title: "Setup" },
    ],
  },
];

export const PAGES: { id: PageId; title: string }[] = NAV_GROUPS.flatMap((g) => g.pages);

/** The pages with sub-pages. */
const SUBS: Record<PageId, boolean> = { providers: true, automation: false, projects: true, hosts: true, usage: false, devices: false, device: false, setup: false };

function isPage(s: string | null | undefined): s is PageId {
  return !!s && PAGES.some((p) => p.id === s);
}

const dec = (s: string) => {
  try {
    return decodeURIComponent(s); // a host's name is URL-encoded
  } catch {
    return s;
  }
};

/** The pages whose host tab a link may name without a sub-page: "setup/lab", "automation/lab". */
const TABBED: Partial<Record<PageId, boolean>> = { automation: true, setup: true };
/** What a link from before V0.settings-ia named after "automation/": a section that is on the host's page now. */
const WAS_ON_AUTOMATION = new Set(["leftovers", "updates", "load", "diagnostics"]);

/** "providers/abc" → {page, sub}; "providers/lab/abc" → {page, host, sub} (V0.127); "hosts/lab/leftovers" → {page, sub,
 *  focus}, "setup/lab" and "automation/lab" → {page, host} (V0.settings-ia); an unknown page is providers. Older links
 *  still land: "automation/lab/leftovers" is hosts/lab/leftovers; "automation/leftovers" (a front with one host,
 *  which had no name in the link) is the Hosts page with the focus, which the shell opens on its first host; and
 *  "hosts/lab/setup" is Setup on lab's tab (the checks are on the Setup page only). */
export function parsePage(s: string | null | undefined): PageRef {
  const raw = (s ?? "").trim();
  const i = raw.indexOf("/");
  const page = i < 0 ? raw : raw.slice(0, i);
  const sub = i < 0 ? "" : raw.slice(i + 1);
  if (!isPage(page)) return { page: "providers" };
  const j = sub.indexOf("/");
  const two = j > 0 && j < sub.length - 1;
  if (page === "automation") {
    if (two) return { page: "hosts", sub: dec(sub.slice(0, j)), focus: dec(sub.slice(j + 1)) };
    if (sub && WAS_ON_AUTOMATION.has(dec(sub))) return { page: "hosts", focus: dec(sub) };
  }
  if (page === "providers" && two) return { page, host: dec(sub.slice(0, j)), sub: dec(sub.slice(j + 1)) };
  if (page === "hosts" && two) {
    const host = dec(sub.slice(0, j));
    const focus = dec(sub.slice(j + 1));
    return focus === "setup" ? { page: "setup", host } : { page, sub: host, focus };
  }
  const one = dec(sub);
  if (one && TABBED[page]) return { page, host: one };
  return one && SUBS[page] ? { page, sub: one } : { page };
}

export function pageUrl(r: PageRef): string {
  if (!r.sub) return r.host && TABBED[r.page] ? `${r.page}/${encodeURIComponent(r.host)}` : r.page;
  if (r.page === "hosts" && r.focus) return `${r.page}/${encodeURIComponent(r.sub)}/${encodeURIComponent(r.focus)}`;
  return r.host ? `${r.page}/${encodeURIComponent(r.host)}/${encodeURIComponent(r.sub)}` : `${r.page}/${encodeURIComponent(r.sub)}`;
}

type Status = { status: "ready" | "needs-login" | "needs-key" };

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** "1 needs you", "3 need you"; "" for none. */
export function needWords(n: number): string {
  return n <= 0 ? "" : n === 1 ? "1 needs you" : `${n} need you`;
}

/** What needs the owner on the Providers page: "1 needs a login", "2 need you", or "". blocked: Fyr's own calls that
 *  cannot run. */
export function providersNeed(list: Status[] | null, blocked: number): string {
  if (!list) return "";
  const login = list.filter((p) => p.status === "needs-login").length;
  const key = list.filter((p) => p.status === "needs-key").length;
  const n = login + key + blocked;
  if (n === 1 && login === 1) return "1 needs a login";
  if (n === 1 && key === 1) return "1 needs a key";
  return needWords(n);
}

/** V0.settings-ia: a nav line whose read failed says so (an empty line would mean "nothing to say"). */
export const COULD_NOT_READ = "Could not read";

/** "3 providers", "3 providers · 1 needs a login"; "" until the list is read; "Could not read" when the read failed. */
export function providersSummary(list: Status[] | null, blocked: number, failed = false): string {
  if (!list) return failed ? COULD_NOT_READ : "";
  if (!list.length) return "No providers";
  const need = providersNeed(list, blocked);
  const n = plural(list.length, "provider", "providers");
  return need ? `${n} · ${need}` : n;
}

/** "10 projects", "No projects" (V0.125: the hosts have their own page). */
export function projectsSummary(projects: number): string {
  return projects ? plural(projects, "project", "projects") : "No projects";
}

/** V0.125: "3 hosts", "3 hosts · 1 offline", "3 hosts · 1 needs you"; one host is "One host" (decision 0031 2.6: the same
 *  through every front), "One host · 1 needs you" when it holds a release back. `need` is the Hosts badge
 *  (settingsCounts). */
export function hostsSummary(hosts: Pick<HostInfo, "online">[] | undefined, need: number): string {
  const hs = hosts ?? [];
  if (hs.length <= 1) return need ? `One host · ${needWords(need)}` : "One host";
  const off = hs.filter((h) => !h.online).length;
  const tail = need ? ` · ${needWords(need)}` : off ? ` · ${off} offline` : "";
  return `${plural(hs.length, "host", "hosts")}${tail}`;
}

// ---- V0.settings-ia: the one count of what needs the owner ----

/** What one host holds that waits for the owner. `key` is the host's name ("" for the one part of a front with one
 *  host, as the per-host pages key their parts). */
export interface HostNeeds {
  key: string;
  /** the host can be reached now. While it cannot, nothing on it can be pressed, so what it holds does not count
   *  (its changed id still does: Trust goes to the front, not to the host). */
  online: boolean;
  /** its providers; null while unread (they count nothing) */
  providers: Status[] | null;
  /** Fyr's own calls that cannot run on it */
  blocked: number;
  /** another host answers under its name */
  idChanged: boolean;
  /** it holds a release back, or its last update failed and it is still behind (hosts.ts updateTrouble) */
  update: boolean;
  /** required setup steps that are not ready, once Settings has read them */
  setup: number;
}

/** One host's needs from what was read of it: its part (settingsHosts), its row in the host list, its providers and
 *  Fyr's own calls that cannot run, its setup report, and its updates view. A host with no install id cannot be asked
 *  or pressed on: it counts as offline. */
export function hostNeeds(
  part: { name: string; online: boolean; noId?: true },
  row: Pick<HostInfo, "idWas" | "update"> | undefined,
  a: { providers: HostNeeds["providers"]; blocked: number; setup: SetupReport | null; view?: { bad?: { commit: string; why: string } } | null },
): HostNeeds {
  return { key: part.name, online: part.online && !part.noId, providers: a.providers, blocked: a.blocked, idChanged: !!row?.idWas, update: !!updateTrouble(row, a.view), setup: setupBadge(a.setup) };
}

type Counted = "providers" | "hosts" | "setup" | "devices";
export interface SettingsCounts {
  /** the nav's badges */
  pages: Record<Counted, number>;
  /** the host tabs' badges on Providers, a host's page and Setup, by HostNeeds.key */
  tabs: Record<"providers" | "hosts" | "setup", Record<string, number>>;
  /** the gear's and the More row's count: the sum of the nav's badges */
  total: number;
}

/** The one count (V0.settings-ia): the desktop gear, the phone's More row, the nav's badges and the host tabs all read
 *  this, so a number on the way in equals the numbers on the way through.
 *  THE RULE: a thing counts only if the owner can act on it now, and every counted thing has an amber chip (`.pst
 *  .needs-login`) on the row where it is fixed; nothing has that chip without being counted. Counted: a provider
 *  without its login or key, one of Fyr's own calls that cannot run, a host whose id changed, a release a host holds
 *  back or an update that failed, a device waiting to be allowed, a required setup step. Not counted: anything on a
 *  host that is offline (no press reaches it; its rows show grey state), a host too old to be read (its update is a
 *  command on that host), what Fyr handles by itself (leftover processes, undeclared work, a low GitHub limit), which
 *  is state on the host's page, and everything while this device has no connection (`offline`: no press reaches
 *  anything; the chips are grey, settings.css `.set.ro`). */
export function settingsCounts(hosts: HostNeeds[], devicesWaiting: number, offline = false): SettingsCounts {
  const tabs: SettingsCounts["tabs"] = { providers: {}, hosts: {}, setup: {} };
  for (const h of hosts) {
    tabs.providers[h.key] = h.online && !offline ? (h.providers?.filter((p) => p.status !== "ready").length ?? 0) + h.blocked : 0;
    tabs.hosts[h.key] = offline ? 0 : (h.idChanged ? 1 : 0) + (h.online && h.update ? 1 : 0);
    tabs.setup[h.key] = h.online && !offline ? h.setup : 0;
  }
  const sum = (m: Record<string, number>) => Object.values(m).reduce((a, b) => a + b, 0);
  const pages = { providers: sum(tabs.providers), hosts: sum(tabs.hosts), setup: sum(tabs.setup), devices: offline ? 0 : Math.max(0, devicesWaiting) };
  return { pages, tabs, total: pages.providers + pages.hosts + pages.setup + pages.devices };
}

/** The pages that hold what is counted, by their nav titles and in the nav's order: "Sign-in 1 · Providers 2"; "" for
 *  none. The gear's tooltip and the More row's line show it, so the owner knows where to go before opening Settings. */
export function needLine(c: SettingsCounts): string {
  return PAGES.flatMap((p) => {
    const n = (c.pages as Partial<Record<PageId, number>>)[p.id] ?? 0;
    return n > 0 ? [`${p.title} ${n}`] : [];
  }).join(" · ");
}

/** What Settings holds, for the gear's tooltip and the More row while nothing needs the owner: the nav's titles. */
export const SETTINGS_HOLDS = PAGES.map((p, i) => (i ? p.title.toLowerCase() : p.title)).join(", ");

const PUSH_WORD: Record<PushStatus, string> = { on: "Notifications on", off: "Notifications off", denied: "Notifications blocked", unsupported: "No notifications here" };

/** "Notifications off · theme auto" */
export function deviceSummary(push: PushStatus, theme: "system" | "light" | "dark", hidden = 0): string {
  return `${PUSH_WORD[push]} · theme ${theme === "system" ? "auto" : theme}${hidden ? ` · ${hidden} hidden` : ""}`;
}

/** Esc in the desktop dialog (outside a changed text field): a sub-page goes to its list (New project and Add provider
 *  too: V0.settings-ia); a page closes Settings (null). */
export function escapeTarget(r: PageRef): PageRef | null {
  return r.sub ? { page: r.page } : null;
}
