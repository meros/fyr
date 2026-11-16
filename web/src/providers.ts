// SPEC 0.5 V0.50: Settings → Providers, as words and command arguments. Pure.
import type { Profile, Project } from "./types.ts";
import { move } from "./reorder.ts";
import { shortModel } from "./format.ts";

export interface ProviderModel {
  id: string;
  name?: string;
  efforts?: string[];
}

export interface ProviderDTO {
  id: string;
  name: string;
  kind: "claude-account" | "endpoint";
  cli: string;
  color?: string;
  auth: string;
  configDir?: string;
  baseURL?: string;
  modelEnv?: Record<string, string>;
  models: ProviderModel[];
  defaultModel?: string;
  defaultEffort?: string;
  efforts: string[];
  quota: string;
  status: "ready" | "needs-login" | "needs-key";
  need?: string;
  keySet: boolean;
  keyLast4?: string;
  keyFrom?: string;
  imported?: string;
  /** an account that logs in with a token from `claude setup-token` only */
  tokenLogin?: boolean;
  threads: number;
  /** V0.53: a Claude account; it can log in through Fyr */
  canLogin?: boolean;
  /** V0.53: its current or recent login (never a token or the code) */
  login?: LoginDTO;
  /** V0.120: what the provider reports; `models` is the part that is on, in the owner's order */
  catalog?: CatalogModel[];
  /** V0.120: the last successful check (absent: never), why the last one failed, and whether one runs now */
  checkedAt?: string;
  checkError?: string;
  checking?: boolean;
}

/** V0.120: one model the provider reports. `new` already has its 14 days applied. */
export interface CatalogModel {
  id: string;
  name?: string;
  efforts?: string[];
  firstSeen: string;
  new?: boolean;
  gone?: boolean;
  manual?: boolean;
}

/** V0.53: a login Fyr runs with the CLI. */
export interface LoginDTO {
  kind: "token" | "folder";
  state: "starting" | "waiting" | "checking" | "done" | "failed" | "cancelled" | "timeout";
  url?: string;
  message?: string;
  expires: string;
}

export interface FoundDTO {
  id: string;
  name: string;
  kind: "claude-account" | "endpoint";
  source: string;
  configDir?: string;
  baseURL?: string;
  models: ProviderModel[];
  defaultModel?: string;
  quotaKind?: string;
  keyFrom?: string;
  loggedIn?: boolean;
  status: ProviderDTO["status"];
  note?: string;
  imported: boolean;
}

export const STATUS_TEXT: Record<ProviderDTO["status"], string> = {
  ready: "Ready",
  "needs-login": "Needs a login",
  "needs-key": "Needs a key",
};

export function kindLabel(kind: string): string {
  return kind === "endpoint" ? "Endpoint" : "Claude account";
}

/** "~/.claude" for the main account, the folder otherwise, or the endpoint's host. */
export function whereText(p: { kind: string; configDir?: string; baseURL?: string; tokenLogin?: boolean }): string {
  if (p.kind === "endpoint") {
    try {
      return p.baseURL ? new URL(p.baseURL).host : "no endpoint";
    } catch {
      return p.baseURL ?? "";
    }
  }
  if (p.tokenLogin) return "token login"; // V0.50: the token is the account, not the folder
  return p.configDir || "~/.claude";
}

/** What the owner sees of a key: never the key. */
export function keyLine(p: Pick<ProviderDTO, "kind" | "keySet" | "keyLast4" | "keyFrom">): string {
  const what = p.kind === "endpoint" ? "Key" : "Login token";
  if (!p.keySet) return p.kind === "endpoint" ? "Key not set" : "";
  const tail = p.keyLast4 ? ` · ••••${p.keyLast4}` : "";
  return p.keyFrom ? `${what} from ${p.keyFrom}${tail}` : `${what} set${tail}`;
}

/** One id per line or comma; blanks and repeats dropped, order kept. */
export function parseModelIds(text: string): string[] {
  const out: string[] = [];
  for (const raw of text.split(/[\n,]/)) {
    const id = raw.trim();
    if (id && !out.includes(id)) out.push(id);
  }
  return out;
}

