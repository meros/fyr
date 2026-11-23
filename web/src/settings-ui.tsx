// SPEC V0.119 (amends V0.50), V0.125: Settings is five pages behind one nav: Providers (with System agents),
// Automation (Auto-deploy, Auto-merge), Projects, Hosts and This device (Notifications, Theme, Show tool details,
// Install, How this browser gets in). A provider, Add provider, a project and a host are sub-pages. Every change saves at once. The same pages on the desktop (a
// dialog: the nav is its left column, the page its right pane) and the phone (the nav is the Settings screen, a page
// is the next screen); the reason for the difference is width.

import { AutomationPage } from "./settings-automation.tsx";
import { HostEnv, AddProvider, ProviderBadge, ProviderPage, HostParts, ProvidersPage } from "./settings-providers.tsx";
import { HostPill, HostPage, HostsPage, HostsReorderButton } from "./settings-hosts.tsx";
import { projectsPage } from "./settings-projects.tsx";
import { automationBadge, type HostTabItem } from "./host-tabs.ts";
import { HostTabs } from "./host-tabs-ui.tsx";
import { useEffect, useRef, useState } from "preact/hooks";
import { type ComponentChildren } from "preact";
import { Boundary } from "./boundary.tsx";
import type { Api } from "./api.ts";
import { howIn, profilesOf, updateLine, type HostInfo, type HostProfiles } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import { resolveRef } from "./store.ts";
import { automationSummaryHosts, readOnlyNote } from "./host-settings.ts";
import { type HostLoad } from "./host-load.ts";
import { useLoad } from "./load-ui.tsx";
import type { Project } from "./types.ts";
import type { Theme } from "./inbox.tsx";
import { findProject, projectRecords, recordOf, settingsProjects } from "./projects.ts";
import { FINISHED_DESC, QUOTA_RESET_DESC, pushRow, type PushPrefs, type PushStatus } from "./push.ts";
import { isNativePush, openNativeNotificationSettings } from "./pwa.ts";
import { onToolDetails, setToolDetails, toolDetails } from "./prefs.ts";
import { DevicesPage, useAuthState } from "./devices-ui.tsx";
import { SetupPage } from "./setup-ui.tsx";
import { setupBadge, setupSummary, type SetupReport } from "./setup.ts";
import { hiddenCount, hostIsHidden, lastShownHost, projectIsHidden, setHostShown, setProjectShown, type Hidden } from "./hidden.ts";
import { PeersPanel, usePeerState } from "./peers-ui.tsx";
import { devicesNeed, devicesSummary } from "./devices.ts";
import { PAGES, deviceSummary, hostsNeed, hostsSummary, navBadge, projectsSummary, providersSummary, type PageId, type PageRef } from "./settings-nav.ts";
import { Empty, Group, LoadError, Row, SavedMark, Seg, SetContext, Skeleton, Switch, useSaved, useSet } from "./settings-rows.tsx";
import { type ProjectPatch, ProjectEditor } from "./project-editor.tsx";
import { linkingFor } from "./project-linking.tsx";
import { type AllSettings, useSettingsData } from "./settings-load.ts";


/** This device: kept in this browser. */
export interface SettingsDevice {
  push: PushStatus;
  pushReason: string | null;
  /** turn notifications on or off; the error, or null */
  setPush(on: boolean): Promise<string | null>;
  testPush(): Promise<string | null>;
  /** V0.notification-polish: what this device is told about */
  pushPrefs: PushPrefs;
  setPushPrefs(p: PushPrefs): void;
  theme: Theme;
  setTheme(t: Theme): void;
  canInstall: boolean;
  onInstall(): void;
  /** V0.207: the hosts and projects this device never shows */
  hidden: Hidden;
  setHidden(h: Hidden): void;
}

