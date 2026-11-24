// SPEC V0.119 (amends V0.50), V0.125, V0.settings-ia: Settings is eight pages behind one nav, in two groups. Only on
// this device: This device (Notifications, Appearance, what it shows). On your hosts: Sign-in (who is signed in to the
// host this device is connected to), Providers (with Fyr's own calls), Projects, Automation (Auto-update, Auto-merge:
// only what Fyr does without a press), Hosts (a host's page holds its own: updates, load, diagnostics, look, about),
// Usage and Setup (each host's checks). A provider, Add provider, a project, New project and a host are sub-pages. Every change saves at once. The same pages
// on the desktop (a dialog: the nav is its left column, the page its right pane) and the phone (the nav is the
// Settings screen, a page is the next screen); the reason for the difference is width.

import { AutomationPage } from "./settings-automation.tsx";
import { HostEnv, AddProvider, ProviderBadge, ProviderPage, HostParts, ProvidersPage } from "./settings-providers.tsx";
import { AddHostButton, HostPill, HostPage, HostsPage, HostsReorderButton } from "./settings-hosts.tsx";
import { projectsPage } from "./settings-projects.tsx";
import { type HostTabItem } from "./host-tabs.ts";
import { HostTabs } from "./host-tabs-ui.tsx";
import { useEffect, useRef, useState } from "preact/hooks";
import { type ComponentChildren } from "preact";
import { Boundary } from "./boundary.tsx";
import type { Api } from "./api.ts";
import { howIn, profilesOf, type HostInfo, type HostProfiles } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import { resolveRef } from "./store.ts";
import { automationSummaryHosts, readOnlyNote } from "./host-settings.ts";
import type { Project } from "./types.ts";
import type { Theme } from "./inbox.tsx";
import { findProject, projectRecords, recordOf, settingsProjects } from "./projects.ts";
import { FINISHED_DESC, QUOTA_RESET_DESC, pushRow, type PushPrefs, type PushStatus } from "./push.ts";
import { isNativePush, openNativeNotificationSettings } from "./pwa.ts";
import { onToolDetails, setToolDetails, toolDetails } from "./prefs.ts";
import { DevicesPage, useAuthState } from "./devices-ui.tsx";
import { SetupPage } from "./setup-ui.tsx";
import { setupSummaryHosts, type SetupReport } from "./setup.ts";
import { NewProjectStep } from "./project-picker.tsx";
import { hiddenCount, hostIsHidden, lastShownHost, projectIsHidden, setHostShown, setProjectShown, type Hidden } from "./hidden.ts";
import { PeersPanel, usePeerState } from "./peers-ui.tsx";
import { devicesNeed, devicesSummary } from "./devices.ts";
import { COULD_NOT_READ, NAV_GROUPS, PAGES, deviceSummary, hostsSummary, projectsSummary, providersSummary, type PageId, type PageRef, type SettingsCounts } from "./settings-nav.ts";
import { Empty, Group, LoadError, Row, SavedMark, Seg, SetContext, Skeleton, Switch, useSaved, useSet } from "./settings-rows.tsx";
import { type ProjectPatch, ProjectEditor, archiveFor } from "./project-editor.tsx";
import type { ArchiveInfo } from "./archived.ts";
import { linkingFor } from "./project-linking.tsx";
import { type AllSettings, useSettingsData } from "./settings-load.ts";
import { countsFrom, noteSetup, publishCounts, settingsOpened, setupAge, useKnown } from "./settings-count.ts";
import { EMPTY_FORM, type ProviderForm } from "./providers.ts";
import { UsagePage } from "./settings-usage.tsx";


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
  /** V0.project-archive: Archive (on) or Unarchive; the error, or null */
  archive(id: string, on: boolean): Promise<string | null>;
  archiveInfo(id: string): ArchiveInfo;
}

