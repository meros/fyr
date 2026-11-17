// SPEC V0.119 (amends V0.50), V0.125: Settings is five pages behind one nav: Providers (with System agents),
// Automation (Auto-deploy, Auto-merge), Projects, Hosts and This device (Notifications, Theme, Show tool details,
// Install, How this browser gets in). A provider, Add provider, a project and a host are sub-pages. Every change saves at once. The same pages on the desktop (a
// dialog: the nav is its left column, the page its right pane) and the phone (the nav is the Settings screen, a page
// is the next screen); the reason for the difference is width.

import { HostMark, useHostMarks } from "./host-mark-ui.tsx";
import { HOST_ICON_NAMES, hostGlyph, ownerIcon } from "./host-mark.ts";
import { HOST_GLYPHS } from "./lucide.ts";
import { PALETTE_NAMES } from "./projects.ts";
import { useEffect, useRef, useState } from "preact/hooks";
import { Fragment, type ComponentChildren } from "preact";
import { Boundary } from "./boundary.tsx";
import type { Api } from "./api.ts";
import { hostForProject, hostLine, hostRows, hostState, howIn, profilesOn, routeOf, shortID, updateWords, type HostInfo } from "./hosts.ts";
import { automationSummaryHosts, groupLine, readOnlyNote, settingsHosts, withHost, type SettingsHost } from "./host-settings.ts";
import { quotaFor } from "./quota.ts";
import { budgetLine, jobLines, type HostLoad } from "./load.ts";
import { useUnd } from "./undeclared-ctx.tsx";
import { UndIcon } from "./undeclared-ctx.tsx";
import { addedRows, parseThreshold, thresholdError, suggestionRows, thresholdRows, undeclaredRows } from "./undeclared.ts";
import { refreshLoad, useLoad } from "./load-ui.tsx";
import { QuotaLine, useQuotas } from "./quota-ui.tsx";
import { CARD_WORD, cardState } from "./quota.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { removeProviderAsk } from "./confirm.ts";
import { useAsk } from "./confirm-ui.tsx";
import type { Profile, Project } from "./types.ts";
import type { Theme } from "./inbox.tsx";
import { repoKey, sortRepos, type AutoMergeRepo } from "./automerge.ts";
import { githubRows, githubState, updateRow, updateState, type UpdatesView } from "./updates.ts";
import { jobLine, modelOptions, providerOptions, type Option, type SystemAgentChoice, type SystemAgentsView } from "./system-agents.ts";
import { colorVar, projectSearchLine, projectSubtitle, searchProjects, settingsProjects } from "./projects.ts";
import { quietNote } from "./quiet.ts";
import { NewProjectStep, ProjectEditor, ProjectIcon, type ProjectPatch } from "./project-ui.tsx";
import { pushRow, type PushStatus } from "./push.ts";
import { onToolDetails, setToolDetails, toolDetails } from "./prefs.ts";
import { move } from "./reorder.ts";
import { DevicesPage, useAuthState } from "./devices-ui.tsx";
import { PeersPanel, usePeerState } from "./peers-ui.tsx";
import { devicesNeed, devicesSummary } from "./devices.ts";
import { PAGES, deviceSummary, hostsNeed, hostsSummary, navBadge, projectsSummary, providersNeed, providersSummary, type PageId, type PageRef } from "./settings-nav.ts";
import { Empty, Grip, Group, LoadError, MoveButtons, Row, SaveField, SavedMark, Seg, SetContext, Skeleton, Switch, useReorder, useSaved, useSet } from "./settings-rows.tsx";
import {
  createArgs,
  EMPTY_FORM,
  importable,
  keyLine,
  kindLabel,
  listLine,
  loginActive,
  loginButton,
  loginLine,
  checkLine,
  modelsFoot,
  modelParts,
  modelsAfter,
  needsCheck,
  offLimit,
  withCurrent,
  removeLine,
  STATUS_TEXT,
  usageLine,
  whereText,
  type FoundDTO,
  type LoginDTO,
  type ModelRow,
  type ModelsOp,
  type ProviderDTO,
  type ProviderForm,
} from "./providers.ts";


/** This device: kept in this browser. */
export interface SettingsDevice {
  push: PushStatus;
  pushReason: string | null;
  /** turn notifications on or off; the error, or null */
  setPush(on: boolean): Promise<string | null>;
  testPush(): Promise<string | null>;
  theme: Theme;
  setTheme(t: Theme): void;
  canInstall: boolean;
  onInstall(): void;
}

export interface ProjectActions {
  update(id: string, patch: ProjectPatch): Promise<string | null>;
  setFolder(id: string, path: string): Promise<string | null>;
  delete(id: string): Promise<string | null>;
  /** New project without a thread; the error, or null. V0.132: on `host` (several hosts) */
  create(name: string, folder: string, host?: string): Promise<string | null>;
  /** V0.121: the owner's project order (front.projectOrder); the error, or null */
  order(ids: string[]): Promise<string | null>;
}

export interface SettingsProps {
  api: Pick<Api, "providers" | "providersFound" | "command"> & Partial<Pick<Api, "autoMerge" | "autoDeploy" | "updates" | "systemAgents" | "authState" | "authAct" | "peerState" | "peerAct">>;
  host: string;
  /** V0.51: the front's hosts with their state */
  hosts?: HostInfo[];
  /** V0.125: this install's id (the state's hostId, V0.124), shown when the front lists no hosts */
  hostId?: string;
  /** V0.125: the state's user, for How this browser gets in */
  user?: string;
  phone?: boolean;
  /** the page shown; null is the phone's nav screen (the desktop always shows a page) */
  at: PageRef | null;
  /** open a page (null: the phone's nav) */
  go(r: PageRef | null): void;
  /** phone: the screen's header (title, and what sits at its right) */
  header?(title: ComponentChildren, end?: ComponentChildren): ComponentChildren;
  /** no connection to the host: what was last read shows and nothing can be changed */
  offline?: boolean;
  /** the providers changed: reload the state's profiles */
  onChanged?(): void;
  device: SettingsDevice;
  projects: Project[];
  profiles: Profile[];
  /** the host's clock */
  now: number;
  /** the first state has arrived, so `now` is the host's clock (V0.120: the page's check on open waits for it) */
  clockKnown?: boolean;
  projectActions: ProjectActions;
}

const OFFLINE = (host: string) => `No connection to ${host || "the host"}. Settings show what was last read and cannot be changed until it is back.`;

// ---- the data every page and the nav read; each part of each host loads by itself ----

type Run = (action: string, args: Record<string, unknown>) => Promise<string | null>;

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

/** V0.127: the settings of every host the front follows. Each host's reads and commands name it (`?host=`,
 *  `args.host`); the front sends them there. One host (or none) is one part that names no host, as before. */