export function modelsText(ms: ProviderModel[]): string {
  return ms.map((m) => m.id).join("\n");
}

/** "Opus 5.5, Sonnet 5.5 + 2" */
export function modelsSummary(ms: ProviderModel[], max = 2): string {
  if (!ms.length) return "No models yet";
  const names = ms.slice(0, max).map((m) => m.name || m.id);
  return ms.length > max ? `${names.join(", ")} + ${ms.length - max}` : names.join(", ");
}

/** The effort levels a profile's model takes; [] when none (effort is then not shown). */
export function effortsFor(p: Profile | undefined, model: string): string[] {
  return p?.modelCaps?.find((m) => m.id === model)?.efforts ?? [];
}

/** The effort a picker starts at: the asked one if the model takes it, else the provider's default, else "". */
export function startEffort(p: Profile | undefined, model: string, asked = ""): string {
  const es = effortsFor(p, model);
  if (asked && es.includes(asked)) return asked;
  if (p?.defaultEffort && es.includes(p.defaultEffort)) return p.defaultEffort;
  return "";
}

export interface ProviderForm {
  kind: "claude-account" | "endpoint";
  name: string;
  configDir: string;
  baseURL: string;
  key: string;
  models: string;
}

export const EMPTY_FORM: ProviderForm = { kind: "claude-account", name: "", configDir: "", baseURL: "", key: "", models: "" };

/** provider.create arguments, or what is missing. */
export function createArgs(f: ProviderForm): { args: Record<string, unknown> } | { error: string } {
  const name = f.name.trim();
  if (!name) return { error: "Give it a name" };
  const args: Record<string, unknown> = { kind: f.kind, name };
  const models = parseModelIds(f.models);
  if (models.length) args.models = models;
  if (f.kind === "claude-account") {
    if (f.configDir.trim()) args.configDir = f.configDir.trim();
    return { args };
  }
  const url = f.baseURL.trim();
  if (!/^https?:\/\/\S+$/.test(url)) return { error: "The base URL starts with https://" };
  args.baseURL = url;
  // V0.120: optional; Fyr asks the endpoint for its models as soon as it is added.
  if (f.key.trim()) args.key = f.key.trim();
  return { args };
}

/** The finds worth an Import button: not imported yet. */
export function importable(fs: FoundDTO[]): FoundDTO[] {
  return fs.filter((f) => !f.imported);
}

/** V0.53: the login still runs. */
export function loginActive(l?: LoginDTO): boolean {
  return !!l && (l.state === "starting" || l.state === "waiting" || l.state === "checking");
}

/** V0.53: the button that starts a login, or null (an endpoint, or a login runs). */
export function loginButton(p: Pick<ProviderDTO, "canLogin" | "status" | "tokenLogin" | "login">): string | null {
  if (!p.canLogin || loginActive(p.login)) return null;
  if (p.status === "needs-login") return "Log in";
  return p.tokenLogin ? "Get a new token" : "Log in again";
}

/** V0.53: what the login is doing, in a line. */
export function loginLine(l: LoginDTO): string {
  switch (l.state) {
    case "starting":
      return "Starting claude…";
    case "waiting":
      return "Open the sign-in page, sign in, then paste the code it shows here.";
    case "checking":
      return "Checking the code…";
    case "done":
      return l.kind === "token" ? "Logged in. Fyr keeps the token on this host." : "Logged in. Claude keeps the login in this account's folder.";
    case "cancelled":
      return "Login cancelled.";
    case "timeout":
      return l.message || "The login timed out. Start it again.";
    case "failed":
      return l.message || "The login did not finish. Start it again.";
  }
}

// ---- V0.119: the Providers page and a provider's page ----

/** The list row's line: "Claude account · ~/.claude · 3 models". */
export function listLine(p: { kind: string; configDir?: string; baseURL?: string; tokenLogin?: boolean; models: unknown[]; catalog?: CatalogModel[] }): string {
  const n = p.models.length;
  const fresh = newCount(p);
  return `${kindLabel(p.kind)} · ${whereText(p)} · ${n ? `${n} model${n === 1 ? "" : "s"}` : "no models"}${fresh ? ` · ${fresh} new` : ""}`;
}