interface ProjectActions {
  update(id: string, patch: ProjectPatch): Promise<string | null>;
  setFolder(id: string, path: string): Promise<string | null>;
  delete(id: string): Promise<string | null>;
  /** New project without a thread; the error, or null. V0.132: on `host` (several hosts) */
  create(name: string, folder: string, host: HostId): Promise<string | null>;
  /** V0.121: the owner's project order (front.projectOrder); the error, or null */
  order(ids: string[]): Promise<string | null>;
  /** Decision 0034: linked projects; the error, or null */
  link(id: string, other: string): Promise<string | null>;
  unlink(id: string): Promise<string | null>;
  add(id: string, host: HostId, folder: string): Promise<string | null>;
}

export interface SettingsProps {
  api: Pick<Api, "providers" | "providersFound" | "command" | "frontCommand"> & Partial<Pick<Api, "autoMerge" | "autoDeploy" | "updates" | "setup" | "systemAgents" | "authState" | "authAct" | "peerState" | "peerAct">>;
  /** decision 0031 2.6: the name of the front this browser is connected through, for This device only */
  front: string;
  /** V0.51: the front's hosts with their state, the front's own among them */
  hosts?: HostInfo[];
  /** V0.125: the state's user, for How this browser gets in */
  user?: string;
  phone?: boolean;
  /** the page shown; null is the phone's nav screen (the desktop always shows a page) */
  at: PageRef | null;
  /** open a page (null: the phone's nav) */
  go(r: PageRef | null): void;
  /** replace the current page in history (phone: switching host tab on a host sub-page keeps Back pointing at Hosts) */
  replace?(r: PageRef | null): void;
  /** phone: the screen's header (title, and what sits at its right) */
  header?(title: ComponentChildren, end?: ComponentChildren): ComponentChildren;
  /** no connection to the host: what was last read shows and nothing can be changed */
  offline?: boolean;
  /** the providers changed: reload the state's profiles */
  onChanged?(): void;
  device: SettingsDevice;
  projects: Project[];
  /** slice 2b: each host's profiles; a project's editor offers its own host's */
  profilesByHost: HostProfiles;
  /** the host's clock */
  now: number;
  /** the first state has arrived, so `now` is the host's clock (V0.120: the page's check on open waits for it) */
  clockKnown?: boolean;
  projectActions: ProjectActions;
  /** Decision 0037: open a newly created first thread from the Setup wizard */
  onOpenThread?(id: string): void;
}

const OFFLINE = () => `No connection to Fyr. Settings show what was last read and cannot be changed until it is back.`;

// ---- the shell ----

