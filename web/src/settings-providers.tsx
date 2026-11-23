// The Providers settings page (split from settings-ui.tsx).

import { subKey } from "./settings-ui.tsx";
import { activeTabId, type HostTabItem } from "./host-tabs.ts";
import { HostTabs } from "./host-tabs-ui.tsx";
import { useEffect, useRef, useState } from "preact/hooks";
import { type ComponentChildren } from "preact";
import type { HostId } from "./hostid.ts";
import { groupLine, readOnlyNote } from "./host-settings.ts";
import { quotaFor } from "./quota.ts";
import { QuotaLine, useQuotas } from "./quota-ui.tsx";
import { CARD_WORD, cardState } from "./quota.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { removeProviderAsk } from "./confirm.ts";
import { useAsk } from "./confirm-ui.tsx";
import { jobLine, jobProviderOptions, jobShowsModel, modelOptions, providerOptions, type Option, type SystemAgentChoice } from "./system-agents.ts";
import { type PageRef } from "./settings-nav.ts";
import { Empty, Group, LoadError, Row, SaveField, SetContext, Skeleton, Switch, useSet } from "./settings-rows.tsx";
import { Grip, MoveControls, ReorderButton, useReorder } from "./reorder-ui.tsx";
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
  type LoginDTO,
  type ModelRow,
  type ModelsOp,
  type ProviderDTO,
  type ProviderForm,
} from "./providers.ts";
import { type Run, type Data, type AllSettings } from "./settings-load.ts";
import { clockTime } from "./format.ts";

// ---- Providers ----

/** V0.127, V0.host-tabs: the per-host pages, tabbed by host when the front has several. With one host the part has no
 *  tabs or heading, as before; with several, HostTabs picks one host at a time (in the owner's host order) with its
 *  mark, offline state and attention count, and only that host's panel renders below. */
export function HostParts({
  s,
  now,
  picked,
  onPick,
  badge,
  onlineOnly,
  part,
}: {
  s: AllSettings;
  now: number;
  picked: string | null;
  onPick(host: string): void;
  badge?(d: Data): number;
  onlineOnly?: boolean;
  part(d: Data): ComponentChildren;
}) {
  if (!s.grouped) return <>{part(s.all[0])}</>;
  const items: HostTabItem[] = s.all.map((d) => ({
    id: d.host.name,
    name: d.host.name,
    label: d.host.label,
    offline: !d.host.online || !!d.host.noId,
    badge: badge ? badge(d) : undefined,
  }));
  const activeId = activeTabId(items, picked);
  const d = s.all.find((x) => x.host.name === activeId) ?? s.all[0];
  const note = onlineOnly && !d.host.online && !d.host.noId ? `${d.host.label} is offline. Live setup checks run once it is back.` : readOnlyNote(d.host, d.readAt, now);
  // V0.host-ids: a host with no install id is listed with why nothing of it shows.
  const nothing = !!d.host.noId || (onlineOnly && !d.host.online) || (!d.host.online && !d.readAt && d.providers === null && d.deploy === null && d.repos === null);
  return (
    <>
      <HostTabs items={items} active={d.host.name} onPick={onPick} label="Hosts" />
      <section key={d.host.name} class={`hpart${d.host.online && !d.host.noId ? "" : " off"}`} role="tabpanel" data-host={d.host.name} aria-label={`Settings on ${d.host.label}`}>
        <div class="hpart-h">
          <span class="hpart-s">{groupLine(d.host, now)}</span>
        </div>
        {note && <div class="note hnote">{note}</div>}
        {!nothing && <HostEnv d={d}>{part(d)}</HostEnv>}
      </section>
    </>
  );
}

/** A host's controls are read-only while it is offline (V0.127), as the whole page is with no connection. */
export function HostEnv({ d, children }: { d: Data; children: ComponentChildren }) {
  const env = useSet();
  return <SetContext.Provider value={{ ...env, readOnly: env.readOnly || !d.host.online }}>{children}</SetContext.Provider>;
}

/** The status badge: today's words; V0.77 (1), #65: it tells the truth about the account, so Ready never sits beside
 *  its own error. V0.127: the quota is its host's. */
