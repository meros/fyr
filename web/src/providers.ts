// SPEC 0.5 V0.50: Settings → Providers, as words and command arguments. Pure.
import type { Profile } from "./types.ts";
import { move } from "./reorder.ts";

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
  if (!models.length) return { error: "Add at least one model id" };
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
export function listLine(p: { kind: string; configDir?: string; baseURL?: string; tokenLogin?: boolean; models: unknown[] }): string {
  const n = p.models.length;
  return `${kindLabel(p.kind)} · ${whereText(p)} · ${n ? `${n} model${n === 1 ? "" : "s"}` : "no models"}`;
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

export type ModelsOp = { add: string } | { remove: string } | { move: [number, number] } | { star: string };

/** A provider's model ids after one edit of the Models rows, or why not. Each edit sends the whole list (and, for ★,
 *  the default). The default is not removed: star another first. */
export function modelsAfter(ids: string[], def: string | undefined, op: ModelsOp): { models: string[]; defaultModel?: string } | { error: string } {
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