const USAGE: Record<string, string> = {
  "anthropic-oauth": "Read from the Claude login",
  deepseek: "Read from the DeepSeek balance",
  openrouter: "Read from the OpenRouter credits",
};

/** The Usage group's line: where the reading comes from, where the provider was imported from, who uses it. */
export function usageLine(p: { quota: string; imported?: string; threads: number }): string {
  const src = !p.quota || p.quota === "none" ? "This provider reports no usage." : (USAGE[p.quota] ?? `Read from the provider (${p.quota})`);
  const parts = [src];
  if (p.imported) parts.push(`imported from ${p.imported}`);
  if (p.threads) parts.push(`used by ${p.threads} thread${p.threads === 1 ? "" : "s"}`);
  return parts.join(" · ");
}

/** The Remove provider row's line. */
export function removeLine(threads: number): string {
  if (!threads) return "No thread uses it.";
  return threads === 1 ? "1 thread uses it. Hand it over first." : `${threads} threads use it. Hand them over first.`;
}

export type ModelsOp = { add: string } | { remove: string } | { move: [number, number] } | { star: string } | { on: string } | { off: string } | { forget: string };

/** A provider's model ids after one edit of the Models rows, or why not. Each edit sends the whole list (and, for ★,
 *  the default). The default is not removed: star another first. */
export function modelsAfter(ids: string[], def: string | undefined, op: ModelsOp): { models: string[]; defaultModel?: string } | { forget: string[] } | { error: string } {
  // V0.120: a switch. On goes to the end of On; the default's switch is locked.
  if ("on" in op) return ids.includes(op.on) ? { models: ids } : { models: [...ids, op.on] };
  if ("off" in op) return op.off === def ? { error: "Star another model first." } : { models: ids.filter((x) => x !== op.off) };
  if ("forget" in op) return ids.includes(op.forget) ? { error: "Turn it off first." } : { forget: [op.forget] };
  if ("add" in op) {
    const id = op.add.trim();
    if (!id) return { error: "Write a model id." };
    if (ids.includes(id)) return { error: `${id} is already in the list.` };
    return { models: [...ids, id] };
  }
  if ("remove" in op) {
    if (op.remove === def) return { error: "Star another model first." };
    return { models: ids.filter((x) => x !== op.remove) };
  }
  if ("move" in op) return { models: move(ids, op.move[0], op.move[1]) };
  if (!ids.includes(op.star)) return { error: `${op.star} is not in the list.` };
  return { models: ids, defaultModel: op.star };
}

// ---- V0.120: the model chooser ----

/** One row of the Models group. */
export interface ModelRow {
  id: string;
  name: string;
  on: boolean;
  star: boolean;
  isNew: boolean;
  gone: boolean;
  manual: boolean;
}

/** How many New models a provider has (the Providers row says " · 4 new"). */
export function newCount(p: { catalog?: CatalogModel[] }): number {
  return (p.catalog ?? []).filter((m) => m.new).length;
}

/** Off shows this many before "Show N more": eight, three while searching. */
export function offLimit(searching: boolean): number {
  return searching ? 3 : 8;
}

/** The Models group's two parts: On in the owner's order (★ on the default), Off by name; both filtered by the search
 *  over name and id. Before the first check the catalog is empty: the stored list is On and nothing is Off. A model
 *  that is off and gone has left the catalog on the host. */
