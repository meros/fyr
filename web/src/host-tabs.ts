// V0.host-tabs: pure helpers for the shared tabbed host switcher across multi-host screens (Settings → Providers,
// Automation, Setup, Hosts, linked projects, the phone Host load sheet, and HostField pickers). Pure.

import type { HostInfo } from "./hosts.ts";
import type { Quota } from "./quota.ts";
import type { Project } from "./types.ts";

export interface HostTabItem {
  /** stable key for selection (host name, host install id, or project member id) */
  id: string;
  /** host name, for HostMark and data-host */
  name: string;
  /** visible mono label */
  label: string;
  /** offline or older host without install id */
  offline?: boolean;
  /** amber count of items needing attention on this host */
  badge?: number;
  /** optional compact mono reading beside the name (e.g. load % on the load sheet) */
  meta?: string;
  /** colour accent for meta: calm (muted), warm (beam), hot (signal) */
  metaLevel?: "calm" | "warm" | "hot";
  /** tooltip text */
  title?: string;
}

/** The active tab id: `picked` when it is still in `items`, else the first item (owner's host order). */
export function activeTabId(items: readonly Pick<HostTabItem, "id">[], picked: string | null | undefined): string {
  if (picked && items.some((x) => x.id === picked)) return picked;
  return items[0]?.id ?? "";
}

/** Keyboard movement inside a horizontal tablist: ArrowLeft/ArrowRight wrap, Home/End jump to the ends. */
export function stepTabIndex(len: number, cur: number, key: string): number | null {
  if (len <= 1) return null;
  if (key === "ArrowRight") return (cur + 1) % len;
  if (key === "ArrowLeft") return (cur - 1 + len) % len;
  if (key === "Home") return 0;
  if (key === "End") return len - 1;
  return null;
}

/** Order a linked project's members in the owner's host order (as `hosts` lists them), so tabs stay in the same order as
 *  every other multi-host screen and never jump when a host goes offline. */
export function orderedMembers(members: readonly Project[], hostList: readonly Pick<HostInfo, "id">[]): Project[] {
  const rank = new Map<string, number>();
  hostList.forEach((h, i) => {
    if (h.id) rank.set(h.id, i);
  });
  return [...members].sort((a, b) => (a.hostId ? (rank.get(a.hostId) ?? hostList.length) : hostList.length) - (b.hostId ? (rank.get(b.hostId) ?? hostList.length) : hostList.length));
}

/** Filter quotas to one host when `host` is named (keeps unstamped quotas from older fronts); return all with one host. */
export function quotasForHost(list: readonly Quota[], host: string | undefined): Quota[] {
  if (!host) return [...list];
  return list.filter((q) => !q.host || q.host === host);
}
