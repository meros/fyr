// The data every Settings page and the nav read (V0.127): each part of each host loads by itself, on open and when the
// host comes back online. A hook; the pages and the shell are in settings-ui.tsx.
import { useEffect, useRef, useState } from "preact/hooks";
import type { HostId } from "./hostid.ts";
import { settingsHosts, type SettingsHost } from "./host-settings.ts";
import { type AutoMergeRepo } from "./automerge.ts";
import { type UpdatesView } from "./updates.ts";
import { type SystemAgentsView } from "./system-agents.ts";
import { type PageRef } from "./settings-nav.ts";
import {
  loginActive,
  newProviderId,
  type FoundDTO,
  type ProviderDTO,
} from "./providers.ts";
import { type SettingsProps } from "./settings-ui.tsx";
import { failed, type Result } from "./api.ts";

export type Run = (action: string, args: Record<string, unknown>) => Promise<string | null>;

/** V0.127: what one host's per-host pages read. */
interface HostSlice {
  providers: ProviderDTO[] | null;
  provErr: string | null;
  found: FoundDTO[];
  deploy: boolean | null;
  deployErr: string | null;
  /** V0.126 (#151): the host's update record; null when it has none */
  updates: UpdatesView | null;
  repos: AutoMergeRepo[] | null;
  reposErr: string | null;
  sa: SystemAgentsView | null;
  saErr: string | null;
  /** the host is offline and this is what the front last read, read then (X-Fyr-Read-At); null when it is fresh */
  readAt: string | null;
}
const EMPTY_SLICE: HostSlice = { providers: null, provErr: null, found: [], deploy: null, deployErr: null, updates: null, repos: null, reposErr: null, sa: null, saErr: null, readAt: null };

/** V0.127: the settings of every host the front follows. Each host's reads and commands name it by its install id
 *  (decision 0031); one host is one part without a heading. */