export function ProviderBadge({ p, host }: { p: ProviderDTO; host: string }) {
  const quota = quotaFor(useQuotas(), p.id, host || undefined);
  return <span class={`pst ${p.status}`}>{quota && p.status === "ready" ? CARD_WORD[cardState(quota)] : STATUS_TEXT[p.status]}</span>;
}

export function ProvidersPage({ d, go, grouped }: { d: Data; go(r: PageRef): void; grouped: boolean }) {
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
          <Empty title="No providers yet" text="A provider is a Claude or Antigravity login, or an API endpoint that threads run on." />
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
                  {kindLabel(f.kind, f.cli)} · {f.keyFrom ? `key from ${f.keyFrom}` : f.source}
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
        const showModel = jobShowsModel(j);
        return (
          <Row
            key={j.id}
            title={j.name}
            desc={line.error ? undefined : line.text}
            error={rowErr(j.id) ?? (line.error ? line.text : null)}
            stack
            class={`sajob${showModel ? " two" : ""}`}
            end={
              <>
                <Select label={j.name} value={j.override ? (j.choice.profile ?? "") : "="} options={jobProviderOptions(v, j)} onPick={(x) => void set(j.id, j.id, x === "=" ? null : { profile: x })} />
                {showModel && <Select label="Model" value={j.choice.model ?? ""} options={withCurrent(modelOptions(v, own), j.choice.model)} onPick={(x) => void set(j.id, j.id, { profile: j.choice.profile, model: x })} />}
              </>
            }
          />
        );
      })}
    </Group>
  );
}