export function Settings(p: SettingsProps) {
  const sv = useSaved();
  const data = useSettingsData(p, sv.mark);
  const peers = usePeerState(p.api, p.at?.page === "hosts"); // V0.159: the installs panel of Hosts
  const auth = useAuthState(p.api, true); // V0.157: Devices, read by the page and by the nav's line
  const [setupRep, setSetupRep] = useState<SetupReport | null>(null);
  const setupHostId = (p.hosts ?? []).find((h) => h.online && h.id)?.id;
  useEffect(() => {
    if (!setupHostId || !p.api.setup) return;
    let alive = true;
    void p.api.setup(setupHostId).then((r) => alive && r.ok && setSetupRep(r.data));
    return () => {
      alive = false;
    };
  }, [p.api, setupHostId, p.at?.page]);
  const env = { readOnly: !!p.offline, phone: !!p.phone, saved: sv.mark };
  // V0.127: the nav's lines count every host; an offline host Fyr has never read is left out.
  // A host with no install id (older than V0.124) cannot be addressed (decision 0031): nothing is read from it.
  const read = data.all.filter((d) => (d.host.online || d.providers !== null) && !d.host.noId);
  const provs = read.some((d) => d.providers === null) ? null : read.flatMap((d) => d.providers!);
  const blocked = data.all.reduce((n, d) => n + (d.sa?.jobs.filter((j) => j.error).length ?? 0), 0);
  const summaries: Record<PageId, string> = {
    providers: providersSummary(provs, blocked),
    automation: automationSummaryHosts(data.all.map((d) => ({ deploy: p.api.autoDeploy ? d.deploy : false, repos: d.repos, skip: (!d.host.online && d.repos === null) || !!d.host.noId }))),
    projects: projectsSummary(settingsProjects(p.projects).length),
    hosts: hostsSummary(p.hosts),
    devices: devicesSummary(auth.s),
    device: deviceSummary(p.device.push, p.device.theme, hiddenCount(p.device.hidden)),
    setup: setupSummary(setupRep),
  };
  const badges: Partial<Record<PageId, number>> = { providers: navBadge(provs, blocked), hosts: hostsNeed(p.hosts), devices: devicesNeed(auth.s), setup: setupBadge(setupRep) };
  const [adding, setAdding] = useState(false);
  const [q, setQ] = useState("");
  const [reordering, setReordering] = useState(false);
  useEffect(() => (setAdding(false), setReordering(false), setQ("")), [p.at?.page]);
  // V0.host-tabs: remember the picked host across Providers, Automation, Setup and Hosts so switching Settings pages
  // keeps working on the same host, and returning from a provider sub-page stays on its host tab.
  const [hostPick, setHostPick] = useState<string | null>(() => p.at?.host ?? (p.at?.page === "hosts" ? (p.at.sub ?? null) : null));
  useEffect(() => {
    if (p.at?.host) setHostPick(p.at.host);
    else if (p.at?.page === "hosts" && p.at.sub) setHostPick(p.at.sub);
  }, [p.at?.page, p.at?.host, p.at?.sub]);
  const loadByHost = new Map(useLoad().map((h) => [h.host, h]));
  const ui: PageUi = { adding, setAdding, empty: settingsProjects(p.projects).length === 0, q, setQ, reordering, setReordering, hostPick, setHostPick, loadByHost };
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
        {p.offline && <div class="m-ban">{OFFLINE()}</div>}
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
          {p.offline && <div class="m-ban d">{OFFLINE()}</div>}
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
export const subKey = (r: PageRef) => (r.host ? `${r.host}/${r.sub ?? ""}` : (r.sub ?? ""));

/** The five pages: a title and a line of state each, and the amber count on a page that needs the owner (Providers,
 *  V0.125 Hosts). Desktop: the dialog's left
 *  column; ↑ and ↓ move between pages and the page follows the focus. Phone: the Settings screen; the chevron shows
 *  there only, because there the row leaves the screen. */
function SettingsNav({ phone, active, summaries, badges, onPick }: { phone: boolean; active: PageId | null; summaries: Record<PageId, string>; badges: Partial<Record<PageId, number>>; onPick(id: PageId, keys?: boolean): void }) {
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

export interface PageView {
  title: ComponentChildren;
  /** the sub-page's parent, for the desktop crumb */
  parent?: string;
  /** at the end of the title line (desktop); first on the page (phone) */
  end?: ComponentChildren;
  lede?: string;
  body: ComponentChildren;
}

/** Page state the shell keeps, so a page's title line and its body agree (New project). */
export interface PageUi {
  adding: boolean;
  setAdding(b: boolean): void;
  empty: boolean;
  /** V0.121: the Projects page's search and the phone's Reorder mode, kept here because the header holds Reorder */
  q: string;
  setQ(q: string): void;
  reordering: boolean;
  setReordering(b: boolean): void;
  /** V0.host-tabs: active host tab shared across multi-host Settings pages */
  hostPick: string | null;
  setHostPick(h: string): void;
  loadByHost: Map<string, HostLoad>;
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
              <AddProvider host={d.host.id} onCancel={() => go({ page: "providers" })} onCreate={async (args) => { const e = await d.runProvider("provider.create", args); if (!e) go({ page: "providers" }); return e; }} />
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
      return {
        title: "Providers",
        body: (
          <HostParts
            s={s}
            now={p.now}
            picked={ui.hostPick}
            onPick={ui.setHostPick}
            badge={(d) => navBadge(d.providers, d.sa?.jobs.filter((j) => j.error).length ?? 0)}
            part={(d) => <ProvidersPage d={d} go={go} grouped={s.grouped} />}
          />
        ),
      };
    }
    case "automation":
      return {
        title: "Automation",
        lede: "What Fyr does without your press. Only you change these; an agent cannot.",
        body: (
          <HostParts
            s={s}
            now={p.now}
            picked={ui.hostPick}
            onPick={ui.setHostPick}
            badge={(d) => automationBadge(d.updates, ui.loadByHost.get(d.host.name))}
            part={(d) => <AutomationPage d={d} hasDeploy={!!p.api.autoDeploy} api={p.api} focus={at.sub} />}
          />
        ),
      };
    case "projects": {
      if (at.sub) {
        const rid = resolveRef(projectRecords(p.projects), at.sub!); // a link names the host's own id (C12)
        const pr = recordOf(p.projects, rid); // decision 0034: a member of a linked project has its own page
        const group = findProject(p.projects, rid);
        return {
          title: pr?.name ?? "Project",
          parent: "Projects",
          body: pr ? (
            <ProjectEditor
              key={pr.id}
              project={pr}
              linking={group && linkingFor(group, p.projects, p.projectActions, (id) => go({ page: "projects", sub: id }))}
              profiles={profilesOf(pr, p.profilesByHost) /* V0.127: its host's providers */}
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
      const rows = p.hosts ?? [];
      const h = at.sub ? rows.find((x) => x.name === at.sub) : undefined;
      // An unknown name shows the Hosts page.
      if (h) {
        const hostTabs: HostTabItem[] = rows.map((x) => ({
          id: x.name,
          name: x.name,
          label: x.name,
          offline: !x.online,
          badge: (x.idWas ? 1 : 0) + (updateLine(x).bad ? 1 : 0),
        }));
        return {
          title: <span class="mono">{h.name}</span>,
          parent: "Hosts",
          end: <HostPill h={h} />,
          body: (
            <>
              <HostTabs
                items={hostTabs}
                active={h.name}
                label="Hosts"
                onPick={(name) => {
                  ui.setHostPick(name);
                  (p.replace ?? go)({ page: "hosts", sub: name });
                }}
              />
              <HostPage key={h.name} h={h} api={p.api} now={p.now} />
            </>
          ),
        };
      }
      const runOrder = async (names: string[]) => {
        const r = await p.api.frontCommand("front.hostOrder", { ids: names.flatMap((n) => rows.find((x) => x.name === n)?.id ?? []) }); // V0.132: the rows as moved, by id
        return r.ok ? null : (r.error ?? "That did not work");
      };
      return {
        title: "Hosts",
        end: <HostsReorderButton ui={ui} n={rows.length} />,
        body: (
          <>
            <HostsPage rows={rows} now={p.now} go={go} ui={ui} run={runOrder} />
            {p.api.peerState && p.api.peerAct && <PeersPanel api={{ peerState: p.api.peerState, peerAct: p.api.peerAct }} st={peers} />}
          </>
        ),
      };
    }
    case "devices":
      if (!p.api.authState || !p.api.authAct) return { title: "Devices", body: <Empty text="This host cannot list devices." /> };
      return { title: "Devices", lede: "Who is signed in to this install, and how a new device gets in.", body: <DevicesPage api={{ authState: p.api.authState, authAct: p.api.authAct }} now={p.now} st={auth} /> };
    case "device":
      return { title: "This device", lede: "These are kept in this browser. Each device has its own.", body: <DevicePage d={p.device} user={p.user ?? ""} front={p.front} hosts={p.hosts ?? []} projects={p.projects} /> };
    case "setup":
      return {
        title: "Setup",
        lede: "Checked live from the host, never a stored done flag.",
        body: (
          <HostParts
            s={s}
            now={p.now}
            picked={ui.hostPick}
            onPick={ui.setHostPick}
            onlineOnly
            part={(d) => <SetupPage key={d.host.id ?? d.host.name} api={p.api} host={d.host.id} hostName={s.grouped ? d.host.name : undefined} go={go} onOpenThread={p.onOpenThread} />}
          />
        ),
      };
  }
}

// ---- This device ----

function useToolDetailsPref(): boolean {
  const [on, setOn] = useState(toolDetails());
  useEffect(() => onToolDetails(setOn), []);
  return on;
}

/** V0.207: which hosts and projects this device shows. One list on the desktop and the phone. */
function ShownHere({ d, hosts, projects }: { d: SettingsDevice; hosts: HostInfo[]; projects: Project[] }) {
  const list = settingsProjects(projects).filter((x) => x.id);
  if (hosts.length < 2 && !list.length) return null;
  return (
    <Group title="Shown on this device" foot="What you turn off leaves this device's inbox, filters, Changes, pickers and notifications. It keeps running, and your other devices still show it.">
      {hosts.length > 1 &&
        hosts.map((h) => {
          const label = `Show host ${h.name}`;
          return <Row key={h.name} label title={h.name} desc="Host" ell end={<Switch on={!hostIsHidden(h, d.hidden)} label={label} disabled={lastShownHost(h, hosts, d.hidden)} onChange={(on) => d.setHidden(setHostShown(d.hidden, h, on))} />} />;
        })}
      {list.map((x) => (
        <Row key={x.id} label title={x.name} desc={x.members ? `Project on ${x.members.length} hosts` : hosts.length > 1 && x.host ? `Project on ${x.host}` : "Project"} ell end={<Switch on={!projectIsHidden(x, d.hidden)} label={`Show project ${x.name}`} onChange={(on) => d.setHidden(setProjectShown(d.hidden, x, on))} />} />
      ))}
    </Group>
  );
}

/** Notifications (V0.15), Theme, Show tool details, Install: kept in this browser. Never read-only: none of it goes to
 *  the host except the push subscription, which says so when it fails. */
function DevicePage({ d, user, front, hosts, projects }: { d: SettingsDevice; user: string; front: string; hosts: HostInfo[]; projects: Project[] }) {
  const [tested, setTested] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const details = useToolDetailsPref();
  const native = isNativePush();
  const row = pushRow(d.push, d.pushReason, tested, native);
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
              {d.push === "denied" && native && (
                <button type="button" class="btn" onClick={(e) => (e.preventDefault(), openNativeNotificationSettings())}>
                  Open System Settings
                </button>
              )}
              <Switch on={row.on} label="Notifications" disabled={row.disabled || busy} onChange={(on) => void toggle(on)} />
            </>
          }
        />
        {/* V0.notification-polish: only while they are on; a device without them has nothing to choose */}
        {row.on && <Row label class="push-finished" title="Finished threads" desc={FINISHED_DESC} end={<Switch on={d.pushPrefs.finished} label="Notify for finished threads" onChange={(on) => d.setPushPrefs({ ...d.pushPrefs, finished: on })} />} />}
        {/* V0.quota-reset-wake: opt-in per device */}
        {row.on && <Row label class="push-quota" title="Quota resets" desc={QUOTA_RESET_DESC} end={<Switch on={d.pushPrefs.quotaReset} label="Notify on quota resets" onChange={(on) => d.setPushPrefs({ ...d.pushPrefs, quotaReset: on })} />} />}
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
        {/* Decision 0031 2.6 (1): the one place that says which front this browser is connected through */}
        {front && <Row class="front" title="Connected through" desc={<span class="mono">{front}</span>} />}
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
      <ShownHere d={d} hosts={hosts} projects={projects} />
    </SetContext.Provider>
  );
}