export interface SettingsProps {
  api: Pick<Api, "providers" | "providersFound" | "command" | "frontCommand"> & Partial<Pick<Api, "autoMerge" | "autoDeploy" | "updates" | "setup" | "systemAgents" | "authState" | "authAct" | "peerState" | "peerAct" | "usage" | "usageOverhead">>;
  /** decision 0031 2.6: the name of the front this browser is connected through, for This device only */
  front: string;
  /** V0.51: the front's hosts with their state, the front's own among them */
  hosts?: HostInfo[];
  /** V0.125: the state's user, for How this device gets in */
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
  // V0.157: Sign-in, read by the page and by the nav's line: once when Settings opens, and every 5 s on its own page
  const auth = useAuthState(p.api, p.at?.page === "devices");
  // V0.settings-ia: the setup report of every host that can be asked (the nav's line and badge count them all). Each
  // read runs the checks live on the host, so Settings reads a host once
  // when it opens, and not at all when its report is younger than a minute; after that the Setup page keeps the
  // report current (it notes each of its own reads, so a step fixed there leaves the badge and the gear at once).
  const known = useKnown();
  const setupReps = known.setup;
  const setupIds = (p.hosts ?? []).flatMap((h) => (h.online && h.id ? [h.id] : []));
  /* eslint-disable react-hooks/exhaustive-deps -- reads when the set of online hosts changes (its joined ids) */
  useEffect(() => {
    const read = p.api.setup?.bind(p.api);
    if (!read) return;
    for (const id of setupIds) if (setupAge(id) > 60_000) void read(id).then((r) => r.ok && noteSetup(id, r.data));
  }, [p.api, setupIds.join(" ")]);
  /* eslint-enable react-hooks/exhaustive-deps */
  // While Settings is open it publishes the count its pages show; when it closes the entry points read again, and the
  // half-filled forms are dropped (the desktop's dialog closed; the phone left Settings from its first screen).
  const atNow = useRef(p.at);
  atNow.current = p.at;
  /* eslint-disable react-hooks/exhaustive-deps -- once per mount; the front does not change while it is mounted */
  useEffect(() => {
    const closed = settingsOpened();
    return () => {
      closed();
      if (!p.phone || atNow.current === null) for (const k of Object.keys(DRAFTS) as (keyof Drafts)[]) delete DRAFTS[k];
    };
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */
  const env = { readOnly: !!p.offline, phone: !!p.phone, saved: sv.mark };
  // V0.127: the nav's lines count every host; an offline host Fyr has never read is left out.
  // A host with no install id (older than V0.124) cannot be addressed (decision 0031): nothing is read from it.
  const read = data.all.filter((d) => (d.host.online || d.providers !== null) && !d.host.noId);
  const provs = read.some((d) => d.providers === null) ? null : read.flatMap((d) => d.providers!);
  const blocked = data.all.reduce((n, d) => n + (d.sa?.jobs.filter((j) => j.error).length ?? 0), 0);
  // V0.settings-ia: the one count. The nav's badges, the host tabs on Providers, a host's page and Setup, and (through
  // publishCounts) the desktop's gear and the phone's More row all show these numbers.
  const counts = countsFrom(
    p.hosts,
    data.all.map((d) => ({ host: d.host, providers: d.providers, blocked: d.sa?.jobs.filter((j) => j.error).length ?? 0, view: d.updates })),
    setupReps,
    devicesNeed(auth.s),
    !!p.offline,
  );
  const countsKey = JSON.stringify(counts);
  // Published once every host's providers are read or their read has failed (a failed read counts nothing, at the
  // door too); before that the number would shrink while the page loads.
  const settled = read.every((d) => d.providers !== null || !!d.provErr);
  /* eslint-disable react-hooks/exhaustive-deps -- publishes when the numbers change (their JSON), once the providers are read */
  useEffect(() => {
    if (settled) publishCounts(counts);
  }, [countsKey, settled]);
  /* eslint-enable react-hooks/exhaustive-deps */
  // V0.settings-ia: a form that is half filled (New project, Add provider) keeps what was typed while Settings stays
  // open: Esc and Back leave the form, and coming back finds the text (as New thread keeps its nested step). Cancel,
  // and adding, empty it; so does closing Settings. A pasted API key is never kept: it is a secret, and the draft
  // outlives the form. Kept outside the component: on the phone a project's page is a screen of its own, and opening
  // one from the list unmounts this shell without closing Settings.
  const drafts = { current: DRAFTS };
  // A link from before V0.settings-ia that names a section and no host ("automation/leftovers") gets its host once the
  // list is known, so the URL, Esc and the crumb treat the page as the host's page it shows.
  const shownFirst = (p.hosts ?? []).at(0); // the page such a link shows (pageView does the same); no call is sent to it by this
  /* eslint-disable react-hooks/exhaustive-deps -- once per such link, when the first host is known */
  useEffect(() => {
    if (p.at?.page === "hosts" && !p.at.sub && p.at.focus && shownFirst) (p.replace ?? p.go)({ page: "hosts", sub: shownFirst.name, focus: p.at.focus });
  }, [p.at?.page, p.at?.sub, p.at?.focus, shownFirst?.name]);
  /* eslint-enable react-hooks/exhaustive-deps */
  const autoErr = data.all.some((d) => (d.deploy === null && d.deployErr) || (d.repos === null && d.reposErr));
  const summaries: Record<PageId, string> = {
    providers: providersSummary(provs, blocked, read.some((d) => d.providers === null && !!d.provErr)),
    automation: automationSummaryHosts(data.all.map((d) => ({ deploy: p.api.autoDeploy ? d.deploy : false, repos: d.repos, skip: (!d.host.online && d.repos === null) || !!d.host.noId }))) || (autoErr ? COULD_NOT_READ : ""),
    projects: projectsSummary(settingsProjects(p.projects).length),
    hosts: hostsSummary(p.hosts, counts.pages.hosts),
    usage: "Tokens by model and project", // V0.usage-page: read only when the page opens
    devices: devicesSummary(auth.s),
    device: deviceSummary(p.device.push, p.device.theme, hiddenCount(p.device.hidden)),
    setup: setupSummaryHosts(
      setupIds.map((id) => setupReps[id] ?? null),
      (p.hosts ?? []).filter((h) => !h.online || !h.id).length, // offline, or too old to be read: said, not left out
    ),
  };
  const badges: Partial<Record<PageId, number>> = counts.pages;
  const [q, setQ] = useState("");
  const [reordering, setReordering] = useState(false);
  useEffect(() => (setReordering(false), setQ("")), [p.at?.page]);
  // V0.host-tabs: remember the picked host across Providers, Automation, Setup and Hosts so switching Settings pages
  // keeps working on the same host, and returning from a provider sub-page stays on its host tab.
  const [hostPick, setHostPick] = useState<string | null>(() => p.at?.host ?? (p.at?.page === "hosts" ? (p.at.sub ?? null) : null));
  useEffect(() => {
    if (p.at?.host) setHostPick(p.at.host);
    else if (p.at?.page === "hosts" && p.at.sub) setHostPick(p.at.sub);
  }, [p.at?.page, p.at?.host, p.at?.sub]);
  const ui: PageUi = { empty: settingsProjects(p.projects).length === 0, q, setQ, reordering, setReordering, hostPick, setHostPick, counts, setupReps, drafts: drafts.current };
  const view = p.at ? pageView(p.at, p, data, ui, auth, peers) : null;
  // No connection: nothing a host holds can be changed. This device's own page is kept in the browser, so it stays
  // usable and shows no banner (V0.settings-ia).
  const offBan = !!p.offline && p.at?.page !== "device";

  // Opening a page moves focus to its heading; coming back from a sub-page focuses the row it came from.
  const pane = useRef<HTMLDivElement>(null);
  const prev = useRef<PageRef | null>(p.at);
  const byKeys = useRef(false);
  /* eslint-disable react-hooks/exhaustive-deps -- moves the focus only when the page, sub-page or host changes; p.at and the front are read at that moment */
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
  /* eslint-enable react-hooks/exhaustive-deps */

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
        {offBan && <div class="m-ban">{OFFLINE()}</div>}
        <div class="m-nbody" ref={pane}>
          <div class={`set phone${offBan ? " ro" : ""}`}>
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
      <div class={`set stx${offBan ? " ro" : ""}`}>
        {nav}
        <div class="stp" ref={pane} data-page={pageKey(at)}>
          {offBan && <div class="m-ban d">{OFFLINE()}</div>}
          <div class="stp-h">
            {v.parent && (
              <>
                <button type="button" class="crumb" onClick={() => p.go({ page: at.page })}>
                  ‹ {v.parent}
                </button>
                <span class="sep">/</span>
              </>
            )}
            <h2 tabIndex={-1} class={at.sub ? "wrap" : ""}>
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

/** The eight pages in their two captioned groups (V0.settings-ia): a title and a line of state each, and the amber
 *  count on a page that holds something that waits for the owner (settingsCounts). Desktop: the dialog's left column;
 *  ↑ and ↓ move between pages and the page follows the focus. Phone: the Settings screen; the chevron shows there
 *  only, because there the row leaves the screen. */
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
      {NAV_GROUPS.map((g) => (
        <div class="stn-g" role="group" aria-label={g.caption} key={g.caption}>
          <div class="stn-cap" aria-hidden="true">
            {g.caption}
          </div>
          <div class="stn-c">
          {g.pages.map((x) => (
            <button type="button" class={`stn-i${x.id === active ? " on" : ""}`} aria-current={x.id === active ? "page" : undefined} data-page={x.id} onClick={() => onPick(x.id)}>
              <span class="stn-t">{x.title}</span>
              <span class="stn-s" title={phone ? undefined : summaries[x.id] /* the desktop cuts a long line with an ellipsis */}>{summaries[x.id]}</span>
              <span class="stn-a">
                {(badges[x.id] ?? 0) > 0 && <span class="pst needs-login">{badges[x.id]}</span>}
                {phone && <span class="chev">›</span>}
              </span>
            </button>
          ))}
          </div>
        </div>
      ))}
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

/** Page state the shell keeps, so a page's title line and its body agree. */
export interface PageUi {
  empty: boolean;
  /** V0.121: the Projects page's search and the phone's Reorder mode, kept here because the header holds Reorder */
  q: string;
  setQ(q: string): void;
  reordering: boolean;
  setReordering(b: boolean): void;
  /** V0.host-tabs: active host tab shared across multi-host Settings pages */
  hostPick: string | null;
  setHostPick(h: string): void;
  /** V0.settings-ia: the one count, for the host tabs' badges */
  counts: SettingsCounts;
  /** each host's setup report, by install id */
  setupReps: Record<string, SetupReport>;
  /** what was typed into a form that was left without Cancel */
  drafts: Drafts;
}

/** The half-filled forms Settings keeps while it is open. */
interface Drafts {
  project?: { name: string; folder: string };
  /** by the host's name ("" with one host) */
  provider?: Record<string, ProviderForm>;
}
const DRAFTS: Drafts = {};

function pageView(at: PageRef, p: SettingsProps, s: AllSettings, ui: PageUi, auth: ReturnType<typeof useAuthState>, peers: ReturnType<typeof usePeerState>): PageView {
  const go = p.go;
  switch (at.page) {
    case "providers": {
      // V0.127: a host's provider and Add provider name the host; with several hosts the page says which one.
      // V0.settings-ia: "providers/new" with several hosts and no host named is Add provider on the first host.
      const d = at.sub ? (s.at(at.host) ?? (at.sub === "new" && !at.host ? s.all[0] : undefined)) : undefined;
      const on = d && s.grouped ? d.host.label : "";
      if (d && at.sub === "new")
        return {
          title: "Add provider",
          parent: "Providers",
          lede: on ? `On ${on}. A key you paste is kept on ${on} and never leaves it.` : undefined,
          body: (
            <HostEnv d={d}>
              {/* V0.settings-ia: after adding, the form lands on the new provider's page, where its next step is (Log in,
                  or the key); on the phone it takes the form's place in the history, so Back goes to the list. */}
              <AddProvider
                host={d.host.id}
                draft={ui.drafts.provider?.[d.host.name] ?? EMPTY_FORM}
                onDraft={(f) => (ui.drafts.provider = { ...ui.drafts.provider, [d.host.name]: { ...f, key: "" } })}
                onCancel={() => {
                  delete ui.drafts.provider?.[d.host.name];
                  go({ page: "providers" });
                }}
                onCreate={async (args) => {
                  const r = await d.createProvider(args);
                  if (r.error) return r.error;
                  delete ui.drafts.provider?.[d.host.name];
                  // the new provider's page takes the form's place (phone: Back then goes to the list); when its id
                  // cannot be told, the list, by going Back to it and not as a second Providers entry in the history
                  if (r.id) (p.replace ?? go)(d.ref(r.id));
                  else go({ page: "providers" });
                  return null;
                }}
              />
            </HostEnv>
          ),
        };
      if (d && at.sub) {
        const x = d.providers?.find((y) => y.id === at.sub);
        const note = readOnlyNote(d.host, d.readAt, p.now);
        return {
          title: x?.name ?? "Provider",
          parent: "Providers",
          end: x && <ProviderBadge p={x} host={d.host.name} online={d.host.online} />,
          lede: on ? `On ${on}.` : undefined,
          body: (
            <HostEnv d={d}>
              {note && x && <div class="note hnote">{note}</div>}
              {x ? <ProviderPage key={`${d.host.name}/${x.id}`} p={x} host={d.host.name} projects={projectRecords(p.projects).filter((r) => r.hostId === d.host.id && r.rid).map((r) => ({ id: r.rid!, name: r.name }))} run={d.runProvider} refresh={() => d.refreshModels(x.id)} now={p.now} clockKnown={p.clockKnown ?? true} onRemoved={() => go({ page: "providers" })} /> : d.providers === null && !d.provErr ? <Skeleton rows={4} /> : d.provErr ? <LoadError text={d.provErr} onRetry={() => void d.loadProviders()} /> : <Group><Empty text="This provider is not here anymore." /></Group>}
            </HostEnv>
          ),
        };
      }
      // An unknown host shows the Providers page. Add provider is in the title line (the desktop) or first on the
      // page (the phone, whose header holds only the title and Back), as Hosts and Projects have theirs; with several
      // hosts it adds on the host whose tab is open.
      const tab = (s.grouped && s.all.find((x) => x.host.name === ui.hostPick)) || s.all[0];
      return {
        title: "Providers",
        end: (
          <button type="button" class={`btn addp${tab.providers?.length === 0 ? " p" : ""}`} disabled={!!p.offline || !tab.host.online || !!tab.host.noId} data-sub={subKey(tab.ref("new"))} onClick={() => go(tab.ref("new"))}>
            Add provider
          </button>
        ),
        lede: s.grouped ? "The logins and API endpoints threads run on. Each host has its own." : "The logins and API endpoints threads run on.",
        body: (
          <HostParts
            s={s}
            now={p.now}
            picked={ui.hostPick}
            onPick={ui.setHostPick}
            badge={(d) => ui.counts.tabs.providers[d.host.name] ?? 0}
            part={(d) => <ProvidersPage d={d} go={go} />}
          />
        ),
      };
    }
    case "automation":
      return {
        title: "Automation",
        lede: "What Fyr does without your press. Only you change these; an agent cannot.",
        body: <HostParts s={s} now={p.now} picked={ui.hostPick} onPick={ui.setHostPick} part={(d) => <AutomationPage d={d} row={s.grouped ? (p.hosts ?? []).find((h) => h.name === d.host.name) : p.hosts?.[0]} hasDeploy={!!p.api.autoDeploy} go={go} />} />,
      };
    case "projects": {
      // V0.settings-ia: New project is a sub-page, so Esc and Back go to the list (as from Add provider), not out of
      // Settings with the typed text dropped.
      if (at.sub === "new")
        return {
          title: "New project",
          parent: "Projects",
          body: (
            <Group title="Name and folder">
              <Row col>
                <NewProjectStep
                  initialName={ui.drafts.project?.name ?? ""}
                  initialFolder={ui.drafts.project?.folder ?? ""}
                  fromThread={false}
                  cancelLabel="Cancel"
                  onDraft={(name, folder) => (ui.drafts.project = { name, folder })}
                  onCancel={() => {
                    delete ui.drafts.project;
                    go({ page: "projects" });
                  }}
                  onCreate={async (name, folder, host) => {
                    const e = await p.projectActions.create(name, folder, host);
                    if (e) return e;
                    delete ui.drafts.project;
                    go({ page: "projects" });
                    return null;
                  }}
                />
              </Row>
            </Group>
          ),
        };
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
              archive={archiveFor(pr.id!, p.projectActions, (id) => p.onOpenThread?.(id))}
              shownHere={shownHereFor(group ?? pr, p.device.hidden, p.device.setHidden)}
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
      return { title: "Projects", lede: "A project is a folder on a host, with the defaults its threads start with.", ...projectsPage(p, ui) };
    }
    case "hosts": {
      const rows = p.hosts ?? [];
      // An unknown name shows the Hosts page. A link to a section that names no host ("automation/leftovers", from
      // before V0.settings-ia, a front with one host) opens the first host; one that names a host this front does not
      // list opens the only host there is (a front with one host: the rail's load may name it differently).
      const h = (at.sub ? rows.find((x) => x.name === at.sub) : undefined) ?? (at.focus && (!at.sub || rows.length === 1) ? rows[0] : undefined);
      if (h) {
        const d = s.grouped ? s.at(h.name) : s.all[0];
        // V0.host-ids: nothing is read from a host with no install id, nor from an offline one Fyr never read.
        const readable = !!d && !d.host.noId && (d.host.online || !!d.readAt || d.providers !== null || d.deploy !== null || d.repos !== null);
        const key = d?.host.name ?? h.name;
        // Remove is for a host that is paired with the one this device is connected to (never that one itself).
        const member = peers.s?.members.find((m) => m.id === h.id && !m.self);
        const peerAct = p.api.peerAct;
        const hostTabs: HostTabItem[] = rows.map((x) => ({
          id: x.name,
          name: x.name,
          label: x.name,
          offline: !x.online,
          badge: ui.counts.tabs.hosts[s.grouped ? x.name : ""] ?? 0,
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
              <HostPage
                key={h.name}
                h={h}
                api={p.api}
                now={p.now}
                part={d?.host ?? { name: h.name, label: h.name, online: h.online }}
                d={readable ? d : undefined}
                hasDeploy={!!p.api.autoDeploy}
                focus={at.focus}
                go={go}
                jump={(focus) => (p.replace ?? go)({ page: "hosts", sub: h.name, focus })}
                single={rows.length <= 1}
                unpair={
                  member && peerAct
                    ? async () => {
                        const r = await peerAct("remove", { id: member.id });
                        if (r.ok) peers.reload();
                        return r.ok ? null : String(r.error ?? "That did not work");
                      }
                    : undefined
                }
                setup={(h.id && ui.setupReps[h.id]) || null}
                setupNeeds={ui.counts.tabs.setup[key] ?? 0}
                shown={rows.length > 1 ? { on: !hostIsHidden(h, p.device.hidden), locked: lastShownHost(h, rows, p.device.hidden), set: (on) => p.device.setHidden(setHostShown(p.device.hidden, h, on)) } : undefined}
              />
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
        lede: "A host is a machine that runs Fyr and its threads.",
        end: (
          <>
            <HostsReorderButton ui={ui} n={rows.length} />
            {p.api.peerState && p.api.peerAct && !ui.reordering && <AddHostButton />}
          </>
        ),
        body: (
          <>
            <HostsPage rows={rows} now={p.now} go={go} ui={ui} run={runOrder} viewOf={(h) => (s.grouped ? s.at(h.name) : s.all[0])?.updates ?? null} />
            {p.api.peerState && p.api.peerAct && <PeersPanel api={{ peerState: p.api.peerState, peerAct: p.api.peerAct }} st={peers} />}
          </>
        ),
      };
    }
    case "usage":
      return { title: "Usage", lede: "The tokens every host counted: each agent's turns and Fyr's own calls.", body: <UsagePage api={p.api} now={p.now} onOpenThread={p.onOpenThread} /> };
    case "devices": {
      // V0.settings-ia: Sign-in also says how this device gets in and through which host.
      const here = <HowIn user={p.user ?? ""} front={p.front} />;
      // The page is the host's this device is connected to, and says which: it is not a page about the person.
      const lede = `The devices signed in to ${p.front || "the host this device is connected to"}, and how a new one gets in.`;
      if (!p.api.authState || !p.api.authAct)
        return {
          title: "Sign-in",
          lede,
          body: (
            <>
              <Group title="Signed in">
                <Empty text="This host cannot list devices." />
              </Group>
              {here}
            </>
          ),
        };
      return { title: "Sign-in", lede, body: <DevicesPage api={{ authState: p.api.authState, authAct: p.api.authAct }} now={p.now} st={auth} bottom={here} /> };
    }
    case "device":
      return { title: "This device", lede: "Kept on this device only. Each device has its own.", body: <DevicePage d={p.device} hosts={p.hosts ?? []} projects={p.projects} /> };
    case "setup":
      return {
        title: "Setup",
        lede: "What a host needs before it can run threads. The checks run on the host each time this page opens.",
        body: (
          <HostParts
            s={s}
            now={p.now}
            picked={ui.hostPick}
            onPick={ui.setHostPick}
            onlineOnly
            badge={(d) => ui.counts.tabs.setup[d.host.name] ?? 0}
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

/** V0.settings-ia: the per-device switch of one project, for its page: the same setting as its row in This device's
 *  list. */
export function shownHereFor(project: Project, hidden: Hidden, setHidden: (h: Hidden) => void): { on: boolean; set(on: boolean): void } {
  return { on: !projectIsHidden(project, hidden), set: (on) => setHidden(setProjectShown(hidden, project, on)) };
}

/** V0.125, decision 0031 2.6 (1): how this device reaches this Fyr and through which host: the last group of Sign-in
 *  (V0.settings-ia: state about the sign-in, not a preference of this device; last, because what waits for the owner
 *  and what can be pressed come before a status block). */
function HowIn({ user, front }: { user: string; front: string }) {
  return (
    // titled by what it says: "This device" is the nav page of what is kept on this device
    <Group title="How this device gets in" label="How this device gets in">
      <Row class="howin" desc={howIn(typeof location === "undefined" ? "" : location.hostname, user)} />
      {front && <Row class="front" title="Connected through" desc={<span class="mono">{front}</span>} />}
    </Group>
  );
}

/** V0.207: which hosts and projects this device shows. One list on the desktop and the phone. V0.settings-ia: each
 *  host's and project's own page has the same switch, so the list is the overview: folded while everything shows,
 *  and open while something is hidden (a hidden thing shows nowhere else on this device, so the list that brings it
 *  back is not behind a press). */
function ShownHere({ d, hosts, projects }: { d: SettingsDevice; hosts: HostInfo[]; projects: Project[] }) {
  const list = settingsProjects(projects).filter((x) => x.id);
  if (hosts.length < 2 && !list.length) return null;
  const n = hiddenCount(d.hidden);
  return (
    <Group title="Shown on this device" label="Shown on this device" fold={{ id: "device:shown", open: n > 0, summary: n ? `${n} hidden` : "Everything shows" }} foot="What you turn off leaves this device's inbox, filters, Changes, pickers and notifications. It keeps running, and your other devices still show it. The same switch is on each host's and project's page.">
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

/** Notifications (V0.15), Appearance (Theme, Show tool details, Install) and what this device shows: kept in this
 *  browser. Never read-only: none of it goes to the host except the push subscription, which says so when it fails. */
function DevicePage({ d, hosts, projects }: { d: SettingsDevice; hosts: HostInfo[]; projects: Project[] }) {
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
      <Group title="Notifications" label="Notifications" foot="A project's Quiet hours (on its page) and what this device shows (below) also decide what notifies here.">
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
        {/* V0.notification-polish, V0.settings-ia: what this device is told about. They show while notifications are off too, disabled and saying why (hidden, nobody could see what a device would be told before turning them on); a device that cannot notify at all has nothing to choose. */}
        {d.push !== "unsupported" && <Row label={row.on} class="push-finished" title="Finished threads" desc={row.on ? FINISHED_DESC : `${FINISHED_DESC} Applies once Notifications are on.`} end={<Switch on={d.pushPrefs.finished} label="Notify for finished threads" disabled={!row.on} onChange={(on) => d.setPushPrefs({ ...d.pushPrefs, finished: on })} />} />}
        {/* V0.quota-reset-wake: opt-in per device */}
        {d.push !== "unsupported" && <Row label={row.on} class="push-quota" title="Quota resets" desc={row.on ? QUOTA_RESET_DESC : `${QUOTA_RESET_DESC} Applies once Notifications are on.`} end={<Switch on={d.pushPrefs.quotaReset} label="Notify on quota resets" disabled={!row.on} onChange={(on) => d.setPushPrefs({ ...d.pushPrefs, quotaReset: on })} />} />}
      </Group>
      <Group title="Appearance" label="Appearance">
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
        {d.canInstall && (
          <Row
            title="Install as app"
            desc="Fyr in its own window on this device."
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
