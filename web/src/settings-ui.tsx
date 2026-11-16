// SPEC 0.5 V0.50: Settings. Providers (list, Found on this host, Add provider), Projects (V0.58), Hosts,
// Notifications, Appearance. The same body on desktop (a dialog) and phone (a screen).

import { useEffect, useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import type { Api } from "./api.ts";
import { hostStatus, type HostInfo } from "./hosts.ts";
import type { Quota } from "./quota.ts";
import { quotaFor } from "./quota.ts";
import { QuotaLine, useQuotas } from "./quota-ui.tsx";
import { CARD_WORD, cardState } from "./quota.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { removeProviderAsk } from "./confirm.ts";
import { useAsk } from "./confirm-ui.tsx";
import type { Project } from "./types.ts";
import { repoKey, sortRepos, type AutoDeploySetting, type AutoMergeRepo } from "./automerge.ts";
import { jobLine, modelOptions, providerOptions, type Option, type SystemAgentChoice, type SystemAgentsView } from "./system-agents.ts";
import { colorVar, projectSubtitle } from "./projects.ts";
import { NewProjectStep, ProjectIcon } from "./project-ui.tsx";
import {
  createArgs,
  EMPTY_FORM,
  importable,
  keyLine,
  kindLabel,
  loginActive,
  loginButton,
  loginLine,
  modelsSummary,
  modelsText,
  parseModelIds,
  STATUS_TEXT,
  whereText,
  type FoundDTO,
  type LoginDTO,
  type ProviderDTO,
  type ProviderForm,
} from "./providers.ts";

export interface SettingsProps {
  api: Pick<Api, "providers" | "providersFound" | "command"> & Partial<Pick<Api, "autoMerge" | "autoDeploy" | "systemAgents">>;
  host: string;
  /** V0.51: the front's hosts with their state */
  hosts?: HostInfo[];
  phone?: boolean;
  /** the push control (Notifications) */
  push?: ComponentChildren;
  /** the theme control (Appearance) */
  appearance?: ComponentChildren;
  /** the providers changed: reload the state's profiles */
  onChanged?(): void;
  /** V0.58: the Projects section; without it the section is left out */
  projects?: Project[];
  onOpenProject?(id: string): void;
  /** New project without a thread; the error, or null */
  onCreateProject?(name: string, folder: string): Promise<string | null>;
}

export function Settings(p: SettingsProps) {
  return (
    <div class={`set${p.phone ? " phone" : ""}`}>
      <ProvidersSection {...p} />
      {p.projects && <ProjectsSection projects={p.projects} onOpen={(id) => p.onOpenProject?.(id)} onCreate={p.onCreateProject} />}
      {p.api.autoMerge && <AutoMergeSection api={p.api as AutoMergeApi} />}
      {p.api.autoDeploy && <AutoDeploySection api={p.api as AutoDeployApi} />}
      {p.api.systemAgents && <SystemAgentsSection api={p.api as SystemAgentsApi} hosts={(p.hosts ?? []).map((h) => h.name)} />}
      <section class="sets">
        <h3>Hosts</h3>
        {p.hosts && p.hosts.length > 0 ? (
          p.hosts.map((h) => (
            <div class="setrow" key={h.name} title={h.error}>
              <span class="setn">{h.name}</span>
              <span class="setd">{hostStatus(h, p.host)}</span>
            </div>
          ))
        ) : (
          <div class="setrow">
            <span class="setn">{p.host || "This host"}</span>
            <span class="setd">this host</span>
          </div>
        )}
      </section>
      <section class="sets">
        <h3>Notifications</h3>
        {p.push ?? <div class="setd">Not available in this browser.</div>}
      </section>
      <section class="sets">
        <h3>Appearance</h3>
        {p.appearance ?? <div class="setd">Theme follows the system.</div>}
      </section>
    </div>
  );
}

/** V0.58: every project by name, with its icon, folder and thread count; a row opens the project screen. */
export function ProjectsSection({ projects, onOpen, onCreate }: { projects: Project[]; onOpen(id: string): void; onCreate?(name: string, folder: string): Promise<string | null> }) {
  const [adding, setAdding] = useState(false);
  const list = [...projects].filter((x) => x.id).sort((a, b) => a.name.localeCompare(b.name));
  return (
    <section class="sets" aria-label="Projects">
      <h3>Projects</h3>
      {list.length === 0 && <div class="setd">No projects yet.</div>}
      {list.length > 0 && (
        <div class="pls setpl">
          {list.map((x) => (
            <button key={x.id} class="pli" style={{ "--pc": colorVar(x.color, x.name) }} onClick={() => onOpen(x.id!)}>
              <ProjectIcon project={x} name={x.name} size={28} />
              <span class="plt">
                <span class="ell pln">{x.name}</span>
                <span class="ell pls2">{projectSubtitle(x)}</span>
              </span>
              <span class="plc" title={`${x.threads ?? 0} threads`}>
                {x.threads ?? 0}
              </span>
            </button>
          ))}
        </div>
      )}
      {onCreate &&
        (adding ? (
          <NewProjectStep
            initialName=""
            initialFolder=""
            fromThread={false}
            onCancel={() => setAdding(false)}
            onCreate={async (name, folder) => {
              const e = await onCreate(name, folder);
              if (!e) setAdding(false);
              return e;
            }}
          />
        ) : (
          <button class="btn sbtn addp" onClick={() => setAdding(true)}>
            New project
          </button>
        ))}
    </section>
  );
}

function ProvidersSection(p: SettingsProps) {
  const [list, setList] = useState<ProviderDTO[] | null>(null);
  const [found, setFound] = useState<FoundDTO[]>([]);
  const [err, setErr] = useState<string | null>(null);
  const [open, setOpen] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const quotas = useQuotas();
  const load = async () => {
    try {
      const [l, f] = await Promise.all([p.api.providers(), p.api.providersFound().catch(() => [] as FoundDTO[])]);
      setList(l);
      setFound(f);
    } catch (e) {
      setErr(`Could not read the providers: ${(e as Error).message}`);
      setList([]);
    }
  };
  useEffect(() => void load(), []);
  // V0.53: while a login runs, follow it (its link, the code's answer, the end).
  const logging = !!list?.some((x) => loginActive(x.login));
  useEffect(() => {
    if (!logging) return;
    const t = setInterval(() => void load(), 1500);
    return () => {
      clearInterval(t);
      p.onChanged?.(); // a login ended: the profiles' status may have changed
    };
  }, [logging]);
  const run = async (action: string, args: Record<string, unknown>): Promise<boolean> => {
    const r = await p.api.command(action, undefined, args);
    if (!r.ok) {
      setErr(r.error ?? "That did not work");
      return false;
    }
    setErr(null);
    await load();
    p.onChanged?.();
    return true;
  };
  const finds = importable(found);
  return (
    <section class="sets" aria-label="Providers">
      <h3>Providers</h3>
      {err && (
        <div class="seterr" role="alert">
          {err}
        </div>
      )}
      {list === null && <div class="setd">Loading…</div>}
      {list?.length === 0 && <div class="setd">No providers yet. Import one below or add one.</div>}
      <ul class="provs">
        {list?.map((x) => (
          <ProviderRow key={x.id} p={x} quota={quotaFor(quotas, x.id)} open={open === x.id} onToggle={() => setOpen(open === x.id ? null : x.id)} run={run} />
        ))}
      </ul>
      {finds.length > 0 && (
        <>
          <h4>Found on this host</h4>
          <ul class="provs">
            {finds.map((f) => (
              <li class="prov found">
                <div class="provh">
                  <span class="provn">
                    <b>{f.name}</b>
                    <span class="setd">
                      {kindLabel(f.kind)} · {f.source}
                    </span>
                    {f.keyFrom && <span class="setd">Key: {f.keyFrom}</span>}
                    {f.note && <span class="setd">{f.note}</span>}
                  </span>
                  <span class={`pst ${f.status}`}>{STATUS_TEXT[f.status]}</span>
                  <button class="btn sbtn" onClick={() => void run("provider.import", { foundId: f.id })}>
                    Import
                  </button>
                </div>
              </li>
            ))}
          </ul>
        </>
      )}
      {adding ? (
        <AddProvider onCancel={() => setAdding(false)} onCreate={async (args) => (await run("provider.create", args)) && (setAdding(false), true)} />
      ) : (
        <button class="btn sbtn addp" onClick={() => setAdding(true)}>
          Add provider
        </button>
      )}
    </section>
  );
}

function ProviderRow({ p, quota, open, onToggle, run }: { p: ProviderDTO; quota?: Quota; open: boolean; onToggle(): void; run(a: string, args: Record<string, unknown>): Promise<boolean> }) {
  const [models, setModels] = useState(modelsText(p.models));
  const [key, setKey] = useState("");
  const ask = useAsk();
  useEffect(() => setModels(modelsText(p.models)), [p.models.map((m) => m.id).join("\n")]);
  const effortModels = p.models.filter((m) => m.efforts?.length);
  const kl = keyLine(p);
  return (
    <li class={`prov${open ? " open" : ""}`}>
      <button class="provh" aria-expanded={open} onClick={onToggle}>
        <span class="pdot" style={p.color ? { background: p.color } : undefined} />
        <span class="provn">
          <b>{p.name}</b>
          <span class="setd">
            {kindLabel(p.kind)} · {whereText(p)} · {modelsSummary(p.models)}
          </span>
        </span>
        {/* V0.77 (1), #65: the badge tells the truth about the account, so Ready never sits beside its own error. */}
        <span class={`pst ${p.status}`}>{quota && p.status === "ready" ? CARD_WORD[cardState(quota)] : STATUS_TEXT[p.status]}</span>
      </button>
      {quota && <QuotaLine q={quota} name={p.name} />}
      {p.need && !loginActive(p.login) && <div class="setneed">{p.need}</div>}
      {p.login && <LoginPanel p={p} l={p.login} run={run} />}
      {p.status === "needs-login" && loginButton(p) && (
        <div class="setbar">
          <button class="btn p sbtn" onClick={() => void run("provider.login", { id: p.id })}>
            {loginButton(p)}
          </button>
        </div>
      )}
      {open && (
        <div class="provb">
          <label class="fld">
            <span>Models (one id per line)</span>
            <textarea rows={Math.min(6, Math.max(2, p.models.length + 1))} value={models} onInput={(e) => setModels((e.target as HTMLTextAreaElement).value)} />
          </label>
          <div class="row2">
            <label class="fld">
              <span>Default model</span>
              <select value={p.defaultModel ?? ""} onChange={(e) => void run("provider.update", { id: p.id, defaultModel: (e.target as HTMLSelectElement).value })}>
                {p.models.map((m) => (
                  <option value={m.id}>{m.name || m.id}</option>
                ))}
              </select>
            </label>
            {effortModels.length > 0 && (
              <label class="fld">
                <span>Default effort</span>
                <select value={p.defaultEffort ?? ""} onChange={(e) => void run("provider.update", { id: p.id, defaultEffort: (e.target as HTMLSelectElement).value })}>
                  <option value="">CLI default</option>
                  {p.efforts.map((x) => (
                    <option value={x}>{x}</option>
                  ))}
                </select>
              </label>
            )}
          </div>
          <div class="setbar">
            <button class="btn sbtn" disabled={models === modelsText(p.models)} onClick={() => void run("provider.update", { id: p.id, models: parseModelIds(models) })}>
              Save models
            </button>
          </div>
          {p.auth !== "none" && (
            <label class="fld">
              <span>{p.kind === "endpoint" ? "API key" : p.tokenLogin ? "Login token" : "Login token (optional)"}</span>
              {kl && <span class="setd">{kl}</span>}
              <div class="setkey">
                <input type="password" autocomplete="off" spellcheck={false} value={key} placeholder={p.keySet ? "Paste a new key to replace it" : "Paste the key"} onInput={(e) => setKey((e.target as HTMLInputElement).value)} />
                <button class="btn sbtn" disabled={!key.trim()} onClick={() => void run("provider.setKey", { id: p.id, key }).then((ok) => ok && setKey(""))}>
                  Save key
                </button>
              </div>
            </label>
          )}
          {p.status !== "needs-login" && loginButton(p) && (
            <div class="setbar">
              <button class="btn sbtn" onClick={() => void run("provider.login", { id: p.id })}>
                {loginButton(p)}
              </button>
            </div>
          )}
          <div class="setd">
            {p.quota !== "none" ? `Quota: ${p.quota}` : "No quota reading"}
            {p.imported ? ` · imported from ${p.imported}` : ""}
            {p.threads ? ` · ${p.threads} thread${p.threads === 1 ? "" : "s"}` : ""}
          </div>
          <div class="setbar">
            <button class="btn sbtn danger" disabled={p.threads > 0} title={p.threads > 0 ? "Threads use it; hand them over first" : ""} onClick={async () => (await ask(removeProviderAsk(p))) && void run("provider.delete", { id: p.id })}>
              Remove
            </button>
          </div>
        </div>
      )}
    </li>
  );
}

// V0.53: a login Fyr runs with the CLI: the sign-in link, the code field, Cancel. The code goes to the CLI only.
function LoginPanel({ p, l, run }: { p: ProviderDTO; l: LoginDTO; run(a: string, args: Record<string, unknown>): Promise<boolean> }) {
  const [code, setCode] = useState("");
  const active = loginActive(l);
  const ends = new Date(l.expires);
  const send = () => void run("provider.loginCode", { id: p.id, code }).then((ok) => ok && setCode(""));
  return (
    <div class={`login ${l.state}`} aria-live="polite">
      <div class="setd">{loginLine(l)}</div>
      {active && l.message && (
        <div class="seterr" role="alert">
          {l.message}
        </div>
      )}
      {active && l.url && (
        <>
          <a class="btn p sbtn loginlink" href={l.url} target="_blank" rel="noopener noreferrer">
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
                disabled={l.state === "checking"}
                onInput={(e) => setCode((e.target as HTMLInputElement).value)}
                onKeyDown={(e) => e.key === "Enter" && code.trim() && send()}
              />
              <button class="btn sbtn" disabled={!code.trim() || l.state === "checking"} onClick={send}>
                Send code
              </button>
            </div>
          </label>
        </>
      )}
      <div class="setbar">
        {active ? (
          <>
            <span class="setd">Ends at {isNaN(ends.getTime()) ? "" : ends.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}</span>
            <button class="btn sbtn" onClick={() => void run("provider.loginCancel", { id: p.id })}>
              Cancel
            </button>
          </>
        ) : (
          (l.state === "failed" || l.state === "timeout") && (
            <button class="btn sbtn" onClick={() => void run("provider.login", { id: p.id })}>
              Try again
            </button>
          )
        )}
      </div>
    </div>
  );
}

function AddProvider(p: { onCancel(): void; onCreate(args: Record<string, unknown>): Promise<boolean> }) {
  const [f, setF] = useState<ProviderForm>(EMPTY_FORM);
  const [err, setErr] = useState<string | null>(null);
  const picker = useFolderPick();
  const set = (k: keyof ProviderForm) => (e: Event) => setF({ ...f, [k]: (e.target as HTMLInputElement).value });
  const submit = async () => {
    const r = createArgs(f);
    if ("error" in r) return setErr(r.error);
    setErr(null);
    await p.onCreate(r.args);
  };
  return (
    <div class="addf">
      <div class="seg" role="radiogroup" aria-label="Kind">
        {(["claude-account", "endpoint"] as const).map((k) => (
          <button role="radio" aria-checked={f.kind === k} class={`sbtn${f.kind === k ? " on" : ""}`} onClick={() => setF({ ...f, kind: k })}>
            {kindLabel(k)}
          </button>
        ))}
      </div>
      <label class="fld">
        <span>Name</span>
        <input value={f.name} onInput={set("name")} placeholder={f.kind === "endpoint" ? "OpenRouter" : "Claude (work)"} />
      </label>
      {f.kind === "claude-account" ? (
        <label class="fld">
          <span>Config folder (empty: ~/.claude)</span>
          <div class="setkey">
            <input value={f.configDir} onInput={set("configDir")} placeholder="~/.claude-work" />
            <button class="btn sbtn" onClick={() => void picker.pick({ title: "Config folder" }).then((d) => d && setF({ ...f, configDir: d }))}>
              Choose…
            </button>
          </div>
          <span class="setd">After adding it, press Log in: Fyr runs claude's login in this folder and shows the sign-in link here.</span>
        </label>
      ) : (
        <>
          <label class="fld">
            <span>Base URL</span>
            <input value={f.baseURL} onInput={set("baseURL")} placeholder="https://openrouter.ai/api" inputMode="url" />
          </label>
          <label class="fld">
            <span>API key</span>
            <input type="password" autocomplete="off" value={f.key} onInput={set("key")} placeholder="Kept in a private file on this host" />
          </label>
        </>
      )}
      <label class="fld">
        <span>Model ids (one per line{f.kind === "claude-account" ? "; empty: Claude's list" : ""})</span>
        <textarea rows={3} value={f.models} onInput={set("models")} />
      </label>
      {err && (
        <div class="seterr" role="alert">
          {err}
        </div>
      )}
      <div class="setbar">
        <button class="btn sbtn" onClick={p.onCancel}>
          Cancel
        </button>
        <button class="btn p sbtn" onClick={() => void submit()}>
          Add
        </button>
      </div>
    </div>
  );
}

type AutoMergeApi = Pick<Api, "autoMerge" | "command">;
type AutoDeployApi = Pick<Api, "autoDeploy" | "command">;
type SystemAgentsApi = Pick<Api, "systemAgents" | "command">;

function Pick1({ label, value, options, onPick }: { label: string; value: string; options: Option[]; onPick(v: string): void }) {
  return (
    <label class="fld">
      <span>{label}</span>
      <select value={value} aria-label={label} onChange={(e) => onPick((e.target as HTMLSelectElement).value)}>
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
    </label>
  );
}

/** V0.115: the provider and model of the calls Fyr starts itself (thread titles, project setup), per host: for all
 *  jobs, and per job. No provider is the host's default, no model is its Haiku; a job that cannot run says why here
 *  and fails visibly, never on a bigger model. Only the owner changes it here; no agent tool can. Desktop and phone
 *  alike (the shared Settings body). */
function SystemAgentsSection({ api, hosts }: { api: SystemAgentsApi; hosts: string[] }) {
  const [host, setHost] = useState(hosts[0] ?? "");
  const [v, setV] = useState<SystemAgentsView | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const load = async (h: string) => {
    try {
      setV(await api.systemAgents(h || undefined));
      setErr(null);
    } catch (e) {
      setV(null);
      setErr(`Could not read the system agents setting: ${(e as Error).message}`);
    }
  };
  useEffect(() => void load(host), [host]);
  // A job "" is the default; null clears a job's own choice.
  const set = async (job: string, c: SystemAgentChoice | null) => {
    const args: Record<string, unknown> = c ? { job, profile: c.profile ?? "", model: c.model ?? "" } : { job, clear: true };
    if (host) args.host = host;
    const r = await api.command("host.systemAgents", undefined, args);
    if (!r.ok) {
      setErr(r.error ?? "That did not work");
      return;
    }
    await load(host);
  };
  const def = v?.default ?? {};
  return (
    <section class="sets" aria-label="System agents">
      <h3>System agents</h3>
      <div class="setd">The model calls Fyr makes itself, such as thread titles and project setup. They run on this provider and model. A job that cannot run fails and shows why; it never moves to a bigger model.</div>
      {hosts.length > 1 && (
        <div class="sarow">
          <Pick1 label="Host" value={host} options={hosts.map((h) => ({ value: h, label: h }))} onPick={setHost} />
        </div>
      )}
      {err && (
        <div class="seterr" role="alert">
          {err}
        </div>
      )}
      {v === null && !err && <div class="setd">Loading…</div>}
      {v && (
        <>
          <div class="sarow">
            <Pick1 label="Provider" value={def.profile ?? ""} options={providerOptions(v)} onPick={(x) => void set("", { profile: x })} />
            <Pick1 label="Model" value={def.model ?? ""} options={modelOptions(v, def.profile ?? "")} onPick={(x) => void set("", { profile: def.profile, model: x })} />
          </div>
          {v.jobs.map((j) => {
            const line = jobLine(v, j);
            const own = j.choice.profile ?? def.profile ?? "";
            return (
              <div class="sajob" key={j.id}>
                <div class="sarow">
                  <Pick1
                    label={j.name}
                    value={j.override ? (j.choice.profile ?? "") : "="}
                    options={[{ value: "=", label: "Same as above" }, ...providerOptions(v).slice(1)]}
                    onPick={(x) => void set(j.id, x === "=" ? null : { profile: x })}
                  />
                  {j.override && <Pick1 label="Model" value={j.choice.model ?? ""} options={modelOptions(v, own)} onPick={(x) => void set(j.id, { profile: j.choice.profile, model: x })} />}
                </div>
                <div class={line.error ? "seterr" : "setd"}>{line.text}</div>
              </div>
            );
          })}
        </>
      )}
    </section>
  );
}

/** V0.111: whether Fyr approves an agent's Fyr deploy ask with no press when the checkout is clean and on origin's
 *  default branch. On by default; only the owner changes it here, and no agent tool can. Desktop and phone alike. */
function AutoDeploySection({ api }: { api: AutoDeployApi }) {
  const [s, setS] = useState<AutoDeploySetting | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const load = async () => {
    try {
      setS(await api.autoDeploy());
    } catch (e) {
      setErr(`Could not read the auto-deploy setting: ${(e as Error).message}`);
    }
  };
  useEffect(() => void load(), []);
  const set = async (on: boolean) => {
    const r = await api.command("host.autoDeploy", undefined, { on });
    if (!r.ok) {
      setErr(r.error ?? "That did not work");
      return;
    }
    setErr(null);
    await load();
  };
  return (
    <section class="sets" aria-label="Auto-deploy">
      <h3>Auto-deploy</h3>
      <div class="setd">
        An agent's Fyr deploy runs without your press when the checkout has no uncommitted change and its commit is on origin's default branch. One deploy
        runs at a time; the build and the tests still decide the switch.
      </div>
      {err && (
        <div class="seterr" role="alert">
          {err}
        </div>
      )}
      {s === null && !err && <div class="setd">Loading…</div>}
      {s && (
        <div class="amrow">
          <span class="setn ell">Approve Fyr deploys</span>
          <button class={`btn sbtn${s.on ? " on" : ""}`} aria-pressed={s.on} aria-label="Auto-approve Fyr deploys" onClick={() => void set(!s.on)}>
            {s.on ? "On" : "Off"}
          </button>
        </div>
      )}
    </section>
  );
}

/** V0.102: per repo, whether Fyr merges an agent's merge ask with no press once its head is green. Only the owner
 *  changes it here; no agent tool can. */
function AutoMergeSection({ api }: { api: AutoMergeApi }) {
  const [list, setList] = useState<AutoMergeRepo[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [name, setName] = useState("");
  const load = async () => {
    try {
      setList(await api.autoMerge());
    } catch (e) {
      setErr(`Could not read the auto-merge setting: ${(e as Error).message}`);
      setList([]);
    }
  };
  useEffect(() => void load(), []);
  const set = async (repo: string, on: boolean) => {
    const r = await api.command("host.autoMerge", undefined, { repo, on });
    if (!r.ok) {
      setErr(r.error ?? "That did not work");
      return;
    }
    setErr(null);
    await load();
  };
  const typed = repoKey(name);
  return (
    <section class="sets" aria-label="Auto-merge">
      <h3>Auto-merge</h3>
      <div class="setd">An agent's merge ask in a repo turned on here merges without your press, once the checks pass on the head it asked for.</div>
      {err && (
        <div class="seterr" role="alert">
          {err}
        </div>
      )}
      {list === null && <div class="setd">Loading…</div>}
      {list?.length === 0 && <div class="setd">No repos yet. A repo shows here once a thread has a PR in it.</div>}
      {sortRepos(list ?? []).map((r) => (
        <div class="amrow" key={r.repo}>
          <span class="setn ell">{r.repo}</span>
          <button class={`btn sbtn${r.on ? " on" : ""}`} aria-pressed={r.on} aria-label={`Auto-merge ${r.repo}`} onClick={() => void set(r.repo, !r.on)}>
            {r.on ? "On" : "Off"}
          </button>
        </div>
      ))}
      <form
        class="amrow"
        onSubmit={(e) => {
          e.preventDefault();
          if (!typed) return;
          void set(typed, true).then(() => setName(""));
        }}
      >
        <input placeholder="owner/repo" value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)} aria-label="Repo to turn on" />
        <button class="btn sbtn" type="submit" disabled={!typed}>
          Turn on
        </button>
      </form>
    </section>
  );
}
