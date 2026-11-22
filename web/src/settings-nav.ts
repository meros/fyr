// SPEC V0.119, V0.125, V0.157: Settings is six pages behind one nav. The pages, their one line of state, the badges and the
// page ids (`providers/<id>`, `hosts/<name>`). Pure.
import type { AutoMergeRepo } from "./automerge.ts";
import type { PushStatus } from "./push.ts";
import type { HostInfo } from "./hosts.ts";

export type PageId = "providers" | "automation" | "projects" | "hosts" | "devices" | "device" | "setup";
export interface PageRef {
  page: PageId;
  /** a sub-page: a provider's id or "new" (providers), a project's id (projects), a host's name (hosts) */
  sub?: string;
  /** V0.127: the host of a provider's page (or Add provider) when the front has several; missing with one */
  host?: string;
}

export const PAGES: { id: PageId; title: string }[] = [
  { id: "providers", title: "Providers" },
  { id: "automation", title: "Automation" },
  { id: "projects", title: "Projects" },
  { id: "hosts", title: "Hosts" },
  { id: "devices", title: "Devices" },
  { id: "device", title: "This device" },
  { id: "setup", title: "Setup" },
];

/** The pages with sub-pages. */
/** Automation's sub is not a page: it names the group to scroll to ("leftovers"). */
const SUBS: Record<PageId, boolean> = { providers: true, automation: true, projects: true, hosts: true, devices: false, device: false, setup: false };

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

/** "providers/abc" → {page, sub}; "providers/lab/abc" or "automation/lab/leftovers" → {page, host, sub} (V0.127, V0.host-tabs); an unknown page is providers. */
export function parsePage(s: string | null | undefined): PageRef {
  const raw = (s ?? "").trim();
  const i = raw.indexOf("/");
  const page = i < 0 ? raw : raw.slice(0, i);
  const sub = i < 0 ? "" : raw.slice(i + 1);
  if (!isPage(page)) return { page: "providers" };
  const j = sub.indexOf("/");
  if ((page === "providers" || page === "automation") && j > 0 && j < sub.length - 1) return { page, host: dec(sub.slice(0, j)), sub: dec(sub.slice(j + 1)) };
  const one = dec(sub);
  return one && SUBS[page] ? { page, sub: one } : { page };
}

export function pageUrl(r: PageRef): string {
  if (!r.sub) return r.page;
  return r.host ? `${r.page}/${encodeURIComponent(r.host)}/${encodeURIComponent(r.sub)}` : `${r.page}/${encodeURIComponent(r.sub)}`;
}

type Status = { status: "ready" | "needs-login" | "needs-key" };

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`;

/** What needs the owner on the Providers page: "1 needs a login", "2 need you", or "". blocked: system jobs that cannot run. */
export function providersNeed(list: Status[] | null, blocked: number): string {
  if (!list) return "";
  const login = list.filter((p) => p.status === "needs-login").length;
  const key = list.filter((p) => p.status === "needs-key").length;
  const n = login + key + blocked;
  if (n === 0) return "";
  if (n === 1 && login === 1) return "1 needs a login";
  if (n === 1 && key === 1) return "1 needs a key";
  return n === 1 ? "1 needs you" : `${n} need you`;
}

/** The amber count beside Providers: providers that are not ready and system jobs that cannot run. */
export function navBadge(list: Status[] | null, blocked: number): number {
  if (!list) return 0;
  return list.filter((p) => p.status !== "ready").length + blocked;
}

/** "3 providers", "3 · 1 needs a login"; "" until the list is read. */
export function providersSummary(list: Status[] | null, blocked: number): string {
  if (!list) return "";
  if (!list.length) return "No providers";
  const need = providersNeed(list, blocked);
  return need ? `${list.length} · ${need}` : plural(list.length, "provider", "providers");
}

/** "Deploys on · merges in 2 repos"; "" until both are read. */
export function automationSummary(deploy: boolean | null, repos: AutoMergeRepo[] | null): string {
  if (deploy === null || repos === null) return "";
  const on = repos.filter((r) => r.on).length;
  return `Updates ${deploy ? "on" : "off"} · ${on ? `merges in ${plural(on, "repo", "repos")}` : "merges off"}`;
}

/** "10 projects", "No projects" (V0.125: the hosts have their own page). */
export function projectsSummary(projects: number): string {
  return projects ? plural(projects, "project", "projects") : "No projects";
}

/** V0.125: the hosts that need the owner: another install answers under the name (V0.124). */
export function hostsNeed(hosts: Pick<HostInfo, "online" | "idWas">[] | undefined): number {
  return (hosts ?? []).filter((h) => h.idWas).length;
}

/** V0.125: "3 hosts", "3 hosts · 1 offline", "3 hosts · 1 needs you"; one host is "One host" (decision 0031 2.6: the same through every front). */
export function hostsSummary(hosts: Pick<HostInfo, "online" | "idWas">[] | undefined): string {
  const hs = hosts ?? [];
  if (hs.length <= 1) return "One host";
  const need = hostsNeed(hs);
  const off = hs.filter((h) => !h.online).length;
  const tail = need ? ` · ${need === 1 ? "1 needs you" : `${need} need you`}` : off ? ` · ${off} offline` : "";
  return `${plural(hs.length, "host", "hosts")}${tail}`;
}

/** V0.125: the More sheet's Settings line: the providers' words (providersNeed), with the hosts that need the owner
 *  added to the count. */
export function settingsNeed(providers: string, hosts: number): string {
  if (!hosts) return providers;
  const n = (Number.parseInt(providers, 10) || 0) + hosts;
  return n === 1 ? "1 needs you" : `${n} need you`;
}

const PUSH_WORD: Record<PushStatus, string> = { on: "Notifications on", off: "Notifications off", denied: "Notifications blocked", unsupported: "No notifications here" };

/** "Notifications off · theme auto" */
export function deviceSummary(push: PushStatus, theme: "system" | "light" | "dark", hidden = 0): string {
  return `${PUSH_WORD[push]} · theme ${theme === "system" ? "auto" : theme}${hidden ? ` · ${hidden} hidden` : ""}`;
}

/** Esc in the desktop dialog (outside a changed text field): a sub-page goes to its list; a page closes Settings (null). */
export function escapeTarget(r: PageRef): PageRef | null {
  return r.sub && r.page !== "automation" ? { page: r.page } : null;
}
