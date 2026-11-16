// SPEC 0.5 V0.50: Settings → Providers in mock mode. In memory; no key ever leaves this module either.
import type { CommandReply } from "./types.ts";
import type { FoundDTO, ProviderDTO } from "./providers.ts";

const EFFORTS = ["low", "medium", "high", "xhigh", "max"];
const CLAUDE_MODELS = [
  { id: "claude-opus-5-5", name: "Opus 5.5", efforts: EFFORTS },
  { id: "claude-sonnet-5-5", name: "Sonnet 5.5", efforts: EFFORTS },
  { id: "claude-haiku-5-5", name: "Haiku 5.5" },
];

/** mode: "" (three providers), "none" (no providers yet), "nokey" (DeepSeek's key is missing). */
export function createMockProviders(mode = "") {
  const keys = new Map<string, string>(mode === "nokey" ? [] : [["claudeDeepseek", "sk-mock-deepseek-4f2a"]]);
  const list: ProviderDTO[] = [
    { id: "claude", name: "Claude", kind: "claude-account", cli: "claude", auth: "cli-login", color: "#d97757", models: CLAUDE_MODELS, defaultModel: "claude-opus-5-5", defaultEffort: "high", efforts: EFFORTS, quota: "anthropic-oauth", status: "ready", keySet: false, imported: "profile claudeAlt", threads: 9, canLogin: true },
    { id: "work", name: "Claude (work account)", kind: "claude-account", cli: "claude", auth: "cli-login", color: "#2f6fb0", configDir: "~/.claude-work", models: CLAUDE_MODELS.slice(1, 2), defaultModel: "claude-sonnet-5-5", efforts: EFFORTS, quota: "anthropic-oauth", status: "needs-login", need: "This account logs in with a token. Press Log in to get one here (Fyr keeps it), or paste a token from `claude setup-token` as its key.", tokenLogin: true, keySet: false, imported: "profile work (claude-work)", threads: 2, canLogin: true,
      // V0.53: a login in progress, so the panel shows (and is measured) in mock mode.
      login: { kind: "token", state: "waiting", url: "https://claude.com/cai/oauth/authorize?code=true&client_id=mock", expires: new Date(Date.now() + 10 * 60_000).toISOString() } },
    { id: "claudeDeepseek", name: "DeepSeek", kind: "endpoint", cli: "claude", auth: "api-key", baseURL: "https://api.deepseek.com/anthropic", models: [{ id: "deepseek-chat" }, { id: "deepseek-reasoner" }], defaultModel: "deepseek-chat", efforts: EFFORTS, quota: "deepseek", status: "ready", keySet: true, keyLast4: "4f2a", threads: 0 },
  ];
  if (mode === "none") list.length = 0;
  if (mode === "nokey") Object.assign(list[2], { keySet: false, keyLast4: undefined, status: "needs-key", need: "Paste the API key for DeepSeek." });
  const found: FoundDTO[] = [
    { id: "openrouter", name: "OpenRouter", kind: "endpoint", source: "OPENROUTER_API_KEY in fyrd's environment", baseURL: "https://openrouter.ai/api", models: [], quotaKind: "openrouter", keyFrom: "OPENROUTER_API_KEY in fyrd's environment", status: "ready", note: "Add the model ids this endpoint should offer.", imported: false },
    { id: "claude-old", name: "Claude (old)", kind: "claude-account", source: "~/.claude-old", configDir: "~/.claude-old", models: CLAUDE_MODELS, loggedIn: true, status: "ready", imported: false },
  ];
  const fail = (error: string): CommandReply => ({ ok: false, error });
  const get = (id: unknown) => list.find((p) => p.id === id);
  const keyed = (p: ProviderDTO) => {
    const k = keys.get(p.id);
    p.keySet = !!k;
    p.keyLast4 = k && k.length >= 12 ? k.slice(-4) : undefined;
    if (p.kind === "endpoint") p.status = k ? "ready" : "needs-key";
    if (p.kind === "endpoint") p.need = k ? undefined : `Paste the API key for ${p.name}.`;
  };
  return {
    list: (): ProviderDTO[] => list.map((p) => ({ ...p })),
    found: (): FoundDTO[] => found.map((f) => ({ ...f, imported: list.some((p) => p.id === f.id) })),
    command(action: string, a: Record<string, unknown>): CommandReply {
      const models = (x: unknown) => (Array.isArray(x) ? x.map((id) => ({ id: String(id), ...(CLAUDE_MODELS.find((m) => m.id === id) ?? {}) })) : undefined);
      switch (action) {
        case "provider.import": {
          const f = found.find((x) => x.id === a.foundId);
          if (!f || get(f.id)) return fail("already imported");
          list.push({ id: f.id, name: f.name, kind: f.kind, cli: "claude", auth: f.kind === "endpoint" ? "api-key" : "cli-login", configDir: f.configDir, baseURL: f.baseURL, models: f.models, efforts: EFFORTS, quota: f.quotaKind ?? "none", status: f.status, keySet: !!f.keyFrom, keyLast4: f.keyFrom ? "9a8b" : undefined, imported: f.source, threads: 0 });
          return { ok: true };
        }
        case "provider.create": {
          const name = String(a.name ?? "").trim();
          const id = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "provider";
          if (get(id)) return fail(`a provider ${id} exists`);
          const kind = a.kind === "endpoint" ? "endpoint" : "claude-account";
          const p: ProviderDTO = { id, name, kind, cli: "claude", auth: kind === "endpoint" ? "api-key" : "cli-login", configDir: a.configDir as string | undefined, baseURL: a.baseURL as string | undefined, models: models(a.models) ?? (kind === "endpoint" ? [] : CLAUDE_MODELS), efforts: EFFORTS, quota: kind === "endpoint" ? "none" : "anthropic-oauth", status: kind === "endpoint" ? "needs-key" : "needs-login", keySet: false, threads: 0 };
          p.defaultModel = p.models[0]?.id;
          if (a.key) keys.set(id, String(a.key));
          keyed(p);
          list.push(p);
          return { ok: true };
        }
        case "provider.update": {
          const p = get(a.id);
          if (!p) return fail("no provider");
          if (typeof a.name === "string") p.name = a.name;
          const ms = models(a.models);
          if (ms) p.models = ms;
          if (typeof a.defaultModel === "string") p.defaultModel = a.defaultModel;
          if (!p.models.some((m) => m.id === p.defaultModel)) p.defaultModel = p.models[0]?.id;
          if (typeof a.defaultEffort === "string") p.defaultEffort = a.defaultEffort || undefined;
          return { ok: true };
        }
        case "provider.setKey": {
          const p = get(a.id);
          if (!p) return fail("no provider");
          const k = String(a.key ?? "").trim();
          if (k) keys.set(p.id, k);
          else keys.delete(p.id);
          keyed(p);
          if (p.kind === "claude-account" && k) (p.status = "ready"), (p.need = undefined);
          return { ok: true };
        }
        // V0.53: login through Fyr. Any code logs in; nothing is kept.
        case "provider.login": {
          const p = get(a.id);
          if (!p || !p.canLogin) return fail("not a Claude account");
          if (p.login && ["starting", "waiting", "checking"].includes(p.login.state)) return fail(`A login for ${p.name} is already running`);
          p.login = { kind: p.tokenLogin ? "token" : "folder", state: "waiting", url: "https://claude.com/cai/oauth/authorize?code=true&client_id=mock", expires: new Date(Date.now() + 10 * 60_000).toISOString() };
          return { ok: true };
        }
        case "provider.loginCode": {
          const p = get(a.id);
          if (!p?.login || p.login.state !== "waiting") return fail("no login is running; start it again");
          if (!String(a.code ?? "").trim()) return fail("paste the code the sign-in page shows");
          p.login = { ...p.login, state: "done", message: undefined };
          if (p.tokenLogin) (p.keySet = true), (p.keyLast4 = "mock");
          p.status = "ready";
          p.need = undefined;
          return { ok: true };
        }
        case "provider.loginCancel": {
          const p = get(a.id);
          if (p?.login) p.login = { ...p.login, state: "cancelled" };
          return { ok: true };
        }
        case "provider.delete": {
          const i = list.findIndex((p) => p.id === a.id);
          if (i < 0) return fail("no provider");
          if (list[i].threads > 0) return fail(`${list[i].threads} threads use ${list[i].name}; hand them over first`);
          list.splice(i, 1);
          keys.delete(String(a.id));
          return { ok: true };
        }
      }
      return fail(`unknown action ${action}`);
    },
  };
}