function useSettingsData(p: SettingsProps, saved: () => void) {
  const api = p.api;
  const hosts = settingsHosts(p.hosts, p.host);
  const [slices, setSlices] = useState<Record<string, HostSlice>>({});
  const patch = (host: string, part: Partial<HostSlice>) => setSlices((s) => ({ ...s, [host]: { ...(s[host] ?? EMPTY_SLICE), ...part } }));
  const seen = (host: string, r: { readAt?: string }) => patch(host, { readAt: r.readAt ?? null });

  const loadProviders = async (host: string) => {
    try {
      const [l, f] = await Promise.all([api.providers(host), api.providersFound(host).catch(() => ({ data: [] as FoundDTO[] }))]);
      patch(host, { providers: l.data, found: f.data, provErr: null, readAt: l.readAt ?? null });
    } catch (e) {
      patch(host, { provErr: `Could not read the providers: ${(e as Error).message}.` });
    }
  };
  const loadDeploy = async (host: string) => {
    if (!api.autoDeploy) return patch(host, { deploy: null });
    try {
      const r = await api.autoDeploy(host);
      patch(host, { deploy: r.data.on, deployErr: null });
      seen(host, r);
    } catch (e) {
      patch(host, { deployErr: `Could not read the auto-deploy setting: ${(e as Error).message}.` });
    }
  };
  // V0.126 (#151): the update record; a host without one shows nothing.
  const loadUpdates = async (host: string) => {
    if (!api.updates) return patch(host, { updates: null });
    try {
      // A host that never updated itself (a NixOS host) sends "updates": null from an older fyrd: an empty list.
      const v = (await api.updates(host)).data;
      patch(host, { updates: { ...v, updates: v.updates ?? [] } });
    } catch {
      patch(host, { updates: null });
    }
  };
  const loadRepos = async (host: string) => {
    if (!api.autoMerge) return patch(host, { repos: [] });
    try {
      const r = await api.autoMerge(host);
      patch(host, { repos: r.data, reposErr: null });
      seen(host, r);
    } catch (e) {
      patch(host, { reposErr: `Could not read the auto-merge setting: ${(e as Error).message}.` });
    }
  };
  const loadSa = async (host: string) => {
    if (!api.systemAgents) return;
    try {
      const r = await api.systemAgents(host);
      patch(host, { sa: r.data, saErr: null });
      seen(host, r);
    } catch (e) {
      patch(host, { saErr: `Could not read the system agents: ${(e as Error).message}.` });
    }
  };
  const loadAll = (host: string) => {
    void loadProviders(host);
    void loadDeploy(host);
    void loadUpdates(host);
    void loadRepos(host);
    void loadSa(host);
  };
  // Every host once, and a host again when it comes back online (what showed was last read).
  const online = hosts.map((h) => `${h.name}:${h.online ? 1 : 0}`).join(" ");
  const was = useRef<Record<string, boolean>>({});
  useEffect(() => {
    for (const h of hosts) {
      if (!(h.name in was.current) || (h.online && !was.current[h.name])) loadAll(h.name);
      was.current[h.name] = h.online;
    }
  }, [online]);
  // V0.53: while a login runs on a host, follow it there (its link, the code's answer, the end).
  const logging = hosts.filter((h) => slices[h.name]?.providers?.some((x) => loginActive(x.login))).map((h) => h.name);
  useEffect(() => {
    if (!logging.length) return;
    const t = setInterval(() => logging.forEach((h) => void loadProviders(h)), 1500);
    return () => {
      clearInterval(t);
      p.onChanged?.(); // a login ended: the profiles' status may have changed
    };
  }, [logging.join(" ")]);

  /** One owner command to a host; ok shows Saved and reloads what it changed. The error is shown on the row that sent it. */
  const command = async (host: string, action: string, args: Record<string, unknown>, reload: () => Promise<void>): Promise<string | null> => {
    const r = await api.command(action, undefined, withHost(args, host));
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
      /** V0.120: ask a provider for its models now. Not a save: no Saved mark. The reply waits for the check. */
      refreshModels: async (id: string): Promise<string | null> => {
        const r = await api.command("provider.refreshModels", undefined, withHost({ id }, host));
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
  /** The part a sub-page belongs to: its host's, else the first. */
  const at = (host: string | undefined) => all.find((d) => d.host.name === (host ?? "")) ?? (host ? undefined : all[0]);
  return { all, at, grouped: hosts.length > 1 };
}
type AllSettings = ReturnType<typeof useSettingsData>;
type Data = AllSettings["all"][number];

/** What needs the owner on the Providers page, for the phone's More sheet ("1 needs a login"); "" while unknown. V0.127:
 *  over every host the front follows. */
export function useProvidersNeed(api: SettingsProps["api"], hosts?: HostInfo[], self = ""): string {
  const [v, setV] = useState("");
  const names = settingsHosts(hosts, self).map((h) => h.name);
  useEffect(() => {
    let live = true;
    void Promise.all(
      names.map((h) =>
        Promise.all([api.providers(h).then((r) => r.data), api.systemAgents ? api.systemAgents(h).then((r) => r.data).catch(() => null) : null]).catch(() => [[], null] as const),
      ),
    ).then((parts) => live && setV(providersNeed(parts.flatMap(([l]) => l), parts.reduce((n, [, sa]) => n + (sa?.jobs.filter((j) => j.error).length ?? 0), 0))));
    return () => void (live = false);
  }, [names.join(" ")]);
  return v;
}

// ---- the shell ----

export function Settings(p: SettingsProps) {
  const sv = useSaved();
  const data = useSettingsData(p, sv.mark);
  const peers = usePeerState(p.api, p.at?.page === "hosts"); // V0.159: the installs panel of Hosts
  const auth = useAuthState(p.api, true); // V0.157: Devices, read by the page and by the nav's line
  const env = { readOnly: !!p.offline, phone: !!p.phone, saved: sv.mark };
  // V0.127: the nav's lines count every host; an offline host Fyr has never read is left out.
  const read = data.all.filter((d) => d.host.online || d.providers !== null);
  const provs = read.some((d) => d.providers === null) ? null : read.flatMap((d) => d.providers!);
  const blocked = data.all.reduce((n, d) => n + (d.sa?.jobs.filter((j) => j.error).length ?? 0), 0);
  const summaries: Record<PageId, string> = {
    providers: providersSummary(provs, blocked),
    automation: automationSummaryHosts(data.all.map((d) => ({ deploy: p.api.autoDeploy ? d.deploy : false, repos: d.repos, skip: !d.host.online && d.repos === null }))),
    projects: projectsSummary(settingsProjects(p.projects).length),
    hosts: hostsSummary(p.hosts),
    devices: devicesSummary(auth.s),
    device: deviceSummary(p.device.push, p.device.theme),
  };
  const badges: Partial<Record<PageId, number>> = { providers: navBadge(provs, blocked), hosts: hostsNeed(p.hosts), devices: devicesNeed(auth.s) };
  const [adding, setAdding] = useState(false);
  const [q, setQ] = useState("");
  const [reordering, setReordering] = useState(false);
  useEffect(() => (setAdding(false), setReordering(false), setQ("")), [p.at?.page]);
  const ui: PageUi = { adding, setAdding, empty: settingsProjects(p.projects).length === 0, q, setQ, reordering, setReordering };
  const view = p.at ? pageView(p.at, p, data, ui, auth, peers) : null;

  // Opening a page moves focus to its heading; coming back from a sub-page focuses the row it came from.
  const pane = useRef<HTMLDivElement>(null);
  const prev = useRef<PageRef | null>(p.at);
  const byKeys = useRef(false);
  useEffect(() => {
    const was = prev.current;
    prev.current = p.at;
    if (!p.at || (was && was.page === p.at.page && was.sub === p.at.sub && was.host === p.at.host)) return;
    if (byKeys.current) return void (byKeys.current = false);
    const el = pane.current ?? document;
    requestAnimationFrame(() => {
      if (was?.sub && was.page === p.at!.page && !p.at!.sub) {
        const row = el.querySelector<HTMLElement>(`[data-sub="${CSS.escape(subKey(was))}"]`);
        if (row) return row.focus();
      }
      if (!p.phone) el.querySelector<HTMLElement>(".stp-h h2")?.focus();
    });
  }, [p.at?.page, p.at?.sub, p.at?.host]);

  const nav = (
    <SettingsNav
      phone={!!p.phone}
      active={p.at?.page ?? null}
      summaries={summaries}
      badges={badges}
      onPick={(id, keys) => {
        byKeys.current = !!keys;
        p.go({ page: id });
      }}
    />
  );

  if (p.phone) {
    // Phone: the nav is the Settings screen and a page is the next screen (no room for two columns). The header
    // carries the title and Back, so the Saved mark sits at its right.
    return (
      <SetContext.Provider value={env}>
        {p.header?.(view ? view.title : "Settings", view ? <SavedMark shown={sv.shown} /> : undefined)}
        {p.offline && <div class="m-ban">{OFFLINE(p.host)}</div>}
        <div class="m-nbody" ref={pane}>
          <div class={`set phone${p.offline ? " ro" : ""}`}>
            {view ? (
              <div class="stp" data-page={pageKey(p.at!)}>
                {view.end && <div class="sbar">{view.end}</div>}
                {view.lede && <p class="stp-lede">{view.lede}</p>}
                <Boundary name="This page" resetKey={pageKey(p.at!)}>
                  {view.body}
                </Boundary>
              </div>
            ) : (
              nav
            )}
          </div>
        </div>
      </SetContext.Provider>
    );
  }
  const v = view ?? pageView({ page: "providers" }, p, data, ui, auth, peers);
  const at = p.at ?? { page: "providers" };
  return (
    <SetContext.Provider value={env}>
      <div class={`set stx${p.offline ? " ro" : ""}`}>
        {nav}
        <div class="stp" ref={pane} data-page={pageKey(at)}>
          {p.offline && <div class="m-ban d">{OFFLINE(p.host)}</div>}
          <div class="stp-h">
            {v.parent && (
              <>
                <button type="button" class="crumb" onClick={() => p.go({ page: at.page })}>
                  ‹ {v.parent}
                </button>
                <span class="sep">/</span>
              </>
            )}
            <h2 tabIndex={-1} class={at.sub && at.page !== "automation" ? "wrap" : ""}>
              {v.title}
            </h2>
            <span class="end">
              <SavedMark shown={sv.shown} />
              {v.end}
            </span>
          </div>
          {v.lede && <p class="stp-lede">{v.lede}</p>}
          {/* V0.24: a page that fails to render shows the crash card in the pane; the nav and Close keep working */}
          <Boundary name="This page" resetKey={pageKey(at)}>
            {v.body}
          </Boundary>
        </div>
      </div>
    </SetContext.Provider>
  );
}

const pageKey = (r: PageRef) => (r.sub ? `${r.page}/${subKey(r)}` : r.page);
/** A sub-page's row (data-sub), where focus goes back to: "lab/claude" for a host's provider (V0.127), else the id. */
const subKey = (r: PageRef) => (r.host ? `${r.host}/${r.sub ?? ""}` : (r.sub ?? ""));

/** The five pages: a title and a line of state each, and the amber count on a page that needs the owner (Providers,
 *  V0.125 Hosts). Desktop: the dialog's left
 *  column; ↑ and ↓ move between pages and the page follows the focus. Phone: the Settings screen; the chevron shows
 *  there only, because there the row leaves the screen. */
export function SettingsNav({ phone, active, summaries, badges, onPick }: { phone: boolean; active: PageId | null; summaries: Record<PageId, string>; badges: Partial<Record<PageId, number>>; onPick(id: PageId, keys?: boolean): void }) {
  const ref = useRef<HTMLElement>(null);
  const onKey = (e: KeyboardEvent) => {
    if (phone || (e.key !== "ArrowDown" && e.key !== "ArrowUp")) return;
    const items = [...(ref.current?.querySelectorAll<HTMLButtonElement>(".stn-i") ?? [])];
    const i = items.indexOf(document.activeElement as HTMLButtonElement);
    if (i < 0) return;
    e.preventDefault();
    const j = Math.max(0, Math.min(items.length - 1, i + (e.key === "ArrowDown" ? 1 : -1)));
    items[j].focus();
    onPick(PAGES[j].id, true);
  };
  return (
    <nav class="stn" aria-label="Settings" ref={ref} onKeyDown={onKey}>
      <div class="stn-c">
        {PAGES.map((x) => (
          <button type="button" class={`stn-i${x.id === active ? " on" : ""}`} aria-current={x.id === active ? "page" : undefined} data-page={x.id} onClick={() => onPick(x.id)}>
            <span class="stn-t">{x.title}</span>
            <span class="stn-s">{summaries[x.id]}</span>
            <span class="stn-a">
              {(badges[x.id] ?? 0) > 0 && <span class="pst needs-login">{badges[x.id]}</span>}
              {phone && <span class="chev">›</span>}
            </span>
          </button>
        ))}
      </div>
    </nav>
  );
}

interface PageView {
  title: ComponentChildren;
  /** the sub-page's parent, for the desktop crumb */
  parent?: string;
  /** at the end of the title line (desktop); first on the page (phone) */
  end?: ComponentChildren;
  lede?: string;
  body: ComponentChildren;
}

/** Page state the shell keeps, so a page's title line and its body agree (New project). */
interface PageUi {
  adding: boolean;
  setAdding(b: boolean): void;
  empty: boolean;
  /** V0.121: the Projects page's search and the phone's Reorder mode, kept here because the header holds Reorder */
  q: string;
  setQ(q: string): void;
  reordering: boolean;
  setReordering(b: boolean): void;
}

function pageView(at: PageRef, p: SettingsProps, s: AllSettings, ui: PageUi, auth: ReturnType<typeof useAuthState>, peers: ReturnType<typeof usePeerState>): PageView {
  const go = p.go;
  switch (at.page) {
    case "providers": {
      // V0.127: a host's provider and Add provider name the host; with several hosts the page says which one.
      const d = at.sub ? s.at(at.host) : undefined;
      const on = d && s.grouped ? d.host.label : "";
      if (d && at.sub === "new")
        return {
          title: "Add provider",
          parent: "Providers",
          lede: on ? `On ${on}. A key you paste is kept on ${on} and never leaves it.` : undefined,
          body: (
            <HostEnv d={d}>
              <AddProvider onCancel={() => go({ page: "providers" })} onCreate={async (args) => { const e = await d.runProvider("provider.create", args); if (!e) go({ page: "providers" }); return e; }} />
            </HostEnv>
          ),
        };
      if (d && at.sub) {
        const x = d.providers?.find((y) => y.id === at.sub);
        const note = readOnlyNote(d.host, d.readAt, p.now);
        return {
          title: x?.name ?? "Provider",
          parent: "Providers",
          end: x && <ProviderBadge p={x} host={d.host.name} />,
          lede: on ? `On ${on}.` : undefined,
          body: (
            <HostEnv d={d}>
              {note && x && <div class="note hnote">{note}</div>}
              {x ? <ProviderPage key={`${d.host.name}/${x.id}`} p={x} host={d.host.name} run={d.runProvider} refresh={() => d.refreshModels(x.id)} now={p.now} clockKnown={p.clockKnown ?? true} onRemoved={() => go({ page: "providers" })} /> : d.providers === null && !d.provErr ? <Skeleton rows={4} /> : d.provErr ? <LoadError text={d.provErr} onRetry={() => void d.loadProviders()} /> : <Group><Empty text="This provider is not here anymore." /></Group>}
            </HostEnv>
          ),
        };
      }
      // An unknown host shows the Providers page.
      return { title: "Providers", body: <HostParts s={s} now={p.now} part={(d) => <ProvidersPage d={d} go={go} grouped={s.grouped} />} /> };
    }
    case "automation":
      return { title: "Automation", lede: "What Fyr does without your press. Only you change these; an agent cannot.", body: <HostParts s={s} now={p.now} part={(d) => <AutomationPage d={d} hasDeploy={!!p.api.autoDeploy} api={p.api} focus={at.sub} />} /> };
    case "projects": {
      if (at.sub) {
        const pr = p.projects.find((x) => x.id === at.sub);
        return {
          title: pr?.name ?? "Project",
          parent: "Projects",
          body: pr ? (
            <ProjectEditor
              key={pr.id}
              project={pr}
              profiles={profilesOn(hostForProject(pr.host, p.hosts), p.hosts, p.profiles) /* V0.127: its host's providers */}
              now={p.now}
              onSave={(patch) => p.projectActions.update(pr.id!, patch)}
              onSetFolder={(v) => p.projectActions.setFolder(pr.id!, v)}
              onDelete={async () => {
                const e = await p.projectActions.delete(pr.id!);
                if (!e) go({ page: "projects" });
                return e;
              }}
            />
          ) : (
            <Group>
              <Empty text={p.projects.length ? "This project is not here anymore." : "Loading…"} />
            </Group>
          ),
        };
      }
      return { title: "Projects", ...projectsPage(p, ui) };
    }
    case "hosts": {
      const self = p.host || "local";
      const rows = hostRows(p.hosts, self, p.hostId ?? "");
      const h = at.sub ? rows.find((x) => x.name === at.sub) : undefined;
      // An unknown name shows the Hosts page.
      if (h)
        return {
          title: <span class="mono">{h.name}</span>,
          parent: "Hosts",
          end: <HostPill h={h} />,
          body: <HostPage key={h.name} h={h} self={self} api={p.api} now={p.now} />,
        };
      const runOrder = async (names: string[]) => {
        const r = await p.api.command("front.hostOrder", undefined, { names }); // V0.132
        return r.ok ? null : (r.error ?? "That did not work");
      };
      return {
        title: "Hosts",
        end: <HostsReorderButton ui={ui} n={rows.length} />,
        body: (
          <>
            <HostsPage rows={rows} self={self} now={p.now} go={go} ui={ui} run={runOrder} />
            {p.api.peerState && p.api.peerAct && <PeersPanel api={{ peerState: p.api.peerState, peerAct: p.api.peerAct }} st={peers} />}
          </>
        ),
      };
    }
    case "devices":
      if (!p.api.authState || !p.api.authAct) return { title: "Devices", body: <Empty text="This host cannot list devices." /> };
      return { title: "Devices", lede: "Who is signed in to this install, and how a new device gets in.", body: <DevicesPage api={{ authState: p.api.authState, authAct: p.api.authAct }} now={p.now} st={auth} /> };
    case "device":
      return { title: "This device", lede: "These are kept in this browser. Each device has its own.", body: <DevicePage d={p.device} user={p.user ?? ""} /> };
  }
}

// ---- Providers ----

/** V0.127: the per-host pages, one part per host. With one host the part has no heading, as before; with several
 *  each has the host's name and state, and an offline host's part shows what Fyr last read from it, read-only, or only
 *  says so when it has nothing. */
function HostParts({ s, now, part }: { s: AllSettings; now: number; part(d: Data): ComponentChildren }) {
  if (!s.grouped) return <>{part(s.all[0])}</>;
  return (
    <>
      {s.all.map((d) => {
        const note = readOnlyNote(d.host, d.readAt, now);
        const nothing = !d.host.online && !d.readAt && d.providers === null && d.deploy === null && d.repos === null;
        return (
          <section key={d.host.name} class={`hpart${d.host.online ? "" : " off"}`} data-host={d.host.name} aria-label={`Settings on ${d.host.label}`}>
            <h3 class="hpart-h">
              <span class="mono">{d.host.label}</span>
              <span class="hpart-s">{groupLine(d.host, now)}</span>
            </h3>
            {note && <div class="note hnote">{note}</div>}
            {!nothing && <HostEnv d={d}>{part(d)}</HostEnv>}
          </section>
        );
      })}
    </>
  );
}

/** A host's controls are read-only while it is offline (V0.127), as the whole page is with no connection. */
function HostEnv({ d, children }: { d: Data; children: ComponentChildren }) {
  const env = useSet();
  return <SetContext.Provider value={{ ...env, readOnly: env.readOnly || !d.host.online }}>{children}</SetContext.Provider>;
}

/** The status badge: today's words; V0.77 (1), #65: it tells the truth about the account, so Ready never sits beside
 *  its own error. V0.127: the quota is its host's. */
function ProviderBadge({ p, host }: { p: ProviderDTO; host: string }) {
  const quota = quotaFor(useQuotas(), p.id, host || undefined);
  return <span class={`pst ${p.status}`}>{quota && p.status === "ready" ? CARD_WORD[cardState(quota)] : STATUS_TEXT[p.status]}</span>;
}

function ProvidersPage({ d, go, grouped }: { d: Data; go(r: PageRef): void; grouped: boolean }) {
  const quotas = useQuotas();
  const [more, setMore] = useState(false);
  const env = useSet();
  const finds = importable(d.found);
  const [importErr, setImportErr] = useState<{ id: string; text: string } | null>(null);
  const none = d.providers?.length === 0;
  return (
    <>
      {d.provErr && d.providers === null ? (
        <LoadError text={d.provErr} onRetry={() => void d.loadProviders()} />
      ) : d.providers === null ? (
        <Skeleton rows={3} label="Loading the providers" />
      ) : none ? (
        <Group>
          <Empty title="No providers yet" text="A provider is a Claude login or an API endpoint that threads run on." />
        </Group>
      ) : (
        <Group label="Providers">
          {d.providers.map((x) => {
            const q = quotaFor(quotas, x.id, d.host.name || undefined);
            return (
              <Row
                onClick={() => go(d.ref(x.id))}
                data={{ sub: subKey(d.ref(x.id)) }}
                class="prow"
                lead={<span class="pdot" style={x.color ? { background: x.color } : undefined} />}
                title={x.name}
                ell
                desc={
                  <>
                    {listLine(x)}
                    {q && <QuotaLine q={q} name={x.name} bare />}
                  </>
                }
                end={
                  <>
                    <ProviderBadge p={x} host={d.host.name} />
                    <span class="chev">›</span>
                  </>
                }
              />
            );
          })}
        </Group>
      )}
      {finds.length > 0 && (
        <Group title="Found on this host" label="Found on this host" foot="Logins and keys Fyr found here. Import makes one a provider; nothing is copied off this host.">
          {(more ? finds : finds.slice(0, 3)).map((f) => (
            <Row
              class="found"
              title={f.name}
              error={importErr?.id === f.id ? importErr.text : null}
              desc={
                <>
                  {kindLabel(f.kind)} · {f.keyFrom ? `key from ${f.keyFrom}` : f.source}
                  {f.note && <span class="sr-d2">{f.note}</span>}
                </>
              }
              end={
                <button
                  type="button"
                  class="btn"
                  disabled={env.readOnly}
                  onClick={async () => {
                    const e = await d.runProvider("provider.import", { foundId: f.id });
                    setImportErr(e ? { id: f.id, text: e } : null);
                  }}
                >
                  Import
                </button>
              }
            />
          ))}
          {!more && finds.length > 3 && (
            <button type="button" class="more" onClick={() => setMore(true)}>
              Show {finds.length - 3} more
            </button>
          )}
        </Group>
      )}
      <div class="sbar">
        <button type="button" class={`btn addp${none ? " p" : ""}`} disabled={env.readOnly} data-sub={subKey(d.ref("new"))} onClick={() => go(d.ref("new"))}>
          Add provider
        </button>
      </div>
      {grouped ? <h4 class="stp-sub">System agents</h4> : <h3 class="stp-sub">System agents</h3>}
      {none ? (
        <Group label="System agents">
          <Empty text="Add a provider first. System agents run on one of them." />
        </Group>
      ) : (
        <SystemAgents d={d} />
      )}
    </>
  );
}

function Select({ label, value, options, onPick }: { label: string; value: string; options: Option[]; onPick(v: string): void }) {
  const env = useSet();
  return (
    <select value={value} aria-label={label} disabled={env.readOnly} onChange={(e) => onPick((e.target as HTMLSelectElement).value)}>
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </select>
  );
}

/** V0.115: the provider and model of the calls Fyr starts itself (thread titles, project setup), per host: for all
 *  jobs, and per job. No provider is the host's default, no model is its Haiku; a job that cannot run says why on its
 *  row and fails visibly, never on a bigger model. Only the owner changes it; no agent tool can. */
function SystemAgents({ d }: { d: Data }) {
  const [err, setErr] = useState<{ row: string; text: string } | null>(null);
  const v = d.sa;
  const set = async (row: string, job: string, c: SystemAgentChoice | null) => {
    const e = await d.runSa(c ? { job, profile: c.profile ?? "", model: c.model ?? "" } : { job, clear: true });
    setErr(e ? { row, text: e } : null);
  };
  const rowErr = (row: string) => (err?.row === row ? err.text : null);
  if (!v && d.saErr) return <LoadError text={d.saErr} onRetry={() => void d.loadSa()} />;
  if (!v) return <Skeleton rows={2} label="Loading the system agents" />;
  const def = v.default ?? {};
  return (
    <Group label="System agents" foot="The model calls Fyr makes itself. A job that cannot run fails and says why; it never moves to a bigger model.">
      <Row
        title="Default"
        desc="For every job that has no choice of its own."
        error={rowErr("")}
        stack
        class="two"
        end={
          <>
            <Select label="Provider" value={def.profile ?? ""} options={providerOptions(v)} onPick={(x) => void set("", "", { profile: x })} />
            <Select label="Model" value={def.model ?? ""} options={withCurrent(modelOptions(v, def.profile ?? ""), def.model)} onPick={(x) => void set("", "", { profile: def.profile, model: x })} />
          </>
        }
      />
      {v.jobs.map((j) => {
        const line = jobLine(v, j);
        const own = j.choice.profile ?? def.profile ?? "";
        return (
          <Row
            key={j.id}
            title={j.name}
            desc={line.error ? undefined : line.text}
            error={rowErr(j.id) ?? (line.error ? line.text : null)}
            stack
            class={`sajob${j.override ? " two" : ""}`}
            end={
              <>
                <Select label={j.name} value={j.override ? (j.choice.profile ?? "") : "="} options={[{ value: "=", label: "Same as default" }, ...providerOptions(v).slice(1)]} onPick={(x) => void set(j.id, j.id, x === "=" ? null : { profile: x })} />
                {j.override && <Select label="Model" value={j.choice.model ?? ""} options={withCurrent(modelOptions(v, own), j.choice.model)} onPick={(x) => void set(j.id, j.id, { profile: j.choice.profile, model: x })} />}
              </>
            }
          />
        );
      })}
    </Group>
  );
}

/** A provider's page: what blocks it first (a login, a key), its models, its usage, Remove last. */
function ProviderPage({ p, host, run, refresh, now, clockKnown, onRemoved }: { p: ProviderDTO; host: string; run: Run; refresh(): Promise<string | null>; now: number; clockKnown: boolean; onRemoved(): void }) {
  const quota = quotaFor(useQuotas(), p.id, host || undefined);
  const ask = useAsk();
  const env = useSet();
  const [rmErr, setRmErr] = useState<string | null>(null);
  return (
    <>
      {p.kind === "endpoint" ? (
        <Group title="API key">
          <KeyRow p={p} run={run} />
        </Group>
      ) : (
        <LoginGroup p={p} run={run} />
      )}
      <ModelsGroup p={p} run={run} refresh={refresh} now={now} clockKnown={clockKnown} />
      <Group title="Usage" class="usage">
        <Row title={quota ? <QuotaLine q={quota} name={p.name} bare /> : "No reading yet"} desc={usageLine(p)} />
      </Group>
      <Group danger>
        <Row
          title="Remove provider"
          desc={removeLine(p.threads)}
          error={rmErr}
          end={
            <button
              type="button"
              class="btn danger"
              disabled={p.threads > 0 || env.readOnly}
              onClick={async () => {
                if (!(await ask(removeProviderAsk(p)))) return;
                const e = await run("provider.delete", { id: p.id });
                setRmErr(e);
                if (!e) onRemoved();
              }}
            >
              Remove…
            </button>
          }
        />
      </Group>
    </>
  );
}

/** A Claude account's login: the login panel while one runs or is needed, else who is logged in; then its token. */
function LoginGroup({ p, run }: { p: ProviderDTO; run: Run }) {
  const env = useSet();
  const [err, setErr] = useState<string | null>(null);
  const lb = loginButton(p);
  const start = async () => setErr(await run("provider.login", { id: p.id }));
  const panel = p.login && (loginActive(p.login) || p.status === "needs-login");
  return (
    <Group title="Login">
      {panel ? (
        <Row col>
          <LoginPanel p={p} l={p.login!} run={run} />
        </Row>
      ) : p.status === "needs-login" ? (
        <Row title="Not logged in" desc={p.need} error={err} end={lb && <button type="button" class="btn p" disabled={env.readOnly} onClick={() => void start()}>{lb}</button>} />
      ) : (
        <Row
          title="Logged in"
          desc={<span class="mono">{whereText(p)}</span>}
          error={err}
          end={lb && (
            <button type="button" class="btn" disabled={env.readOnly} onClick={() => void start()}>
              {lb}
            </button>
          )}
        />
      )}
      {p.auth !== "none" && <KeyRow p={p} run={run} />}
    </Group>
  );
}

/** The API key (an endpoint) or the login token (a Claude account): "Set" or "Not set" with Replace… / Set…, which
 *  shows the field in place. A missing key comes first, open and focused, with the page's primary Save key. */
function KeyRow({ p, run }: { p: ProviderDTO; run: Run }) {
  const missing = p.kind === "endpoint" && !p.keySet;
  const [open, setOpen] = useState(missing);
  const [err, setErr] = useState<string | null>(null);
  const env = useSet();
  const what = p.kind === "endpoint" ? "API key" : "Login token";
  useEffect(() => setOpen(missing), [missing]);
  if (open)
    return (
      <Row col error={null} class={`keyrow${missing ? " bad" : ""}`}>
        <span class="sr-b">
          <span class="sr-t">{what}</span>
          <span class={`sr-d${err || missing ? " err" : ""}`} role={err ? "alert" : undefined}>
            {err ?? (missing ? "The key is missing. Paste it to use this provider." : p.kind === "endpoint" ? "Paste the new key." : "Paste a token from `claude setup-token`.")}
          </span>
        </span>
        <span class="keyf">
          <SaveField
            value=""
            label={what}
            type="password"
            placeholder={p.kind === "endpoint" ? "Paste the key" : "Paste the token"}
            button={p.kind === "endpoint" ? "Save key" : "Save token"}
            primary={missing}
            alwaysButton
            clears
            autoFocus={missing}
            onError={setErr}
            onSave={async (v) => {
              const e = await run("provider.setKey", { id: p.id, key: v });
              if (!e) setOpen(false);
              return e;
            }}
          />
          {!missing && (
            <button type="button" class="btn" onClick={() => (setOpen(false), setErr(null))}>
              Cancel
            </button>
          )}
        </span>
        <span class="sr-d">Kept in a private file on this host. It is never shown again.</span>
      </Row>
    );
  const kl = keyLine(p);
  return (
    <Row
      title={what}
      desc={p.keySet ? `Set · ${kl.replace(/^(Key|Login token) /, "")}` : "Not set. Optional: a token is used instead of the browser sign-in."}
      end={
        <button type="button" class="btn" disabled={env.readOnly} onClick={() => setOpen(true)}>
          {p.keySet ? "Replace…" : "Set…"}
        </button>
      }
    />
  );
}

/** V0.120 (amends V0.44, V0.50): the model chooser. What the provider reports, in two parts: On (what every model
 *  picker offers, in this order; a grip, ★ for the default, a switch) and Off (by name, a switch). Search from nine
 *  models filters both. Refresh asks the provider now; the page asks on open when the last check is over 10 minutes
 *  old. Each change sends provider.update with the whole on-list. */
function ModelsGroup({ p, run, refresh, now, clockKnown }: { p: ProviderDTO; run: Run; refresh(): Promise<string | null>; now: number; clockKnown: boolean }) {
  const env = useSet();
  const ids = p.models.map((m) => m.id);
  const [q, setQ] = useState("");
  const [more, setMore] = useState(false);
  const [checking, setChecking] = useState(false);
  const [err, setErr] = useState<{ row: string; text: string } | null>(null);
  const parts = modelParts(p, q);
  const searching = !!q.trim();
  const apply = async (row: string, op: ModelsOp) => {
    const r = modelsAfter(ids, p.defaultModel, op);
    if ("error" in r) return setErr({ row, text: r.error }), r.error;
    const args: Record<string, unknown> = { id: p.id };
    if ("forget" in r) args.forget = r.forget;
    else if ("star" in op) args.defaultModel = r.defaultModel;
    else args.models = r.models;
    const e = await run("provider.update", args);
    setErr(e ? { row, text: e } : null);
    return e;
  };
  const doRefresh = async () => {
    setChecking(true);
    const e = await refresh();
    setChecking(false);
    setErr(e ? { row: "check", text: e } : null);
  };
  // The page asks the provider once when it opens and the last check is over 10 minutes old (by the host's clock).
  const asked = useRef(false);
  useEffect(() => {
    if (asked.current || !clockKnown) return;
    asked.current = true;
    if (needsCheck(p, now) && !env.readOnly) void doRefresh();
  }, [p.id, clockKnown]);
  const ro = useReorder(ids, (from, to) => void apply(ids[from], { move: [from, to] }));
  const canReorder = !searching && ids.length > 1 && !env.readOnly; // while the search has text, reorder is off
  const rowErr = (row: string) => (err?.row === row ? err.text : null);
  const tags = (m: ModelRow) => (
    <>
      {m.isNew && <span class="tag new">New</span>}
      {m.gone && <span class="tag gone">Gone</span>}
      {m.manual && <span class="tag">Added by hand</span>}
    </>
  );
  const desc = (m: ModelRow) => (
    <>
      {m.name !== m.id && <span class="mono mid">{m.id}</span>}
      {m.gone && <span class="sr-d2">The provider no longer lists it. Threads on it keep running.</span>}
    </>
  );
  const limit = offLimit(searching);
  const offShown = more ? parts.off : parts.off.slice(0, limit);
  const line = searching ? `${parts.matched} of ${parts.total} match` : checkLine({ ...p, checking: p.checking || checking }, now);
  const foot = modelsFoot(p, now);
  const hasEfforts = p.models.some((m) => m.efforts?.length);
  return (
    <Group
      title="Models"
      label="Models"
      class="models"
      end={
        <>
          <span class="mchk" aria-live="polite">
            {line}
          </span>
          {!ro.mode && p.status === "ready" && (
            <button type="button" class="lnk" disabled={checking || p.checking || env.readOnly} onClick={() => void doRefresh()}>
              Refresh
            </button>
          )}
          {
            // The phone reorders in a mode: touch has no hover to reveal a grip, and a drag on a list fights the
            // page's scroll. The desktop drags the grip instead, so it has no Reorder button.
            env.phone &&
              canReorder &&
              (ro.mode ? (
                <button type="button" class="btn p" onClick={() => ro.setMode(false)}>
                  Done
                </button>
              ) : (
                <button type="button" class="lnk reo" onClick={() => ro.setMode(true)}>
                  Reorder
                </button>
              ))
          }
        </>
      }
      foot={foot.text}
      footErr={foot.error}
    >
      {rowErr("check") && <Row error={rowErr("check")} />}
      {!ro.mode && parts.total >= 9 && (
        <Row class="srch">
          <input class="pq" type="search" placeholder={`Search ${parts.total} models`} aria-label="Search models" value={q} onInput={(e) => (setQ((e.target as HTMLInputElement).value), setMore(false))} />
        </Row>
      )}
      {parts.on.length > 0 && <div class="sg-sub">On · offered in every model picker, in this order</div>}
      {parts.on.map((m) => {
        const i = ids.indexOf(m.id);
        const r = ro.row(i);
        return (
          <Row
            key={m.id}
            class={`model ${searching ? "" : (r.class as string)}`}
            rowRef={r.ref as (el: HTMLElement | null) => void}
            data={{ model: m.id }}
            error={rowErr(m.id)}
            lead={
              !ro.mode && (
                <>
                  {canReorder && <Grip {...ro.grip(i, m.name)} />}
                  <button type="button" class="star" aria-pressed={m.star} aria-label={m.star ? "The default model" : `Make ${m.id} the default`} disabled={env.readOnly} onClick={() => !m.star && void apply(m.id, { star: m.id })}>
                    {m.star ? "★" : "☆"}
                  </button>
                </>
              )
            }
            title={
              <>
                {m.name}
                {tags(m)}
              </>
            }
            desc={desc(m)}
            end={ro.mode ? undefined : <Switch on label={`Offer ${m.id}`} onChange={() => void apply(m.id, { off: m.id })} />}
          >
            {ro.mode && <MoveButtons name={m.name} i={i} n={ids.length} onMove={(f, t) => void apply(ids[f], { move: [f, t] })} />}
          </Row>
        );
      })}
      {!ro.mode && parts.off.length > 0 && <div class="sg-sub">Off · {parts.off.length}</div>}
      {!ro.mode &&
        offShown.map((m) => (
          <Row
            key={m.id}
            class="omodel off"
            data={{ model: m.id }}
            error={rowErr(m.id)}
            title={
              <>
                {m.name}
                {tags(m)}
              </>
            }
            desc={desc(m)}
            end={
              <>
                {m.manual && (
                  <button type="button" class="xb" aria-label={`Remove ${m.id}`} title="Remove" disabled={env.readOnly} onClick={() => void apply(m.id, { forget: m.id })}>
                    ×
                  </button>
                )}
                <Switch on={false} label={`Offer ${m.id}`} onChange={() => void apply(m.id, { on: m.id })} />
              </>
            }
          />
        ))}
      {!ro.mode && !more && parts.off.length > limit && (
        <button type="button" class="more" onClick={() => setMore(true)}>
          Show {parts.off.length - limit} more
        </button>
      )}
      {!ro.mode && searching && parts.matched === 0 && <Row desc={`No model matches “${q.trim()}”.`} />}
      {!ro.mode && (
        <Row error={rowErr("+")} class="fieldrow">
          <SaveField value="" label="Model id to add" placeholder="Add a model id by hand" mono clears alwaysButton button="Add" onError={(e) => setErr(e ? { row: "+", text: e } : null)} onSave={async (v) => (await apply("+", { add: v })) ?? null} check={(v) => { const r = modelsAfter(ids, p.defaultModel, { add: v }); return "error" in r ? r.error : null; }} />
        </Row>
      )}
      {!ro.mode && hasEfforts && (
        <Row
          title="Default effort"
          error={rowErr("effort")}
          end={
            <select aria-label="Default effort" value={p.defaultEffort ?? ""} disabled={env.readOnly} onChange={async (e) => { const x = await run("provider.update", { id: p.id, defaultEffort: (e.target as HTMLSelectElement).value }); setErr(x ? { row: "effort", text: x } : null); }}>
              <option value="">CLI default</option>
              {p.efforts.map((x) => (
                <option value={x}>{x}</option>
              ))}
            </select>
          }
        />
      )}
    </Group>
  );
}

// V0.53: a login Fyr runs with the CLI: the sign-in link, the code field, Cancel. The code goes to the CLI only.
function LoginPanel({ p, l, run }: { p: ProviderDTO; l: LoginDTO; run: Run }) {
  const [code, setCode] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const env = useSet();
  const active = loginActive(l);
  const ends = new Date(l.expires);
  const send = () => void run("provider.loginCode", { id: p.id, code }).then((e) => (setErr(e), !e && setCode("")));
  return (
    <div class={`login ${l.state}`} aria-live="polite">
      <div class="setd">{loginLine(l)}</div>
      {((active && l.message) || err) && (
        <div class="seterr" role="alert">
          {err ?? l.message}
        </div>
      )}
      {active && l.url && (
        <>
          <a class="btn p loginlink" href={l.url} target="_blank" rel="noopener noreferrer">
            Open the sign-in page
          </a>
          <label class="fld">
            <span>Code from the sign-in page</span>
            <div class="setkey">
              <input
                autocomplete="off"
                autocapitalize="off"
                spellcheck={false}
                value={code}
                placeholder="Paste the code"
                disabled={l.state === "checking" || env.readOnly}
                onInput={(e) => setCode((e.target as HTMLInputElement).value)}
                onKeyDown={(e) => e.key === "Enter" && code.trim() && send()}
              />
              <button type="button" class="btn" disabled={!code.trim() || l.state === "checking" || env.readOnly} onClick={send}>
                Send code
              </button>
            </div>
          </label>
        </>
      )}
      <div class="setbar">
        {active ? (
          <>
            <span class="setd">The sign-in ends at {isNaN(ends.getTime()) ? "" : ends.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>
            <button type="button" class="btn" disabled={env.readOnly} onClick={() => void run("provider.loginCancel", { id: p.id })}>
              Cancel
            </button>
          </>
        ) : (
          (l.state === "failed" || l.state === "timeout" || l.state === "cancelled") && (
            <button type="button" class="btn" disabled={env.readOnly} onClick={() => void run("provider.login", { id: p.id })}>
              Try again
            </button>
          )
        )}
      </div>
    </div>
  );
}

/** Add provider (A5): today's fields and validation, as a page. */
function AddProvider(p: { onCancel(): void; onCreate(args: Record<string, unknown>): Promise<string | null> }) {
  const [f, setF] = useState<ProviderForm>(EMPTY_FORM);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const env = useSet();
  const picker = useFolderPick();
  const set = (k: keyof ProviderForm) => (e: Event) => setF({ ...f, [k]: (e.target as HTMLInputElement).value });
  const submit = async () => {
    const r = createArgs(f);
    if ("error" in r) return setErr(r.error);
    setErr(null);
    setBusy(true);
    const e = await p.onCreate(r.args);
    setBusy(false);
    if (e) setErr(e);
  };
  return (
    <>
      <Group>
        <Row title="Kind" stack end={<Seg label="Kind" value={f.kind} options={(["claude-account", "endpoint"] as const).map((k) => ({ value: k, label: kindLabel(k) }))} onChange={(k) => setF({ ...f, kind: k })} />} />
        <Row col>
          <label class="fld">
            <span>Name</span>
            <input value={f.name} onInput={set("name")} placeholder={f.kind === "endpoint" ? "OpenRouter" : "Claude (work)"} disabled={env.readOnly} />
          </label>
        </Row>
        {f.kind === "claude-account" ? (
          <Row col>
            <label class="fld">
              <span>Config folder (empty: ~/.claude)</span>
              <div class="setkey">
                <input value={f.configDir} onInput={set("configDir")} placeholder="~/.claude-work" disabled={env.readOnly} />
                <button type="button" class="btn" disabled={env.readOnly} onClick={() => void picker.pick({ title: "Config folder" }).then((d) => d && setF({ ...f, configDir: d }))}>
                  Choose…
                </button>
              </div>
            </label>
            <span class="sr-d">After adding it, press Log in on its page: Fyr runs claude's login in this folder and shows the sign-in link there.</span>
          </Row>
        ) : (
          <>
            <Row col>
              <label class="fld">
                <span>Base URL</span>
                <input value={f.baseURL} onInput={set("baseURL")} placeholder="https://openrouter.ai/api" inputMode="url" disabled={env.readOnly} />
              </label>
            </Row>
            <Row col>
              <label class="fld">
                <span>API key</span>
                <input type="password" autocomplete="off" value={f.key} onInput={set("key")} placeholder="Paste the key" disabled={env.readOnly} />
              </label>
              <span class="sr-d">Kept in a private file on this host.</span>
            </Row>
          </>
        )}
        <Row col>
          <label class="fld">
            <span>Model ids, one per line{f.kind === "claude-account" ? " (empty: Claude's list)" : " (optional: Fyr asks the endpoint for its list)"}</span>
            <textarea rows={3} class="mono" value={f.models} onInput={set("models")} disabled={env.readOnly} />
          </label>
        </Row>
      </Group>
      {err && (
        <div class="seterr" role="alert">
          {err}
        </div>
      )}
      <div class="sbar">
        <button type="button" class="btn end" onClick={p.onCancel}>
          Cancel
        </button>
        <button type="button" class="btn p" disabled={busy || env.readOnly} onClick={() => void submit()}>
          Add provider
        </button>
      </div>
    </>
  );
}

// ---- Automation ----

/** V0.111 Auto-deploy and V0.102 Auto-merge: what Fyr does without the owner's press. Only the owner changes them here;
 *  no agent tool can. */
function AutomationPage({ d, hasDeploy, api, focus }: { d: Data; hasDeploy: boolean; api: SettingsProps["api"]; focus?: string }) {
  const [deployErr, setDeployErr] = useState<string | null>(null);
  const [repoErr, setRepoErr] = useState<{ repo: string; text: string } | null>(null);
  const [q, setQ] = useState("");
  const repos = sortRepos(d.repos ?? []);
  const shown = q.trim() ? repos.filter((r) => r.repo.includes(q.trim().toLowerCase())) : repos;
  return (
    <>
      {hasDeploy &&
        (d.deploy === null && d.deployErr ? (
          <LoadError text={d.deployErr} onRetry={() => void d.loadDeploy()} />
        ) : d.deploy === null ? (
          <Skeleton rows={1} />
        ) : (
          <Group label="Auto-update">
            <Row
              label
              title="Update Fyr by itself"
              desc="Installs each signed release CI publishes for main, once it has stayed the newest for 10 minutes. The new version is checked before the switch and rolled back if it is not healthy. A local build of a checkout always waits for your press."
              error={deployErr}
              end={<Switch on={d.deploy} label="Auto-update Fyr" onChange={async (on) => setDeployErr(await d.runDeploy(on))} />}
            />
          </Group>
        ))}
      {d.updates && <UpdatesGroup d={d} />}
      {d.updates?.github && <GitHubGroup d={d} />}
      <HeavyJobsGroup host={d.host.name} api={api} focus={focus} />
      {d.repos === null && d.reposErr ? (
        <LoadError text={d.reposErr} onRetry={() => void d.loadRepos()} />
      ) : d.repos === null ? (
        <Skeleton rows={2} />
      ) : (
        <Group title="Merge without asking" label="Auto-merge" foot="In a repo that is on, an agent's merge ask merges once the checks pass on the head it asked for. A repo shows here once a thread has a pull request in it.">
          {repos.length > 8 && (
            <div class="sr srch">
              <input class="pq" type="search" placeholder={`Search ${repos.length} repos`} aria-label="Search repos" value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} />
            </div>
          )}
          {!repos.length && <Empty text="No repos yet. A repo shows here once a thread has a pull request in it." />}
          {shown.map((r) => (
            <Row key={r.repo} label title={r.repo} mono error={repoErr?.repo === r.repo ? repoErr.text : null} end={<Switch on={r.on} label={`Auto-merge ${r.repo}`} onChange={async (on) => { const e = await d.runRepo(r.repo, on); setRepoErr(e ? { repo: r.repo, text: e } : null); }} />} />
          ))}
          <RepoAdd run={d.runRepo} />
        </Group>
      )}
    </>
  );
}

/** Decision 0029 S1 (#185), V0.164: heavy jobs on this host: what runs undeclared now, the commands behind it that no
 *  pattern names yet, the patterns the owner added, when a thread counts as heavy, and the classes the host measured.
 *  Desktop and phone share it; a read-only front shows it all with its buttons disabled. */
function HeavyJobsGroup({ host, api, focus }: { host: string; api: SettingsProps["api"]; focus?: string }) {
  const env = useSet();
  const list = useLoad();
  const h = list.find((x) => x.host === host) ?? (list.length === 1 ? list[0] : undefined);
  if (!h?.provider) return null;
  return (
    <>
      <UndeclaredGroups h={h} host={host} api={api} focus={focus} />
      <MeasuredJobs h={h} host={host} api={api} env={env} />
    </>
  );
}

/** A command to the host, with the Saved mark and a fresh read of the load. */
function useHostCommand(host: string, api: SettingsProps["api"]): (action: string, args: Record<string, unknown>) => Promise<string | null> {
  const env = useSet();
  return async (action, args) => {
    const r = await api.command(action, undefined, withHost(args, host));
    if (r.ok) (env.saved(), refreshLoad());
    return r.ok ? null : (r.error ?? "That did not work");
  };
}

/** V0.164 (SPEC 8.5). A group is absent when it has nothing to show; the thresholds are absent on a host that does not
 *  send them. */
function UndeclaredGroups({ h, host, api, focus }: { h: HostLoad; host: string; api: SettingsProps["api"]; focus?: string }) {
  const env = useSet();
  const und = useUnd();
  const run = useHostCommand(host, api);
  const [err, setErr] = useState<{ key: string; text: string } | null>(null);
  const fail = (key: string) => (text: string | null) => setErr(text ? { key, text } : null);
  const act = async (key: string, action: string, args: Record<string, unknown>) => fail(key)(await run(action, args));
  // "Add as heavy job…" lands here: the suggestion of the strip's command is scrolled to and marked.
  useEffect(() => {
    if (focus) document.querySelector(`[data-sug="${CSS.escape(focus)}"]`)?.scrollIntoView({ block: "center" });
  }, [focus, h.suggestions?.length]);
  const now = und.now();
  const rows = undeclaredRows(h, now, und.lookup);
  const sugg = suggestionRows(h, und.lookup);
  const added = addedRows(h);
  const th = thresholdRows(h);
  return (
    <>
      {rows.length > 0 && (
        <Group label="Undeclared now" title="Undeclared now" foot="Threads that use clearly more than their idle level without having asked for a heavy job. Fyr tells the agent once and counts the use against this host's heavy jobs, so jobs that asked wait instead of overloading it. Only when memory is short does Fyr pause the newest one; nothing is killed.">
          {rows.map((r) => (
            <Row
              key={r.thread}
              class={`undrow${r.dismissed ? " off" : ""}`}
              title={
                <>
                  {!r.dismissed && <UndIcon size={13} />}
                  {r.title}
                </>
              }
              desc={
                <>
                  {r.project && `${r.project} · `}
                  <span class="mono">{r.command}</span> · {r.usage}
                  {r.tail && ` · ${r.tail}`}
                </>
              }
              error={err?.key === `u:${r.thread}` ? err.text : null}
              end={
                <>
                  <button type="button" class="btn" onClick={() => und.openThread(r.thread)}>
                    Open
                  </button>
                  <button type="button" class="btn" disabled={env.readOnly} onClick={() => void act(`u:${r.thread}`, "host.undeclaredDismiss", { thread: r.thread, on: !r.dismissed })}>
                    {r.dismissed ? "Show again" : "Dismiss"}
                  </button>
                </>
              }
            />
          ))}
        </Group>
      )}
      {sugg.length > 0 && (
        <Group label="Suggested heavy jobs" title="Suggested heavy jobs" foot="The commands behind undeclared work. Once added, Fyr refuses the command until its thread has asked for a heavy job, as it does for make test. Ignore hides a suggestion for good.">
          {sugg.map((s) => (
            <Suggestion key={s.id} s={s} picked={s.id === focus} error={err?.key === `s:${s.id}` ? err.text : null} onDo={(args) => act(`s:${s.id}`, "host.heavySuggestion", { id: s.id, ...args })} />
          ))}
        </Group>
      )}
      {added.length > 0 && (
        <Group label="Heavy jobs you added" title="Heavy jobs you added" foot="Besides the built-in ones (make test, go test ./..., playwright, nix build and the like).">
          {added.map((a) => (
            <Row
              key={a.pattern}
              title={a.pattern}
              mono
              desc={a.line}
              error={err?.key === `a:${a.pattern}` ? err.text : null}
              end={
                <button type="button" class="btn" disabled={env.readOnly} onClick={() => void act(`a:${a.pattern}`, "host.heavyRemove", { pattern: a.pattern })}>
                  Remove
                </button>
              }
            />
          ))}
        </Group>
      )}
      {th && (
        <Group label="When a thread counts as heavy" title="When a thread counts as heavy" foot="A thread's idle level is what its agent uses between jobs (the CLI and the servers it keeps running), learned per thread.">
          {th.map((r) => (
            <Row
              key={r.key}
              label
              title={r.title}
              desc={r.now}
              error={err?.key === `t:${r.key}` ? err.text : null}
              end={
                <>
                  <SaveField
                    cls="pq num"
                    numeric
                    value={r.value}
                    label={r.title}
                    check={(v) => thresholdError(parseThreshold(r.key, v))}
                    onError={(e) => fail(`t:${r.key}`)(e)}
                    onSave={(v) => {
                      const p = parseThreshold(r.key, v);
                      return thresholdError(p) ? Promise.resolve(thresholdError(p)) : run("host.undeclaredSettings", p as Record<string, number>);
                    }}
                  />
                  <span class="unit">{r.unit}</span>
                </>
              }
            />
          ))}
        </Group>
      )}
    </>
  );
}

/** One suggestion: the command, how often it was seen, the pattern the owner may edit, its kind, Add and Ignore. */
function Suggestion({ s, picked, error, onDo }: { s: ReturnType<typeof suggestionRows>[number]; picked: boolean; error: string | null; onDo(args: Record<string, unknown>): Promise<void> }) {
  const env = useSet();
  const [pattern, setPattern] = useState(s.pattern);
  const [kind, setKind] = useState(s.kind);
  const [busy, setBusy] = useState(false);
  const go = async (args: Record<string, unknown>) => {
    setBusy(true);
    await onDo(args);
    setBusy(false);
  };
  return (
    <Row col class={picked ? "pick" : undefined} data={{ sug: s.id }} error={error}>
      <span class="sr-b">
        <span class="cmd">{s.command}</span>
        <span class="sr-d">{s.line}</span>
      </span>
      <span class="inl">
        <input class="mono" type="text" value={pattern} aria-label={`Pattern for ${s.command}`} spellcheck={false} disabled={env.readOnly} onInput={(e) => setPattern((e.target as HTMLInputElement).value)} />
        <select value={kind} aria-label="Kind" disabled={env.readOnly} onChange={(e) => setKind((e.target as HTMLSelectElement).value)}>
          {["test", "build", "e2e"].map((k) => (
            <option key={k} value={k}>
              {k}
            </option>
          ))}
        </select>
        <button type="button" class="btn" disabled={env.readOnly || busy || !pattern.trim()} onClick={() => void go({ do: "add", pattern: pattern.trim(), kind })}>
          Add
        </button>
        <button type="button" class="btn" disabled={env.readOnly || busy} onClick={() => void go({ do: "ignore" })}>
          Ignore
        </button>
      </span>
    </Row>
  );
}

/** Decision 0029 S1 (#185): the heavy jobs this host measured, per class (project and command), the newest first. */
function MeasuredJobs({ h, host, api, env }: { h: HostLoad; host: string; api: SettingsProps["api"]; env: { readOnly: boolean } }) {
  const [err, setErr] = useState<{ class: string; text: string } | null>(null);
  const lines = jobLines(h);
  const budget = budgetLine(h); // V0.163: the budget as percentages of the host
  // Decision 0029 S3: Reset forgets a class's learned cost on this host (owner only); it counts as 1.0 again.
  const reset = async (c: string) => {
    const r = await api.command("host.jobReset", undefined, withHost({ class: c }, host));
    setErr(r.ok ? null : { class: c, text: r.error ?? "Not reset" });
    if (r.ok) refreshLoad();
  };
  return (
    <Group title="Heavy jobs" label="Heavy jobs" foot="What each kind of heavy job used the last time it ran, measured from its thread: its share of the CPU and the memory this host gives its agents. A job starts when both fit; a CPU-heavy job runs beside a memory-heavy one. Until three runs are learned, a job counts as half the host.">
      {budget && <Row title="Budget" desc={`${budget}${h.slots?.budgetWhy ? ` · ${h.slots.budgetWhy}` : ""}`} />}
      {!lines.length && <Empty text="No heavy job measured yet. A job is measured while it is admitted." />}
      {lines.slice(0, 12).map((l) => (
        <Row
          key={l.class}
          title={l.class}
          mono
          desc={l.cost ? `${l.text} · ${l.cost}` : l.text}
          error={err?.class === l.class ? err.text : null}
          end={
            l.learned && (
              <button type="button" class="btn" disabled={env.readOnly} aria-label={`Reset ${l.class}`} onClick={() => void reset(l.class)}>
                Reset
              </button>
            )
          }
        />
      ))}
    </Group>
  );
}

/** V0.126 (#151): what runs, Update now or Back to release, and the newest updates with their logs. Desktop and phone
 *  share it; the logs open in a new tab on both. */
function UpdatesGroup({ d }: { d: Data }) {
  const env = useSet();
  const [err, setErr] = useState<string | null>(null);
  const [all, setAll] = useState(false);
  const v = d.updates!;
  const now = Date.now();
  const list = all ? v.updates : v.updates.slice(0, 8);
  const press = v.paused ? "Back to release" : "Update now";
  return (
    <Group title="Updates" label="Updates" foot={v.bad?.commit ? `Release ${v.bad.commit.slice(0, 7)} rolled back here (${v.bad.why}); Fyr waits for a newer one.` : undefined}>
      <Row
        title={updateState(v)}
        error={err}
        end={
          <button type="button" class="btn" disabled={env.readOnly} onClick={async () => setErr(await d.runUpdateNow())}>
            {press}
          </button>
        }
      />
      {!v.updates.length && <Empty text="No updates yet on this host." />}
      {list.map((u) => {
        const r = updateRow(u, now);
        return (
          <Row
            key={`${u.build}-${u.startedAt}`}
            title={r.title}
            desc={
              <>
                {r.desc}
                {(u.logs ?? []).map((n) => (
                  <>
                    {" · "}
                    <a href={`/api/updates/${encodeURIComponent(u.build)}/${n}${d.host.name ? `?host=${encodeURIComponent(d.host.name)}` : ""}` /* V0.127: from its host */} target="_blank" rel="noopener noreferrer">
                      {n}
                    </a>
                  </>
                ))}
              </>
            }
            class={r.fail ? "bad" : undefined}
          />
        );
      })}
      {v.updates.length > 8 && (
        <Row end={<button type="button" class="btn" onClick={() => setAll(!all)}>{all ? "Fewer" : `All ${v.updates.length}`}</button>} />
      )}
    </Group>
  );
}

/** V0.173: the account's GitHub REST limit (every caller of the login shares it) and what this host's Fyr spent of it
 *  this hour, by kind. Desktop and phone share it. */
function GitHubGroup({ d }: { d: Data }) {
  const v = d.updates!.github!;
  const s = githubState(v);
  return (
    <Group title="GitHub API" label="GitHub API" foot="Every agent, every host's Fyr and your own gh share this limit. Fyr reads it every 10 minutes; a call answered 304 costs nothing.">
      <Row title={s.title} desc={s.desc} class={s.low ? "bad" : undefined} />
      {githubRows(v).map((r) => (
        <Row key={r.title} title={r.title} desc={r.desc} mono />
      ))}
    </Group>
  );
}

function RepoAdd({ run }: { run(repo: string, on: boolean): Promise<string | null> }) {
  const [err, setErr] = useState<string | null>(null);
  return (
    <Row error={err} class="fieldrow">
      <SaveField value="" label="Repo to add" placeholder="owner/repo" mono clears alwaysButton button="Add" onError={setErr} check={(v) => (repoKey(v) ? null : "Write it as owner/repo.")} onSave={(v) => run(repoKey(v), true)} />
    </Row>
  );
}

// ---- Projects ----

function projectsPage(p: SettingsProps, ui: PageUi): Omit<PageView, "title"> {
  return {
    end: <NewProjectButton ui={ui} n={settingsProjects(p.projects).length} />,
    body: <ProjectsPage p={p} ui={ui} />,
  };
}

function NewProjectButton({ ui, n }: { ui: PageUi; n: number }) {
  const env = useSet();
  if (ui.adding) return null;
  // The phone reorders in a mode: touch has no hover to reveal a grip, and a drag on a list fights the page's
  // scroll. The desktop drags the grip on each row instead, so it has no Reorder button.
  if (env.phone && ui.reordering)
    return (
      <button type="button" class="btn p" onClick={() => ui.setReordering(false)}>
        Done
      </button>
    );
  return (
    <>
      {env.phone && n > 1 && !ui.q.trim() && !env.readOnly && (
        <button type="button" class="btn reo" onClick={() => ui.setReordering(true)}>
          Reorder
        </button>
      )}
      <button type="button" class={`btn addp${ui.empty ? " p" : ""}`} disabled={env.readOnly} onClick={() => ui.setAdding(true)}>
        New project
      </button>
    </>
  );
}

// ---- Hosts (V0.125, decision 0026 slice 1) ----

/** One host in Settings → Hosts, the same row on the desktop and the phone (the phone's 56 px and larger type come from
 *  the one `.set.phone` rule set, for touch): the state mark, the name, one line (state, route, short id); a changed id
 *  replaces the line and is the only red row. The row opens the host's page, where every action is. */
function HostRow({ h, self, now, go, still }: { h: HostInfo; self: string; now: number; go(r: PageRef): void; /** the phone's Reorder mode: the row does not open */ still?: boolean }) {
  const line = hostLine(h, self, now);
  return (
    <Row
      class="host"
      onClick={still ? undefined : () => go({ page: "hosts", sub: h.name })}
      lead={<HostMark name={h.name} size={24} />}
      title={
        <>
          {h.name}
          {h.name === self && <span class="tag">This install</span>}
        </>
      }
      mono
      desc={h.idWas ? undefined : line}
      error={h.idWas ? line : null}
      data={{ host: h.name, sub: h.name, ...(h.id ? { id: h.id } : {}) }}
      end={
        <>
          {h.idWas && <span class="pst needs-login">Needs you</span>}
          <span class="chev">›</span>
        </>
      }
    />
  );
}

const HOSTS_FOOT = (
  <>
    Fyr finds your other installs on the tailnet by itself. A host it cannot see goes in fyrd's config (<span class="mono">hosts</span>).
  </>
);

/** The Hosts page: the hosts in the owner's order (V0.132). No buttons: today a host gets on the list
 *  by discovery or by the config, and the foot says so. */
/** V0.132: the owner orders the hosts with the one reorder control (V0.119 (4)): a grip on the desktop, a Reorder mode
 *  with ↑ and ↓ on the phone. The first is the default host. Each move saves at once (front.hostOrder, kept on every
 *  host) and shows on this device first. */
function HostsPage({ rows, self, now, go, ui, run }: { rows: HostInfo[]; self: string; now: number; go(r: PageRef): void; ui: PageUi; run(names: string[]): Promise<string | null> }) {
  const env = useSet();
  const [pending, setPending] = useState<string[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const names = rows.map((h) => h.name);
  useEffect(() => {
    if (pending && pending.join("\n") === names.join("\n")) setPending(null); // the hosts event caught up
  }, [names.join("\n")]);
  const shown = pending ? [...rows].sort((a, b) => pending.indexOf(a.name) - pending.indexOf(b.name)) : rows;
  const ids = shown.map((h) => h.name);
  const reorder = async (from: number, to: number) => {
    const next = move(ids, from, to);
    setPending(next);
    const e = await run(next);
    setErr(e);
    if (e) setPending(null);
    else env.saved();
  };
  const ro = useReorder(ids, (f, t) => void reorder(f, t));
  const can = rows.length > 1 && !env.readOnly;
  const mode = env.phone && ui.reordering && can;
  return (
    <Group
      label="Hosts"
      foot={
        <>
          {rows.length <= 1 && "This is your only Fyr install. "}
          {rows.length > 1 && "The first host is the default for New project, New thread and the folder picker. This order is kept on your hosts, so every device shows it. "}
          {HOSTS_FOOT}
        </>
      }
    >
      {err && (
        <div class="seterr" role="alert">
          {err}
        </div>
      )}
      {shown.map((h, i) => {
        if (mode)
          return (
            <div key={h.name} class="hreo mvrow" data-sub={h.name}>
              <HostRow h={h} self={self} now={now} go={go} still />
              <MoveButtons name={h.name} i={i} n={ids.length} onMove={(f, t) => void reorder(f, t)} />
            </div>
          );
        if (!can || env.phone) return <HostRow key={h.name} h={h} self={self} now={now} go={go} />;
        const r = ro.row(i);
        return (
          <div key={h.name} class={`hreo g ${r.class as string}`} ref={r.ref as (el: HTMLElement | null) => void}>
            <Grip {...ro.grip(i, h.name)} />
            <HostRow h={h} self={self} now={now} go={go} />
          </div>
        );
      })}
    </Group>
  );
}

/** The phone's Reorder and Done for the Hosts page (touch has no hover for a grip, and a drag fights the scroll). */
function HostsReorderButton({ ui, n }: { ui: PageUi; n: number }) {
  const env = useSet();
  if (!env.phone || n < 2 || env.readOnly) return null;
  return ui.reordering ? (
    <button type="button" class="btn p" onClick={() => ui.setReordering(false)}>
      Done
    </button>
  ) : (
    <button type="button" class="btn reo" onClick={() => ui.setReordering(true)}>
      Reorder
    </button>
  );
}

/** The status pill at the end of a host's title (desktop) or first on its page (phone: its header has only the title
 *  and Back). */
function HostPill({ h }: { h: HostInfo }) {
  if (h.idWas) return <span class="pst needs-login">Needs you</span>;
  return <span class="pst">{h.online ? "Online" : "Offline"}</span>;
}

/** An install id in full: four groups, the first (the short form) in ink. */
function IdFull({ id, big }: { id: string; big?: boolean }) {
  return (
    <span class={`idf${big ? " big" : ""}`} aria-label={`id ${id}`}>
      {id.split("-").map((g, i) => (
        <span key={i} class={i ? undefined : "s"}>
          {g}
        </span>
      ))}
    </span>
  );
}

/** Copy writes the id with its dashes; "Copied" for 2 s. Not rendered where the browser has no clipboard API. */
function CopyId({ id }: { id: string }) {
  const [done, setDone] = useState(false);
  useEffect(() => {
    if (!done) return;
    const t = setTimeout(() => setDone(false), 2000);
    return () => clearTimeout(t);
  }, [done]);
  if (typeof navigator === "undefined" || !navigator.clipboard?.writeText) return null;
  return (
    <button type="button" class="btn" aria-label={`Copy the id ${id}`} onClick={() => void navigator.clipboard.writeText(id).then(() => setDone(true), () => {})}>
      {done ? "Copied" : "Copy"}
    </button>
  );
}

/** A row whose text is a label, an id in full and a line under it (the id rows of a host's page). */
function IdRow({ title, id, desc, end, bad }: { title: string; id: string; desc: ComponentChildren; end?: ComponentChildren; bad?: boolean }) {
  return (
    <div class={`sr idr${bad ? " bad" : ""}`}>
      <span class="sr-b">
        <span class="sr-t">{title}</span>
        <IdFull id={id} big />
        <span class="sr-d">{desc}</span>
      </span>
      {end && <span class="sr-e">{end}</span>}
    </div>
  );
}

const sentence = (s: string) => {
  const t = s.trim();
  if (!t) return "";
  return `${t[0].toUpperCase()}${t.slice(1)}${/[.!?]$/.test(t) ? "" : "."}`;
};

/** A host's page: Identity (the full id, Copy, how Fyr trusts it today) and Connection (state, address and route, how
 *  it got on the list). A changed id: the note, both ids in full and Trust <new> (front.hostId); the foot says what
 *  Fyr does meanwhile. The same page on the desktop and the phone. */
const GLYPH_WORDS: Record<string, string> = { laptop: "a laptop (it has a battery)", monitor: "a desktop", server: "a server" };

/** V0.171: the host's mark, set by the owner: an icon (Automatic, one of the glyphs, or an emoji) and a colour
 *  (Automatic or one of the ten hues). Kept by its install id on every host (front.hostMark); the same group on the
 *  desktop and the phone. */
function HostMarkGroup({ h, api }: { h: HostInfo; api: SettingsProps["api"] }) {
  const env = useSet();
  const marks = useHostMarks();
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  if (!h.id)
    return (
      <Group title="Mark">
        <Row title={<HostMark name={h.name} size={32} />} desc="Automatic. This host runs a Fyr older than V0.124 and has no install id, so its mark cannot be set until its next update." />
      </Group>
    );
  const own = marks[h.id] ?? {};
  const icon = ownerIcon(own.icon);
  const auto = hostGlyph(h.machine);
  const set = async (patch: { icon?: string; color?: string }) => {
    const next = { icon: own.icon ?? "", color: own.color ?? "", ...patch };
    setBusy(true);
    const r = await api.command("front.hostMark", undefined, { id: h.id, ...next });
    setBusy(false);
    if (r.ok) {
      setErr(null);
      env.saved();
    } else setErr(r.error || "the mark could not be saved");
  };
  const off = env.readOnly || busy;
  return (
    <Group title="Mark" foot="Kept on every host by its id, so every device shows it.">
      <Row
        class="hmprev"
        title={<HostMark name={h.name} size={32} />}
        desc={!own.icon && !own.color ? `Automatic: ${GLYPH_WORDS[auto]}, a colour from its id.` : `Set by you.${!own.icon ? ` The icon is automatic: ${GLYPH_WORDS[auto]}.` : ""}${!own.color ? " The colour is automatic." : ""}`}
        error={err}
      />
      <div class="sr col hmicon">
        <span class="sr-t">Icon</span>
        <div class="igrid" role="radiogroup" aria-label="Icon">
          <button type="button" class={`ig auto${!icon ? " on" : ""}`} role="radio" aria-checked={!icon} title="Automatic" aria-label="Automatic" disabled={off} onClick={() => icon && void set({ icon: "" })}>
            Auto
          </button>
          {HOST_ICON_NAMES.map((nm) => {
            const on = !!icon && "glyph" in icon && icon.glyph === nm;
            return (
              <button type="button" class={`ig${on ? " on" : ""}`} role="radio" aria-checked={on} title={nm} aria-label={nm} disabled={off} onClick={() => !on && void set({ icon: nm })}>
                <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" dangerouslySetInnerHTML={{ __html: HOST_GLYPHS[nm] }} />
              </button>
            );
          })}
        </div>
        <span class="pemoji">
          <SaveField value={icon && "emoji" in icon ? icon.emoji : ""} label="Emoji" placeholder="Or an emoji" onError={setErr} check={(v) => (v.trim() && !ownerIcon(v.trim()) ? "One emoji" : null)} onSave={async (v) => {
              // Empty gives the automatic icon back; otherwise the first emoji typed.
              const i = ownerIcon(v.trim());
              await set({ icon: i && "emoji" in i ? i.emoji : "" });
              return null;
            }} />
        </span>
      </div>
      <Row
        title="Colour"
        stack
        end={
          <div class="swatches" role="radiogroup" aria-label="Colour">
            <button type="button" class={`sw auto${!own.color ? " on" : ""}`} role="radio" aria-checked={!own.color} aria-label="Automatic" title="Automatic" disabled={off} onClick={() => own.color && void set({ color: "" })}>
              A
            </button>
            {PALETTE_NAMES.map((c) => (
              <button type="button" class={`sw${c === own.color ? " on" : ""}`} role="radio" aria-checked={c === own.color} aria-label={c} title={c} style={{ "--pc": `var(--p-${c})` }} disabled={off} onClick={() => c !== own.color && void set({ color: c })} />
            ))}
          </div>
        }
      />
    </Group>
  );
}

function HostPage({ h, self, api, now }: { h: HostInfo; self: string; api: SettingsProps["api"]; now: number }) {
  const env = useSet();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const isSelf = h.name === self;
  const route = routeOf(h.url, isSelf, h.found);
  const trust = async () => {
    setBusy(true);
    const r = await api.command("front.hostId", undefined, { host: h.name });
    setBusy(false);
    if (r.ok) {
      setErr(null);
      env.saved(); // the hosts stream event makes this page the ordinary one
    } else setErr(r.error || "the id could not be saved");
  };
  const state = hostState({ ...h, deploy: undefined }, now);
  const TRUST_ROW = !isSelf && <Row title="Trust" desc="Through your tailnet: Fyr follows it because it is a node of your Tailscale login, not because of its key." />;
  const identity =
    h.idWas && h.id ? (
      <Group title="Identity" foot={`Fyr still follows ${h.name} meanwhile: today it trusts a host through your tailnet login, not its key. If you do not know this install, take that machine off your tailnet.`}>
        <IdRow title="It was" id={h.idWas} desc={`The id Fyr first saw for ${h.name}.`} />
        <IdRow title="It is now" id={h.id} bad desc={`To check, open Fyr on ${h.name}: its Hosts page shows its own id.`} end={<CopyId id={h.id} />} />
        <Row
          class="trust"
          title={`Trust ${shortID(h.id)} as ${h.name}`}
          desc={err ? undefined : "Fyr then remembers the new id and stops warning."}
          error={err ? `Not trusted: ${self} answered “${err}”. Nothing changed.` : null}
          end={
            <button type="button" class="btn" disabled={env.readOnly || busy} onClick={() => void trust()}>
              Trust {shortID(h.id)}
            </button>
          }
        />
      </Group>
    ) : (
      <Group title="Identity">
        {h.id ? (
          <IdRow
            title="Id"
            id={h.id}
            desc={isSelf ? `The id of the Fyr you are using. Your other installs show this id for ${h.name}.` : "Fyr remembers this id and tells you when another install answers under this name."}
            end={<CopyId id={h.id} />}
          />
        ) : (
          <Row title="Id" desc="None. This host runs a Fyr older than V0.124, which has no install key. It shows an id after its next update." />
        )}
        {TRUST_ROW}
      </Group>
    );
  const under = !h.online ? (
    <>
      {h.error && <span class="err">{sentence(h.error)} </span>}
      Its threads show as last read and cannot be changed.
    </>
  ) : h.deploy && updateWords(h) ? (
    `Updating Fyr: ${updateWords(h)}`
  ) : undefined;
  return (
    <>
      {h.idWas && h.id && (
        <div class="note bad" role="alert">
          <span>
            <b>Another install answers as {h.name}.</b> That is normal after you reinstalled or replaced {h.name}. It is wrong if you did not.
          </span>
        </div>
      )}
      {identity}
      {/* V0.171: after Identity, so a changed id and its Trust stay first. */}
      <HostMarkGroup h={h} api={api} />
      <Group title="Connection">
        <Row class="hstate" title={state[0].toUpperCase() + state.slice(1)} desc={under} />
        {h.url && (
          <Row
            title="Address"
            desc={
              <>
                <span class="mono">{h.url}</span>
                {route && ` · ${route}`}
              </>
            }
          />
        )}
        {!isSelf &&
          (h.found ? (
            <Row title="Found on the tailnet" desc="Fyr found it by itself. It leaves this list 30 days after its last answer." />
          ) : (
            <Row
              title="Set in this front's config"
              desc={
                <>
                  It stays on this list until it is taken out of <span class="mono">hosts</span> there.
                </>
              }
            />
          ))}
      </Group>
    </>
  );
}

/** V0.58: every project with its icon, folder and thread count; a row opens the project's page. Search from nine
 *  projects. V0.121: in the owner's one order, which this page sets with the one reorder control (a grip on the
 *  desktop, a Reorder mode with ↑ and ↓ on the phone); quiet projects are listed and keep their place. V0.125: the hosts have their own page. */
function ProjectsPage({ p, ui }: { p: SettingsProps; ui: PageUi }) {
  const { adding, setAdding, q, setQ } = ui;
  const env = useSet();
  const all = settingsProjects(p.projects);
  const list = searchProjects(all, q);
  const ids = all.map((x) => x.id!);
  const [err, setErr] = useState<string | null>(null);
  const reorder = async (from: number, to: number) => {
    const e = await p.projectActions.order(move(ids, from, to));
    setErr(e);
    if (!e) env.saved();
  };
  const ro = useReorder(ids, (f, t) => void reorder(f, t));
  const mode = env.phone && ui.reordering;
  const grips = !env.phone && !q.trim() && ids.length > 1 && !env.readOnly; // while the search has text, reorder is off
  return (
    <>
      {adding ? (
        <Group title="New project">
          <Row col>
            <NewProjectStep
              initialName=""
              initialFolder=""
              fromThread={false}
              onCancel={() => setAdding(false)}
              onCreate={async (name, folder, host) => {
                const e = await p.projectActions.create(name, folder, host);
                if (!e) setAdding(false);
                return e;
              }}
            />
          </Row>
        </Group>
      ) : (
        <section class="sg" aria-label="Projects">
          {all.length >= 9 && !mode && <input class="pq" type="search" placeholder={`Search ${all.length} projects`} aria-label="Search projects" value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} />}
          {err && (
            <div class="seterr" role="alert">
              {err}
            </div>
          )}
          {all.length === 0 ? (
            <div class="sg-c">
              <Empty title="No projects yet" text="A project gathers the threads of one folder. Fyr makes one when you start a thread in a new folder." />
            </div>
          ) : (
            <div class="pls setpl">
              {list.map((x) => {
                const quiet = quietNote(x, p.now);
                const i = ids.indexOf(x.id!);
                const body = (
                  <>
                    <ProjectIcon project={x} name={x.name} size={28} />
                    <span class="plt">
                      <span class="ell pln">{x.name}</span>
                      <span class="ell pls2">
                        {quiet ? `${quiet} · ` : ""}
                        {projectSubtitle(x)}
                      </span>
                    </span>
                  </>
                );
                const style = { "--pc": colorVar(x.color, x.name) };
                if (mode)
                  return (
                    <div key={x.id} class="pli mvrow" data-sub={x.id} style={style}>
                      {body}
                      <MoveButtons name={x.name} i={i} n={ids.length} onMove={(f, t) => void reorder(f, t)} />
                    </div>
                  );
                const go = () => p.go({ page: "projects", sub: x.id! });
                const open = (
                  <button type="button" data-sub={x.id} class={grips ? "plb" : "pli"} style={grips ? undefined : style} onClick={grips ? undefined : go}>
                    {body}
                    <span class="plc" title={`${x.threads ?? 0} threads`}>
                      {x.threads ?? 0}
                    </span>
                  </button>
                );
                if (!grips) return <Fragment key={x.id}>{open}</Fragment>;
                const r = ro.row(i);
                return (
                  // The whole row opens the project, as without a grip; the grip only moves it.
                  <div key={x.id} class={`pli g ${r.class as string}`} ref={r.ref as (el: HTMLElement | null) => void} style={style} onClick={(e) => !(e.target as Element).closest(".grip") && go()}>
                    <Grip {...ro.grip(i, x.name)} />
                    {open}
                  </div>
                );
              })}
              {!list.length && <div class="plempty">No project matches “{q}”.</div>}
            </div>
          )}
          {q.trim() ? <div class="sg-f">{projectSearchLine(list.length, all.length)}</div> : all.length > 1 && <div class="sg-f">This order is used in New thread, the project chips and every list of projects, on all your devices.</div>}
        </section>
      )}
    </>
  );
}

// ---- This device ----

function useToolDetailsPref(): boolean {
  const [on, setOn] = useState(toolDetails());
  useEffect(() => onToolDetails(setOn), []);
  return on;
}

/** Notifications (V0.15), Theme, Show tool details, Install: kept in this browser. Never read-only: none of it goes to
 *  the host except the push subscription, which says so when it fails. */
function DevicePage({ d, user }: { d: SettingsDevice; user: string }) {
  const [tested, setTested] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const details = useToolDetailsPref();
  const row = pushRow(d.push, d.pushReason, tested);
  useEffect(() => {
    if (!tested) return;
    const t = setTimeout(() => setTested(false), 5000);
    return () => clearTimeout(t);
  }, [tested]);
  const toggle = async (on: boolean) => {
    setBusy(true);
    setErr(await d.setPush(on));
    setBusy(false);
  };
  const test = async () => {
    setBusy(true);
    const e = await d.testPush();
    setBusy(false);
    setErr(e);
    setTested(!e);
  };
  return (
    <SetContext.Provider value={{ ...useSet(), readOnly: false }}>
      <Group label="This device">
        <Row
          label={!row.disabled}
          title="Notifications"
          desc={row.error ? undefined : row.desc}
          error={err ?? (row.error ? row.desc : null)}
          class="push"
          end={
            <>
              {row.test && (
                <button type="button" class="btn" disabled={busy} onClick={(e) => (e.preventDefault(), void test())}>
                  Send test
                </button>
              )}
              <Switch on={row.on} label="Notifications" disabled={row.disabled || busy} onChange={(on) => void toggle(on)} />
            </>
          }
        />
        <Row
          title="Theme"
          stack
          end={
            <Seg
              label="Theme"
              value={d.theme}
              options={[
                { value: "system", label: "Auto" },
                { value: "light", label: "Light" },
                { value: "dark", label: "Dark" },
              ]}
              onChange={d.setTheme}
            />
          }
        />
        <Row label title="Show tool details" desc="Every tool group open, every machine line in full." end={<Switch on={details} label="Show tool details" onChange={setToolDetails} />} />
        {/* V0.125: how this browser reaches this Fyr; the device list replaces it in slice 5 of decision 0026 */}
        <Row class="howin" title="How this browser gets in" desc={howIn(typeof location === "undefined" ? "" : location.hostname, user)} />
        {d.canInstall && (
          <Row
            title="Install Fyr"
            desc="As an app with its own window."
            end={
              <button type="button" class="btn" onClick={d.onInstall}>
                Install
              </button>
            }
          />
        )}
      </Group>
    </SetContext.Provider>
  );
}