export function useSettingsData(p: SettingsProps, saved: () => void) {
  const api = p.api;
  const hosts = settingsHosts(p.hosts);
  /** The install id of a part (keyed by its host's name): a host with none cannot be named, and nothing is sent. */
  const hid = (name: string): HostId => {
    const id = hosts.find((h) => h.name === name)?.id;
    if (!id) throw new Error("this host reports no id");
    return id;
  };
  const [slices, setSlices] = useState<Record<string, HostSlice>>({});
  const patch = (host: string, part: Partial<HostSlice>) => setSlices((s) => ({ ...s, [host]: { ...(s[host] ?? EMPTY_SLICE), ...part } }));
  const seen = (host: string, r: { readAt?: string }) => patch(host, { readAt: r.readAt ?? null });
  /** One host's read; a part with no install id answers as a read that failed, and nothing is sent. */
  const readOf = <T,>(host: string, read: (id: HostId) => Promise<Result<T>>): Promise<Result<T>> => {
    let id: HostId;
    try {
      id = hid(host);
    } catch (e) {
      return Promise.resolve(failed((e as Error).message));
    }
    return read(id);
  };

  const loadProviders = async (host: string) => {
    const [l, f] = await Promise.all([readOf(host, (id) => api.providers(id)), readOf(host, (id) => api.providersFound(id))]);
    if (!l.ok) return patch(host, { provErr: `Could not read the providers: ${l.error}.` });
    patch(host, { providers: l.data, found: f.ok ? f.data : [], provErr: null, readAt: l.readAt ?? null });
  };
  const loadDeploy = async (host: string) => {
    const read = api.autoDeploy?.bind(api);
    if (!read) return patch(host, { deploy: null });
    const r = await readOf(host, read);
    if (!r.ok) return patch(host, { deployErr: `Could not read the auto-update setting: ${r.error}.` });
    patch(host, { deploy: r.data.on, deployErr: null });
    seen(host, r);
  };
  // V0.126 (#151): the update record; a host without one shows nothing.
  const loadUpdates = async (host: string) => {
    const read = api.updates?.bind(api);
    if (!read) return patch(host, { updates: null });
    const r = await readOf(host, read);
    // A host that never updated itself (a NixOS host) sends "updates": null from an older fyrd: an empty list.
    patch(host, { updates: r.ok ? { ...r.data, updates: r.data.updates ?? [] } : null });
  };
  const loadRepos = async (host: string) => {
    const read = api.autoMerge?.bind(api);
    if (!read) return patch(host, { repos: [] });
    const r = await readOf(host, read);
    if (!r.ok) return patch(host, { reposErr: `Could not read the auto-merge setting: ${r.error}.` });
    patch(host, { repos: r.data, reposErr: null });
    seen(host, r);
  };
  const loadSa = async (host: string) => {
    const read = api.systemAgents?.bind(api);
    if (!read) return;
    const r = await readOf(host, read);
    if (!r.ok) return patch(host, { saErr: `Could not read Fyr's own calls: ${r.error}.` });
    patch(host, { sa: r.data, saErr: null });
    seen(host, r);
  };
  const loadAll = (host: string) => {
    void loadProviders(host);
    void loadDeploy(host);
    void loadUpdates(host);
    void loadRepos(host);
    void loadSa(host);
  };
  // Every host once, and a host again when it comes back online (what showed was last read).
  // The id is in the key: Settings opened before the front's host list arrived has one part with no id yet, and must
  // load once the id comes, not stay on "no install id" (V0.client-per-host).
  const online = hosts.map((h) => `${h.name}:${h.id ?? ""}:${h.online ? 1 : 0}`).join(" ");
  const was = useRef<Record<string, boolean>>({});
  /* eslint-disable react-hooks/exhaustive-deps -- loads a host when it is new or comes back online (was keeps each one's last state); hosts and loadAll are read at that moment */
  useEffect(() => {
    for (const h of hosts) {
      if (!h.id) continue; // V0.host-ids: no call can name it (noId), or the host list has not arrived yet
      if (!(h.name in was.current) || (h.online && !was.current[h.name])) loadAll(h.name);
      was.current[h.name] = h.online;
    }
  }, [online]);
  /* eslint-enable react-hooks/exhaustive-deps */
  // V0.53: while a login runs on a host, follow it there (its link, the code's answer, the end).
  // V0.provider-mcp: likewise while a provider lists its MCP servers or signs in to one.
  const logging = hosts.filter((h) => slices[h.name]?.providers?.some((x) => loginActive(x.login) || !!x.mcp?.listing || loginActive(x.mcp?.login))).map((h) => h.name);
  /* eslint-disable react-hooks/exhaustive-deps -- the poll restarts only when the set of hosts with a login running changes (its JSON key); loadProviders is new each render and p.onChanged is read when the poll ends */
  useEffect(() => {
    if (!logging.length) return;
    const t = setInterval(() => logging.forEach((h) => void loadProviders(h)), 1500);
    return () => {
      clearInterval(t);
      p.onChanged?.(); // a login ended: the profiles' status may have changed
    };
    // The key must differ from no login at all: one host's part is named "", and [""].join(" ") is "" too (#30).
  }, [JSON.stringify(logging)]);
  /* eslint-enable react-hooks/exhaustive-deps */

  /** One owner command to a host; ok shows Saved and reloads what it changed. The error is shown on the row that sent it. */
  const command = async (host: string, action: string, args: Record<string, unknown>, reload: () => Promise<void>): Promise<string | null> => {
    const id = hosts.find((h) => h.name === host)?.id;
    if (!id) return "This host reports no id, so nothing can be sent to it until it updates";
    const r = await api.command(id, action, undefined, args);
    if (!r.ok) {
      await reload(); // the control goes back to what the host has
      return r.error ?? "That did not work";
    }
    saved();
    await reload();
    return null;
  };
  const of = (h: SettingsHost) => {
    const host = h.name;
    const slice = slices[host] ?? EMPTY_SLICE;
    const runProvider: Run = async (action, args) => {
      const e = await command(host, action, args, () => loadProviders(host));
      if (!e) {
        p.onChanged?.();
        void loadSa(host);
      }
      return e;
    };
    return {
      ...slice,
      host: h,
      /** a sub-page of this host's Providers part (a provider, "new") */
      ref: (sub: string): PageRef => (host ? { page: "providers", host, sub } : { page: "providers", sub }),
      loadProviders: () => loadProviders(host),
      loadDeploy: () => loadDeploy(host),
      loadRepos: () => loadRepos(host),
      loadSa: () => loadSa(host),
      runProvider,
      /** V0.settings-ia: Add provider, and the id of the provider it made (so the form lands on its page): the list
       *  is read again and compared with the one the form opened on. */
      createProvider: async (args: Record<string, unknown>): Promise<{ error: string | null; id?: string }> => {
        const before = (slice.providers ?? []).map((x) => x.id);
        const e = await runProvider("provider.create", args);
        if (e) return { error: e };
        const l = await readOf(host, (id) => api.providers(id));
        return { error: null, id: l.ok ? newProviderId(before, l.data, String(args.name ?? "")) : undefined };
      },
      /** V0.120: ask a provider for its models now. Not a save: no Saved mark. The reply waits for the check. */
      refreshModels: async (id: string): Promise<string | null> => {
        const to = hosts.find((h) => h.name === host)?.id;
        if (!to) return "This host reports no id, so nothing can be sent to it until it updates";
        const r = await api.command(to, "provider.refreshModels", undefined, { id });
        await loadProviders(host);
        if (r.ok) p.onChanged?.();
        return r.ok ? null : (r.error ?? "That did not work");
      },
      runDeploy: (on: boolean) => command(host, "host.autoDeploy", { on }, () => loadDeploy(host)),
      loadUpdates: () => loadUpdates(host),
      runUpdateNow: () => command(host, "host.updateNow", {}, () => loadUpdates(host)),
      runRepo: (repo: string, on: boolean) => command(host, "host.autoMerge", { repo, on }, () => loadRepos(host)),
      runSa: (args: Record<string, unknown>) => command(host, "host.systemAgents", args, () => loadSa(host)),
    };
  };
  const all = hosts.map(of);
  /** The part a sub-page belongs to: its host's (the one part of a front with one host has no name); none for a host
   *  that is not one of the parts. */
  const at = (host: string | undefined) => all.find((d) => d.host.name === (host ?? "")) ?? (!host && all.length === 1 ? all[0] : undefined);
  return { all, at, grouped: hosts.length > 1 };
}
export type AllSettings = ReturnType<typeof useSettingsData>;
export type Data = AllSettings["all"][number];
