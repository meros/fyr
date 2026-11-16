// SPEC V0.119: Settings is four pages behind one nav. The pages, their one line of state, the badge and the page ids
// (`providers/<id>`). Pure.
import type { AutoMergeRepo } from "./automerge.ts";
import type { PushStatus } from "./push.ts";

export type PageId = "providers" | "automation" | "projects" | "device";
export interface PageRef {
  page: PageId;
  /** a sub-page: a provider's id or "new" (providers), a project's id (projects) */
  sub?: string;
}

export const PAGES: { id: PageId; title: string }[] = [
  { id: "providers", title: "Providers" },
  { id: "automation", title: "Automation" },
  { id: "projects", title: "Projects" },
  { id: "device", title: "This device" },
];

/** The pages with sub-pages. */
const SUBS: Record<PageId, boolean> = { providers: true, automation: false, projects: true, device: false };

export function isPage(s: string | null | undefined): s is PageId {
  return !!s && PAGES.some((p) => p.id === s);
}

/** "providers/abc" → {page, sub}; an unknown page is providers. */
export function parsePage(s: string | null | undefined): PageRef {
  const raw = (s ?? "").trim();
  const i = raw.indexOf("/");
  const page = i < 0 ? raw : raw.slice(0, i);
  const sub = i < 0 ? "" : raw.slice(i + 1);
  if (!isPage(page)) return { page: "providers" };
  return sub && SUBS[page] ? { page, sub } : { page };
}

export function pageUrl(r: PageRef): string {
  return r.sub ? `${r.page}/${r.sub}` : r.page;
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
  return `Deploys ${deploy ? "on" : "off"} · ${on ? `merges in ${plural(on, "repo", "repos")}` : "merges off"}`;
}

/** "10 projects · 1 host"; no hosts listed is this host. */
export function projectsSummary(projects: number, hosts: number): string {
  return `${projects ? plural(projects, "project", "projects") : "No projects"} · ${plural(Math.max(1, hosts), "host", "hosts")}`;
}

const PUSH_WORD: Record<PushStatus, string> = { on: "Notifications on", off: "Notifications off", denied: "Notifications blocked", unsupported: "No notifications here" };

/** "Notifications off · theme auto" */
export function deviceSummary(push: PushStatus, theme: "system" | "light" | "dark"): string {
  return `${PUSH_WORD[push]} · theme ${theme === "system" ? "auto" : theme}`;
}
