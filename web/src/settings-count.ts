// V0.settings-ia: the one count outside Settings (the desktop's gear, the phone's More row) and the things Settings has
// read that the count needs later. The rule is settings-nav.ts settingsCounts; this file gathers its inputs, the same
// way for the door (readDoor) and for Settings' pages (countsFrom over what they hold).
//
// What it costs: outside Settings the count reads each online host's providers and Fyr's own calls, the devices that
// wait, and, for a host whose row carries no update summary, what it holds of its updates: cheap reads of what the
// host already has. It never runs a host's setup checks (each read of them runs the checks live on that machine): a
// required setup step counts once Settings has read it, from the report kept here. When it is read: when the hosts'
// rows or the state's provider status change, when the connection comes back, when Settings closes, when the window
// gets the focus again, and every five minutes; a count younger than 30 s for the same state is reused (the More row
// mounts on every sheet open). While Settings is open it publishes what its pages show.
import { useEffect, useState } from "preact/hooks";
import type { Api } from "./api.ts";
import { devicesNeed } from "./devices.ts";
import { updateTrouble, type HostInfo, type HostProfiles } from "./hosts.ts";
import { settingsHosts, type SettingsHost } from "./host-settings.ts";
import { hostNeeds, settingsCounts, type HostNeeds, type SettingsCounts } from "./settings-nav.ts";
import { setupBadge, type SetupReport } from "./setup.ts";

export type CountApi = Pick<Api, "providers"> & Partial<Pick<Api, "systemAgents" | "authState" | "updates">>;

// ---- the count from what was read ----

/** What was read of one host: its part (settingsHosts), its providers (null while unread, or when the read failed),
 *  Fyr's own calls that cannot run on it, and its updates view (null while unread). */
export interface HostRead {
  host: SettingsHost;
  providers: HostNeeds["providers"];
  blocked: number;
  view: { bad?: { commit: string; why: string } } | null;
}

/** The one count from what was read of every host. The door and Settings' pages both go through here, so they can
 *  differ only in what they read, and test/settings-count.test.ts holds them equal on that. */
export function countsFrom(hosts: HostInfo[] | undefined, reads: HostRead[], setup: Record<string, SetupReport>, devicesWaiting: number, offline: boolean): SettingsCounts {
  const rows = hosts ?? [];
  // a front with one host has one part without a name: its row is the only one
  const rowOf = (name: string) => (reads.length > 1 ? rows.find((h) => h.name === name) : rows[0]);
  return settingsCounts(
    reads.map((r) => hostNeeds(r.host, rowOf(r.host.name), { providers: r.providers, blocked: r.blocked, setup: (r.host.id && setup[r.host.id]) || null, view: r.view })),
    devicesWaiting,
    offline,
  );
}

/** The door's reads, and the count from them. An offline host counts nothing and one with no install id cannot be
 *  asked: neither is read. A host whose row carries an update summary is not asked for its updates (the summary is
 *  live, and wins: hosts.ts updateTrouble). With no connection nothing is read. */
export async function readDoor(api: CountApi, hosts: HostInfo[] | undefined, setup: Record<string, SetupReport>, offline: boolean): Promise<SettingsCounts> {
  const parts = settingsHosts(hosts);
  const none = (host: SettingsHost): HostRead => ({ host, providers: null, blocked: 0, view: null });
  if (offline) return countsFrom(hosts, parts.map(none), setup, 0, true);
  const [reads, auth] = await Promise.all([
    Promise.all(
      parts.map(async (h): Promise<HostRead> => {
        if (!h.id || !h.online) return none(h);
        const row = parts.length > 1 ? hosts?.find((x) => x.name === h.name) : hosts?.[0];
        const [l, sa, up] = await Promise.all([api.providers(h.id), api.systemAgents ? api.systemAgents(h.id) : null, !row?.update && api.updates ? api.updates(h.id) : null]);
        return { host: h, providers: l.ok ? l.data : null, blocked: sa?.ok ? sa.data.jobs.filter((j) => j.error).length : 0, view: up?.ok ? up.data : null };
      }),
    ),
    api.authState ? api.authState() : null,
  ]);
  return countsFrom(hosts, reads, setup, auth?.ok ? devicesNeed(auth.data) : 0, false);
}

// ---- what Settings has read ----

/** By install id: each host's setup report with when it was read. */
interface Known {
  setup: Record<string, SetupReport>;
  setupAt: Record<string, number>;
}
let known: Known = { setup: {}, setupAt: {} };
const knownSubs = new Set<() => void>();
const tell = (subs: Set<() => void>) => {
  for (const f of [...subs]) f();
};

