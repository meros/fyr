// SPEC 0.5 V0.47: Settings → Providers, as words and command arguments. Pure.
import type { Profile } from "./types.ts";

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
export function whereText(p: { kind: string; configDir?: string; baseURL?: string }): string {
  if (p.kind === "endpoint") {
    try {
      return p.baseURL ? new URL(p.baseURL).host : "no endpoint";
    } catch {
      return p.baseURL ?? "";
    }
  }
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