/** A provider's page: what blocks it first (a login, a key), its models, its usage, Remove last. */
export function ProviderPage({ p, host, run, refresh, now, clockKnown, onRemoved }: { p: ProviderDTO; host: string; run: Run; refresh(): Promise<string | null>; now: number; clockKnown: boolean; onRemoved(): void }) {
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
      {p.auth !== "none" && p.kind !== "agy-account" && <KeyRow p={p} run={run} />}
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
  const ro = useReorder(ids, (next, _from, to) => apply(next[to], { order: next }));
  const canReorder = !searching && ids.length > 1 && !env.readOnly; // while the search has text, reorder is off
  const [mode, setMode] = useState(false); // the phone's Reorder mode
  const reo = env.phone && mode && canReorder;
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
          {!reo && p.status === "ready" && (
            <button type="button" class="lnk" disabled={checking || p.checking || env.readOnly} onClick={() => void doRefresh()}>
              Refresh
            </button>
          )}
          {
            // The phone reorders in a mode (reorder-ui.tsx says why). The desktop drags the grip, so it has no button.
            env.phone && canReorder && <ReorderButton on={mode} set={setMode} />
          }
        </>
      }
      foot={foot.text}
      footErr={foot.error}
    >
      {rowErr("check") && <Row error={rowErr("check")} />}
      {!reo && parts.total >= 9 && (
        <Row class="srch">
          <input class="pq" type="search" placeholder={`Search ${parts.total} models`} aria-label="Search models" value={q} onInput={(e) => (setQ((e.target as HTMLInputElement).value), setMore(false))} />
        </Row>
      )}
      {parts.on.length > 0 && <div class="sg-sub">On · offered in every model picker, in this order</div>}
      {(searching ? parts.on : ro.sort(parts.on, (m) => m.id)).map((m) => {
        const i = ro.ids.indexOf(m.id);
        const r = ro.row(i);
        return (
          <Row
            key={m.id}
            class={`model ${r.class}`}
            rowRef={r.ref}
            data={{ model: m.id }}
            error={rowErr(m.id)}
            lead={
              !reo && (
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
            end={reo ? undefined : <Switch on label={`Offer ${m.id}`} onChange={() => void apply(m.id, { off: m.id })} />}
          >
            {reo && <MoveControls ro={ro} i={i} name={m.name} />}
          </Row>
        );
      })}
      {!reo && parts.off.length > 0 && <div class="sg-sub">Off · {parts.off.length}</div>}
      {!reo &&
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
      {!reo && !more && parts.off.length > limit && (
        <button type="button" class="more" onClick={() => setMore(true)}>
          Show {parts.off.length - limit} more
        </button>
      )}
      {!reo && searching && parts.matched === 0 && <Row desc={`No model matches “${q.trim()}”.`} />}
      {!reo && (
        <Row error={rowErr("+")} class="fieldrow">
          <SaveField value="" label="Model id to add" placeholder="Add a model id by hand" mono clears alwaysButton button="Add" onError={(e) => setErr(e ? { row: "+", text: e } : null)} onSave={async (v) => (await apply("+", { add: v })) ?? null} check={(v) => { const r = modelsAfter(ids, p.defaultModel, { add: v }); return "error" in r ? r.error : null; }} />
        </Row>
      )}
      {!reo && hasEfforts && (
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
  const send = () => void run("provider.loginCode", { id: p.id, code }).then((e) => (setErr(e), !e && setCode("")));
  return (
    <div class={`login ${l.state}`} aria-live="polite">
      <div class="setd">{loginLine(l, p.cli)}</div>
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
            <span class="setd">The sign-in ends at {clockTime(l.expires)}</span>
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
export function AddProvider(p: { /** slice 2b: the host the provider is added on, whose folders the picker reads */ host?: HostId; onCancel(): void; onCreate(args: Record<string, unknown>): Promise<string | null> }) {
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
  const kindKey = f.kind === "endpoint" && f.cli === "agy" ? "agy-endpoint" : f.kind;
  const setKindKey = (v: string) => {
    if (v === "agy-endpoint") setF({ ...f, kind: "endpoint", cli: "agy" });
    else setF({ ...f, kind: v as ProviderForm["kind"], cli: undefined });
  };
  return (
    <>
      <Group>
        <Row
          title="Kind"
          stack
          end={
            <select aria-label="Kind" value={kindKey} disabled={env.readOnly} onChange={(e) => setKindKey((e.target as HTMLSelectElement).value)}>
              <option value="claude-account">{kindLabel("claude-account")}</option>
              <option value="agy-account">{kindLabel("agy-account")}</option>
              <option value="agy-endpoint">{kindLabel("endpoint", "agy")}</option>
              <option value="endpoint">{kindLabel("endpoint")}</option>
            </select>
          }
        />
        <Row col>
          <label class="fld">
            <span>Name</span>
            <input value={f.name} onInput={set("name")} placeholder={kindKey === "agy-endpoint" ? "Gemini API" : f.kind === "endpoint" ? "OpenRouter" : f.kind === "agy-account" ? "Antigravity" : "Claude (work)"} disabled={env.readOnly} />
          </label>
        </Row>
        {f.kind === "claude-account" || f.kind === "agy-account" ? (
          <Row col>
            <label class="fld">
              <span>Config folder (empty: {f.kind === "agy-account" ? "~/.gemini" : "~/.claude"})</span>
              <div class="setkey">
                <input value={f.configDir} onInput={set("configDir")} placeholder={f.kind === "agy-account" ? "~/.gemini-work" : "~/.claude-work"} disabled={env.readOnly} />
                <button type="button" class="btn" disabled={env.readOnly} onClick={() => void picker.pick({ title: "Config folder", ...(p.host ? { host: p.host } : {}) }).then((d) => d && setF({ ...f, configDir: d }))}>
                  Choose…
                </button>
              </div>
            </label>
            <span class="sr-d">After adding it, press Log in on its page: Fyr runs {f.kind === "agy-account" ? "agy" : "claude"}'s login in this folder and shows the sign-in link there.</span>
          </Row>
        ) : (
          <>
            <Row col>
              <label class="fld">
                <span>Base URL{f.cli === "agy" ? " (optional: Gemini API)" : ""}</span>
                <input value={f.baseURL} onInput={set("baseURL")} placeholder={f.cli === "agy" ? "https://generativelanguage.googleapis.com" : "https://openrouter.ai/api"} inputMode="url" disabled={env.readOnly} />
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
            <span>Model ids, one per line{f.kind === "claude-account" ? " (empty: Claude's list)" : f.kind === "agy-account" || f.cli === "agy" ? " (empty: Antigravity's list)" : " (optional: Fyr asks the endpoint for its list)"}</span>
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

