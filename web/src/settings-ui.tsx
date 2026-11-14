// SPEC 0.5 V0.50: Settings. Providers (list, Found on this host, Add provider), Hosts, Notifications,
// Appearance. The same body on desktop (a dialog) and phone (a screen).

import { useEffect, useState } from "preact/hooks";
import type { ComponentChildren } from "preact";
import type { Api } from "./api.ts";
import { hostStatus, type HostInfo } from "./hosts.ts";
import type { Quota } from "./quota.ts";
import { quotaFor } from "./quota.ts";
import { QuotaLine, useQuotas } from "./quota-ui.tsx";
import { useFolderPick } from "./folder-picker.tsx";
import {
  createArgs,
  EMPTY_FORM,
  importable,
  keyLine,
  kindLabel,
  modelsSummary,
  modelsText,
  parseModelIds,
  STATUS_TEXT,
  whereText,
  type FoundDTO,
  type ProviderDTO,
  type ProviderForm,
} from "./providers.ts";

export interface SettingsProps {
  api: Pick<Api, "providers" | "providersFound" | "command">;
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
}

export function Settings(p: SettingsProps) {
  return (
    <div class={`set${p.phone ? " phone" : ""}`}>
      <ProvidersSection {...p} />
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
        <span class={`pst ${p.status}`}>{STATUS_TEXT[p.status]}</span>
      </button>
      {quota && <QuotaLine q={quota} name={p.name} />}
      {p.need && <div class="setneed">{p.need}</div>}
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
          <div class="setd">
            {p.quota !== "none" ? `Quota: ${p.quota}` : "No quota reading"}
            {p.imported ? ` · imported from ${p.imported}` : ""}
            {p.threads ? ` · ${p.threads} thread${p.threads === 1 ? "" : "s"}` : ""}
          </div>
          <div class="setbar">
            <button class="btn sbtn danger" disabled={p.threads > 0} title={p.threads > 0 ? "Threads use it; hand them over first" : ""} onClick={() => confirm(`Remove ${p.name}? Its stored key is deleted too.`) && void run("provider.delete", { id: p.id })}>
              Remove
            </button>
          </div>
        </div>
      )}
    </li>
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
          <span class="setd">Log in once with this folder; Fyr then runs claude with it.</span>
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
