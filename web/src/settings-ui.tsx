// SPEC V0.119 (amends V0.50): Settings is four pages behind one nav: Providers (with System agents), Automation
// (Auto-deploy, Auto-merge), Projects (with Hosts) and This device (Notifications, Theme, Show tool details, Install).
// A provider, Add provider and a project are sub-pages. Every change saves at once. The same pages on the desktop (a
// dialog: the nav is its left column, the page its right pane) and the phone (the nav is the Settings screen, a page
// is the next screen); the reason for the difference is width.

import { useEffect, useRef, useState } from "preact/hooks";
import { Fragment, type ComponentChildren } from "preact";
import type { Api } from "./api.ts";
import { hostStatus, type HostInfo } from "./hosts.ts";
import { quotaFor } from "./quota.ts";
import { QuotaLine, useQuotas } from "./quota-ui.tsx";
import { CARD_WORD, cardState } from "./quota.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { removeProviderAsk } from "./confirm.ts";
import { useAsk } from "./confirm-ui.tsx";
import type { Profile, Project } from "./types.ts";
import type { Theme } from "./inbox.tsx";
import { repoKey, sortRepos, type AutoMergeRepo } from "./automerge.ts";
import { jobLine, modelOptions, providerOptions, type Option, type SystemAgentChoice, type SystemAgentsView } from "./system-agents.ts";
import { colorVar, projectSearchLine, projectSubtitle, searchProjects, settingsProjects } from "./projects.ts";
import { quietNote } from "./quiet.ts";
import { NewProjectStep, ProjectEditor, ProjectIcon, type ProjectPatch } from "./project-ui.tsx";
import { pushRow, type PushStatus } from "./push.ts";
import { onToolDetails, setToolDetails, toolDetails } from "./prefs.ts";
import { move } from "./reorder.ts";
import { PAGES, automationSummary, deviceSummary, navBadge, projectsSummary, providersNeed, providersSummary, type PageId, type PageRef } from "./settings-nav.ts";
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
  checkErrorLine,
  checkLine,
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
  /** New project without a thread; the error, or null */
  create(name: string, folder: string): Promise<string | null>;
  /** V0.121: the owner's project order (front.projectOrder); the error, or null */
  order(ids: string[]): Promise<string | null>;
}

