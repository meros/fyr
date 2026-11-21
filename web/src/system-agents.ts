// SPEC V0.115: the system agents setting. The model calls Fyr starts itself (thread titles, project setup) run on the
// provider and model the owner picks here, per host; no provider is the host's default, no model is its Haiku.

/** A provider and a model; "" (or missing) is the default. */
export interface SystemAgentChoice {
  profile?: string;
  model?: string;
}

/** A provider of the host as the setting offers it. */
export interface SystemProvider {
  id: string;
  name: string;
  models: string[];
  /** its Haiku model; missing when it has none */
  haiku?: string;
  unavailable?: string;
}

/** Sentinel profile on a thread-defaulting job (e.g. approval) to follow System agents Default. */
export const PROFILE_SYSTEM_DEFAULT = "$default";

/** One job: its override, and what it runs on now or why it cannot run. */
export interface SystemJob {
  id: string;
  name: string;
  defaultThread?: boolean;
  override?: boolean;
  choice: SystemAgentChoice;
  profile?: string;
  model?: string;
  error?: string;
}

/** GET /api/systemAgents?host=, and the reply of host.systemAgents. */
export interface SystemAgentsView {
  default: SystemAgentChoice;
  jobs: SystemJob[];
  providers: SystemProvider[];
}

export interface Option {
  value: string;
  label: string;
}

function providerOf(v: SystemAgentsView, id: string | undefined): SystemProvider | undefined {
  return id ? v.providers.find((p) => p.id === id) : v.providers[0];
}

/** The providers to pick: the host's default first (its first provider), then each by name. */
export function providerOptions(v: SystemAgentsView): Option[] {
  const first = v.providers[0];
  return [
    { value: "", label: first ? `Default (${first.name || first.id})` : "Default" },
    ...v.providers.map((p) => ({ value: p.id, label: p.unavailable ? `${p.name || p.id} (cannot run: ${p.unavailable})` : p.name || p.id })),
  ];
}

/** The models to pick on a provider ("" is the default provider): its fast model (Haiku / Flash) first, then its models. */
export function modelOptions(v: SystemAgentsView, profile: string): Option[] {
  const p = providerOf(v, profile);
  const haiku =
    p?.haiku && !p.haiku.toLowerCase().includes("haiku")
      ? `Fast (${p.haiku})`
      : p?.haiku
        ? `Haiku (${p.haiku})`
        : `Haiku (none on ${p?.name || p?.id || "this provider"})`;
  return [{ value: "", label: haiku }, ...(p?.models ?? []).map((m) => ({ value: m, label: m }))];
}

/** Provider choices for a single job row. */
export function jobProviderOptions(v: SystemAgentsView, j: SystemJob): Option[] {
  const concrete = providerOptions(v).slice(1);
  if (j.defaultThread) {
    return [
      { value: "=", label: "Thread's provider (auto)" },
      { value: PROFILE_SYSTEM_DEFAULT, label: "Same as default" },
      ...concrete,
    ];
  }
  return [{ value: "=", label: "Same as default" }, ...concrete];
}

/** Whether a job row shows the per-job model selector. */
export function jobShowsModel(j: SystemJob): boolean {
  return !!j.override && j.choice.profile !== PROFILE_SYSTEM_DEFAULT;
}

/** What a job runs on now, or why it cannot run. */
export function jobLine(v: SystemAgentsView, j: SystemJob): { text: string; error: boolean } {
  if (j.error) return { text: j.error, error: true };
  if (j.defaultThread && !j.override) return { text: "Runs on each thread's provider (fast model)", error: false };
  const p = providerOf(v, j.profile);
  return { text: `Runs on ${p?.name || j.profile || "?"} · ${j.model ?? "?"}`, error: false };
}