export function modelParts(p: Pick<ProviderDTO, "models" | "defaultModel" | "catalog">, q = ""): { on: ModelRow[]; off: ModelRow[]; total: number; matched: number } {
  const cat = new Map((p.catalog ?? []).map((m) => [m.id, m]));
  const onIds = new Set(p.models.map((m) => m.id));
  const row = (id: string, name: string | undefined, on: boolean): ModelRow => {
    const c = cat.get(id);
    return { id, name: name || c?.name || id, on, star: on && id === p.defaultModel, isNew: !!c?.new, gone: !!c?.gone, manual: !!c?.manual };
  };
  const on = p.models.map((m) => row(m.id, m.name, true));
  const off = (p.catalog ?? [])
    .filter((m) => !onIds.has(m.id))
    .map((m) => row(m.id, m.name, false))
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
  const total = on.length + off.length;
  const s = q.trim().toLowerCase();
  if (!s) return { on, off, total, matched: total };
  const hit = (m: ModelRow) => m.id.toLowerCase().includes(s) || m.name.toLowerCase().includes(s);
  const on2 = on.filter(hit);
  const off2 = off.filter(hit);
  return { on: on2, off: off2, total, matched: on2.length + off2.length };
}

const pad = (n: number) => String(n).padStart(2, "0");
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];

/** "today", "yesterday" or "14 Nov" for a time, in this device's time zone. */
function dayWord(t: Date, now: number): string {
  const d0 = new Date(now);
  d0.setHours(0, 0, 0, 0);
  const day = new Date(t);
  day.setHours(0, 0, 0, 0);
  const diff = Math.round((d0.getTime() - day.getTime()) / 86_400_000);
  return diff === 0 ? "today" : diff === 1 ? "yesterday" : `${t.getDate()} ${MONTHS[t.getMonth()]}`;
}

/** When a check was: "just now", "12 min ago", "today 09:10", "yesterday 14:02", "14 Nov 14:02". */
function checkedWhen(at: string, now: number): string {
  const t = new Date(at);
  const min = Math.floor((now - t.getTime()) / 60_000);
  if (min < 1) return "just now";
  if (min < 60) return `${min} min ago`;
  return `${dayWord(t, now)} ${pad(t.getHours())}:${pad(t.getMinutes())}`;
}

/** The Models header's line: checking, when it was checked, or why it is not. */
export function checkLine(p: Pick<ProviderDTO, "status" | "checkedAt" | "checkError" | "checking">, now: number): string {
  if (p.status === "needs-login") return "Not checked: needs a login";
  if (p.status === "needs-key") return "Not checked: needs a key";
  if (p.checking) return "Checking…";
  if (!p.checkedAt) return "Not checked yet";
  return `${p.checkError ? "Last checked" : "Checked"} ${checkedWhen(p.checkedAt, now)}`;
}

/** The foot's line in red after a failed check, or null. */
export function checkErrorLine(p: Pick<ProviderDTO, "checkedAt" | "checkError">, now: number): string | null {
  if (!p.checkError) return null;
  const err = p.checkError.replace(/\.$/, "");
  if (!p.checkedAt) return `Could not list the models: ${err}.`;
  return `Could not list the models: ${err}. The list below is the one from ${dayWord(new Date(p.checkedAt), now)}; it still works.`;
}

/** The page asks the provider when it opens and the last check is over 10 minutes old; never for one that needs a
 *  login or a key, and not while a check runs. */
export function needsCheck(p: Pick<ProviderDTO, "status" | "checkedAt" | "checking">, now: number): boolean {
  if (p.status !== "ready" || p.checking) return false;
  return !p.checkedAt || now - Date.parse(p.checkedAt) > 10 * 60_000;
}

/** V0.120 (5): every other model picker lists what is on; a thread, job or project whose model is off still shows
 *  it, as "opus 5.4 (off)", until another is picked. */
export function withCurrent<T extends { value: string; label: string }>(options: T[], value: string | undefined): T[] {
  if (!value || options.some((o) => o.value === value)) return options;
  return [...options, { value, label: offLabel(value) } as T];
}

export function offLabel(model: string): string {
  return `${shortModel(model)} (off)`;
}

/** V0.120 (5): a project whose default model is off says so on its row (C12), or null. */
export function projectModelOff(pr: Pick<Project, "name" | "profile" | "model">, profiles: Profile[]): string | null {
  const prof = profiles.find((x) => x.id === pr.profile);
  if (!prof || !pr.model || prof.models.includes(pr.model)) return null;
  return `${shortModel(pr.model)} is turned off in ${prof.name}. New threads here start on ${prof.name}'s default until you pick another.`;
}