export interface SettingsProps {
  api: Pick<Api, "providers" | "providersFound" | "command"> & Partial<Pick<Api, "autoMerge" | "autoDeploy" | "systemAgents">>;
  host: string;
  /** V0.51: the front's hosts with their state */
  hosts?: HostInfo[];
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

// ---- the data every page and the nav read; each part loads by itself ----

type Run = (action: string, args: Record<string, unknown>) => Promise<string | null>;

function useSettingsData(p: SettingsProps, saved: () => void) {
  const api = p.api;
  const [providers, setProviders] = useState<ProviderDTO[] | null>(null);
  const [provErr, setProvErr] = useState<string | null>(null);
  const [found, setFound] = useState<FoundDTO[]>([]);
  const [deploy, setDeploy] = useState<boolean | null>(null);
  const [deployErr, setDeployErr] = useState<string | null>(null);
  const [repos, setRepos] = useState<AutoMergeRepo[] | null>(null);
  const [reposErr, setReposErr] = useState<string | null>(null);
  const hostNames = (p.hosts ?? []).map((h) => h.name);
  const [saHost, setSaHost] = useState(hostNames[0] ?? "");
  const [sa, setSa] = useState<SystemAgentsView | null>(null);
  const [saErr, setSaErr] = useState<string | null>(null);

  const loadProviders = async () => {
    try {
      const [l, f] = await Promise.all([api.providers(), api.providersFound().catch(() => [] as FoundDTO[])]);
      setProviders(l);
      setFound(f);
      setProvErr(null);
    } catch (e) {
      setProvErr(`Could not read the providers: ${(e as Error).message}.`);
    }
  };
  const loadDeploy = async () => {
    if (!api.autoDeploy) return setDeploy(null);
    try {
      setDeploy((await api.autoDeploy()).on);
      setDeployErr(null);
    } catch (e) {
      setDeployErr(`Could not read the auto-deploy setting: ${(e as Error).message}.`);
    }
  };
  const loadRepos = async () => {
    if (!api.autoMerge) return setRepos([]);
    try {
      setRepos(await api.autoMerge());
      setReposErr(null);
    } catch (e) {
      setReposErr(`Could not read the auto-merge setting: ${(e as Error).message}.`);
    }
  };
  const loadSa = async (h = saHost) => {
    if (!api.systemAgents) return;
    try {
      setSa(await api.systemAgents(h || undefined));
      setSaErr(null);
    } catch (e) {
      setSaErr(`Could not read the system agents: ${(e as Error).message}.`);
    }
  };
  useEffect(() => {
    void loadProviders();
    void loadDeploy();
    void loadRepos();
  }, []);
  useEffect(() => void loadSa(saHost), [saHost]);
  // V0.53: while a login runs, follow it (its link, the code's answer, the end).
  const logging = !!providers?.some((x) => loginActive(x.login));
  useEffect(() => {
    if (!logging) return;
    const t = setInterval(() => void loadProviders(), 1500);
    return () => {
      clearInterval(t);
      p.onChanged?.(); // a login ended: the profiles' status may have changed
    };
  }, [logging]);

  /** One owner command; ok shows Saved and reloads what it changed. The error is shown on the row that sent it. */
  const command = async (action: string, args: Record<string, unknown>, reload: () => Promise<void>): Promise<string | null> => {
    const r = await api.command(action, undefined, args);
    if (!r.ok) {
      await reload(); // the control goes back to what the host has
      return r.error ?? "That did not work";
    }
    saved();
    await reload();
    return null;
  };
  const runProvider: Run = async (action, args) => {
    const e = await command(action, args, loadProviders);
    if (!e) {
      p.onChanged?.();
      void loadSa();
    }
    return e;
  };
  /** V0.120: ask a provider for its models now. Not a save: no Saved mark. The reply waits for the check. */
  const refreshModels = async (id: string): Promise<string | null> => {
    const r = await api.command("provider.refreshModels", undefined, { id });
    await loadProviders();
    if (r.ok) p.onChanged?.();
    return r.ok ? null : (r.error ?? "That did not work");
  };
  return {
    providers,
    provErr,
    refreshModels,
    found,
    loadProviders,
    deploy,
    deployErr,
    loadDeploy,
    repos,
    reposErr,
    loadRepos,
    sa,
    saErr,
    saHost,
    setSaHost,
    loadSa,
    hostNames,
    runProvider,
    runDeploy: (on: boolean) => command("host.autoDeploy", { on }, loadDeploy),
    runRepo: (repo: string, on: boolean) => command("host.autoMerge", { repo, on }, loadRepos),
    runSa: (args: Record<string, unknown>) => command("host.systemAgents", saHost ? { ...args, host: saHost } : args, () => loadSa()),
  };
}
type Data = ReturnType<typeof useSettingsData>;

/** What needs the owner on the Providers page, for the phone's More sheet ("1 needs a login"); "" while unknown. */
export function useProvidersNeed(api: SettingsProps["api"]): string {
  const [v, setV] = useState("");
  useEffect(() => {
    let live = true;
    void Promise.all([api.providers(), api.systemAgents ? api.systemAgents().catch(() => null) : null])
      .then(([l, sa]) => live && setV(providersNeed(l, sa?.jobs.filter((j) => j.error).length ?? 0)))
      .catch(() => {});
    return () => void (live = false);
  }, []);
  return v;
}

// ---- the shell ----

export function Settings(p: SettingsProps) {
  const sv = useSaved();
  const data = useSettingsData(p, sv.mark);
  const env = { readOnly: !!p.offline, phone: !!p.phone, saved: sv.mark };
  const blocked = data.sa?.jobs.filter((j) => j.error).length ?? 0;
  const summaries: Record<PageId, string> = {
    providers: providersSummary(data.providers, blocked),
    automation: automationSummary(p.api.autoDeploy ? data.deploy : false, data.repos),
    projects: projectsSummary(settingsProjects(p.projects).length, p.hosts?.length ?? 0),
    device: deviceSummary(p.device.push, p.device.theme),
  };
  const badge = navBadge(data.providers, blocked);
  const [adding, setAdding] = useState(false);
  const [q, setQ] = useState("");
  const [reordering, setReordering] = useState(false);
  useEffect(() => (setAdding(false), setReordering(false), setQ("")), [p.at?.page]);
  const ui: PageUi = { adding, setAdding, empty: settingsProjects(p.projects).length === 0, q, setQ, reordering, setReordering };
  const view = p.at ? pageView(p.at, p, data, ui) : null;

  // Opening a page moves focus to its heading; coming back from a sub-page focuses the row it came from.
  const pane = useRef<HTMLDivElement>(null);
  const prev = useRef<PageRef | null>(p.at);
  const byKeys = useRef(false);
  useEffect(() => {
    const was = prev.current;
    prev.current = p.at;
    if (!p.at || (was && was.page === p.at.page && was.sub === p.at.sub)) return;
    if (byKeys.current) return void (byKeys.current = false);
    const el = pane.current ?? document;
    requestAnimationFrame(() => {
      if (was?.sub && was.page === p.at!.page && !p.at!.sub) {
        const row = el.querySelector<HTMLElement>(`[data-sub="${CSS.escape(was.sub)}"]`);
        if (row) return row.focus();
      }
      if (!p.phone) el.querySelector<HTMLElement>(".stp-h h2")?.focus();
    });
  }, [p.at?.page, p.at?.sub]);

  const nav = (
    <SettingsNav
      phone={!!p.phone}
      active={p.at?.page ?? null}
      summaries={summaries}
      badge={badge}
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
                {view.body}
              </div>
            ) : (
              nav
            )}
          </div>
        </div>
      </SetContext.Provider>
    );
  }
  const v = view ?? pageView({ page: "providers" }, p, data, ui);
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
            <h2 tabIndex={-1} class={at.sub ? "wrap" : ""}>
              {v.title}
            </h2>
            <span class="end">
              <SavedMark shown={sv.shown} />
              {v.end}
            </span>
          </div>
          {v.lede && <p class="stp-lede">{v.lede}</p>}
          {v.body}
        </div>
      </div>
    </SetContext.Provider>
  );
}