/** A host's setup report, as the Setup page or the Settings shell just read it. */
export function noteSetup(id: string, r: SetupReport): void {
  known = { ...known, setup: { ...known.setup, [id]: r }, setupAt: { ...known.setupAt, [id]: Date.now() } };
  tell(knownSubs);
}

/** How old a host's setup report is, in ms; Infinity when it was never read. */
export function setupAge(id: string): number {
  return id in known.setupAt ? Date.now() - known.setupAt[id] : Infinity;
}

export function useKnown(): Known {
  const [, bump] = useState(0);
  useEffect(() => {
    const f = () => bump((n) => n + 1);
    knownSubs.add(f);
    return () => void knownSubs.delete(f);
  }, []);
  return known;
}

// ---- the count ----

const FRESH_MS = 30_000;
const SLOW_MS = 5 * 60_000;
let last: { c: SettingsCounts; key: string; at: number } | null = null;
let settingsOpen = 0;
const countSubs = new Set<() => void>();
const againSubs = new Set<() => void>();

function publish(c: SettingsCounts, key: string): void {
  const same = last && JSON.stringify(last.c) === JSON.stringify(c);
  last = { c, key, at: Date.now() };
  if (!same) tell(countSubs);
}

/** Settings shows these numbers now (its nav's badges): the entry points show the same. */
export function publishCounts(c: SettingsCounts): void {
  publish(c, "");
}

/** Settings is open; the returned function says it closed, and the entry points read again. */
export function settingsOpened(): () => void {
  settingsOpen++;
  return () => {
    settingsOpen--;
    if (last) last = { ...last, at: 0 };
    tell(againSubs);
  };
}

/** The state's provider status as one string: a login that expires changes it, and the count is read again. */
export function profilesKey(byHost: HostProfiles): string {
  return Object.keys(byHost)
    .sort()
    .map((id) => `${id}:${byHost[id].map((p) => `${p.id}${p.unavailable ? "!" : ""}`).join(",")}`)
    .join(" ");
}

/** The one count for the gear and the More row; null while unknown. `hosts` is every host Settings lists (the ones
 *  this device hides too: Settings shows them, and the number at the door is the number inside). `offline`: no
 *  connection, so nothing can be pressed and nothing counts. */
export function useSettingsCounts(api: CountApi, hosts: HostInfo[] | undefined, profiles: string, offline: boolean): SettingsCounts | null {
  const [, bump] = useState(0);
  const [again, setAgain] = useState(0);
  const k = useKnown();
  useEffect(() => {
    const show = () => bump((n) => n + 1);
    const read = () => setAgain((n) => n + 1);
    // coming back to the tab fires both visibilitychange and focus: one read for the two
    let asked = 0;
    const seen = () => {
      if (document.visibilityState !== "visible" || Date.now() - asked < 1000) return;
      asked = Date.now();
      read();
    };
    countSubs.add(show);
    againSubs.add(read);
    document.addEventListener("visibilitychange", seen);
    window.addEventListener("focus", seen);
    const t = setInterval(seen, SLOW_MS);
    return () => {
      countSubs.delete(show);
      againSubs.delete(read);
      document.removeEventListener("visibilitychange", seen);
      window.removeEventListener("focus", seen);
      clearInterval(t);
    };
  }, []);
  const parts = settingsHosts(hosts);
  const key = [
    offline ? "off" : "on",
    ...parts.map((h) => {
      const r = parts.length > 1 ? hosts?.find((x) => x.name === h.name) : hosts?.[0];
      return `${h.id ?? ""}:${h.online ? 1 : 0}:${r?.idWas ? 1 : 0}:${r?.update ? (updateTrouble(r) ? 1 : 0) : ""}:${setupBadge((h.id && k.setup[h.id]) || null)}`;
    }),
    profiles,
  ].join(" ");
  /* eslint-disable react-hooks/exhaustive-deps -- reads when what the count depends on changes (the key) or a re-read is due (again); api is fixed for the app's life */
  useEffect(() => {
    if (settingsOpen) return; // Settings publishes what its pages show
    if (last && last.key === key && Date.now() - last.at < FRESH_MS) return;
    let live = true;
    void readDoor(api, hosts, k.setup, offline).then((c) => live && !settingsOpen && publish(c, key));
    return () => void (live = false);
  }, [key, again]);
  /* eslint-enable react-hooks/exhaustive-deps */
  return last?.c ?? null;
}