const pageKey = (r: PageRef) => (r.sub ? `${r.page}/${r.sub}` : r.page);

/** The four pages: a title and a line of state each, and the amber count on Providers. Desktop: the dialog's left
 *  column; ↑ and ↓ move between pages and the page follows the focus. Phone: the Settings screen; the chevron shows
 *  there only, because there the row leaves the screen. */
export function SettingsNav({ phone, active, summaries, badge, onPick }: { phone: boolean; active: PageId | null; summaries: Record<PageId, string>; badge: number; onPick(id: PageId, keys?: boolean): void }) {
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
              {x.id === "providers" && badge > 0 && <span class="pst needs-login">{badge}</span>}
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

function pageView(at: PageRef, p: SettingsProps, d: Data, ui: PageUi): PageView {
  const go = p.go;
  switch (at.page) {
    case "providers": {
      if (at.sub === "new") return { title: "Add provider", parent: "Providers", body: <AddProvider onCancel={() => go({ page: "providers" })} onCreate={async (args) => { const e = await d.runProvider("provider.create", args); if (!e) go({ page: "providers" }); return e; }} /> };
      if (at.sub) {
        const x = d.providers?.find((y) => y.id === at.sub);
        return {
          title: x?.name ?? "Provider",
          parent: "Providers",
          end: x && <ProviderBadge p={x} />,
          body: x ? <ProviderPage key={x.id} p={x} run={d.runProvider} refresh={() => d.refreshModels(x.id)} now={p.now} clockKnown={p.clockKnown ?? true} onRemoved={() => go({ page: "providers" })} /> : d.providers === null && !d.provErr ? <Skeleton rows={4} /> : d.provErr ? <LoadError text={d.provErr} onRetry={() => void d.loadProviders()} /> : <Group><Empty text="This provider is not here anymore." /></Group>,
        };
      }
      return { title: "Providers", body: <ProvidersPage d={d} go={go} /> };
    }
    case "automation":
      return { title: "Automation", lede: "What Fyr does without your press. Only you change these; an agent cannot.", body: <AutomationPage d={d} hasDeploy={!!p.api.autoDeploy} /> };
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
              profiles={p.profiles}
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
    case "device":
      return { title: "This device", lede: "These are kept in this browser. Each device has its own.", body: <DevicePage d={p.device} /> };
  }
}

// ---- Providers ----

/** The status badge: today's words; V0.77 (1), #65: it tells the truth about the account, so Ready never sits beside
 *  its own error. */
function ProviderBadge({ p }: { p: ProviderDTO }) {
  const quota = quotaFor(useQuotas(), p.id);
  return <span class={`pst ${p.status}`}>{quota && p.status === "ready" ? CARD_WORD[cardState(quota)] : STATUS_TEXT[p.status]}</span>;
}

function ProvidersPage({ d, go }: { d: Data; go(r: PageRef): void }) {
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
            const q = quotaFor(quotas, x.id);
            return (
              <Row
                onClick={() => go({ page: "providers", sub: x.id })}
                data={{ sub: x.id }}
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
                    <ProviderBadge p={x} />
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
        <button type="button" class={`btn addp${none ? " p" : ""}`} disabled={env.readOnly} data-sub="new" onClick={() => go({ page: "providers", sub: "new" })}>
          Add provider
        </button>
      </div>
      <h3 class="stp-sub">System agents</h3>
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
      {d.hostNames.length > 1 && <Row title="Host" desc="Each host runs these on its own providers." stack end={<Select label="Host" value={d.saHost} options={d.hostNames.map((h) => ({ value: h, label: h }))} onPick={d.setSaHost} />} />}
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
function ProviderPage({ p, run, refresh, now, clockKnown, onRemoved }: { p: ProviderDTO; run: Run; refresh(): Promise<string | null>; now: number; clockKnown: boolean; onRemoved(): void }) {
  const quota = quotaFor(useQuotas(), p.id);
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
  const failed = checkErrorLine(p, now);
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
      foot={failed ?? "★ is the default for a new thread. Fyr asks the provider for its models when you open this page and once a day."}
      footErr={!!failed}
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
function AutomationPage({ d, hasDeploy }: { d: Data; hasDeploy: boolean }) {
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
          <Group label="Auto-deploy">
            <Row
              label
              title="Deploy Fyr without asking"
              desc="When an agent asks to deploy Fyr and the checkout is clean and on origin's default branch. The build and the tests still decide the switch."
              error={deployErr}
              end={<Switch on={d.deploy} label="Auto-approve Fyr deploys" onChange={async (on) => setDeployErr(await d.runDeploy(on))} />}
            />
          </Group>
        ))}
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

/** V0.58: every project with its icon, folder and thread count; a row opens the project's page. Search from nine
 *  projects. V0.121: in the owner's one order, which this page sets with the one reorder control (a grip on the
 *  desktop, a Reorder mode with ↑ and ↓ on the phone); quiet projects are listed and keep their place. Hosts (V0.51)
 *  under their own heading. */
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
              onCreate={async (name, folder) => {
                const e = await p.projectActions.create(name, folder);
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
      {!mode && (
        <>
          <h3 class="stp-sub">Hosts</h3>
          <Group label="Hosts">
            {p.hosts && p.hosts.length > 0 ? (
              p.hosts.map((h) => <Row key={h.name} title={h.name} mono class="host" desc={h.error ? undefined : hostStatus(h, p.host)} error={h.error ? `${hostStatus(h, p.host)} · ${h.error}` : null} />)
            ) : (
              <Row title={p.host || "This host"} mono class="host" desc="this machine" />
            )}
          </Group>
        </>
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
function DevicePage({ d }: { d: SettingsDevice }) {
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
