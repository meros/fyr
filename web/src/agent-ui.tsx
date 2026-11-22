/** V0.agent-pill: the agent a thread runs as (provider, model, effort) in one compact pill, on both fronts: the desktop
 *  thread header, the phone thread header, the phone Details sheet and both New thread screens. Each part that can
 *  change holds its own native select, laid over it (the platform picker, also on the phone). On New thread the
 *  provider and model are one part over V0.model-choice's one choice ("Claude · Opus 5.5"), and the effort is its
 *  Effort select. */
import type { ComponentChildren } from "preact";
import type { Profile, Thread } from "./types.ts";
import { effortsFor, offLabel, startEffort } from "./providers.ts";
import { modelName } from "./model-choice.ts";
import { EffortSelect, ModelSelect } from "./model-choice-ui.tsx";

interface AgentOption {
  value: string;
  label: string;
}

/** One part: what it shows (k is its class: agv provider, agm model, age effort), and how it picks, if it can:
 *  options for a plain select, or a picker element (a select) laid over the part. */
interface AgentSeg {
  k: "agv" | "agm" | "age";
  text: ComponentChildren;
  title: string;
  aria?: string;
  value?: string;
  options?: AgentOption[];
  onPick?(v: string): void;
  picker?: ComponentChildren;
}

/** The effort meter: one bar per level the model takes, filled up to the current one; none filled for the CLI default. */
function EffortBars({ efforts, cur }: { efforts: string[]; cur: string }) {
  const at = efforts.indexOf(cur);
  return (
    <span class="agb" aria-hidden="true">
      {efforts.map((_, i) => (
        <i class={i <= at ? "on" : ""} style={{ height: `${4 + Math.round((i * 6) / Math.max(1, efforts.length - 1))}px` }} />
      ))}
    </span>
  );
}

function effortText(efforts: string[], cur: string): ComponentChildren {
  return (
    <>
      <EffortBars efforts={efforts} cur={cur} />
      <span class="agt">{cur || "default"}</span>
    </>
  );
}

function Seg({ s }: { s: AgentSeg }) {
  const select =
    s.picker ??
    (s.onPick && s.options && s.options.length > 1 ? (
      <select value={s.value} aria-label={s.aria} onChange={(e) => s.onPick!((e.target as HTMLSelectElement).value)}>
        {s.options.map((o) => (
          <option value={o.value}>{o.label}</option>
        ))}
      </select>
    ) : null);
  return (
    <span class={`ags ${s.k}${select ? " pick" : ""}`} title={select ? `${s.title} · change` : s.title}>
      {s.text}
      {select}
    </span>
  );
}

/** The pill. size "chip" sits in a header's chip row (24 px desktop, 28 px phone); "field" is a form's full-width
 *  control (34 px desktop, 48 px phone: a finger's size, the phone's stated difference). */
export function AgentPill(p: { segs: (AgentSeg | null)[]; size: "chip" | "field"; phone?: boolean }) {
  return (
    <span class={`agp ${p.size}${p.phone ? " ph" : ""}`} role="group" aria-label="Agent">
      {p.segs.map((s) => s && <Seg s={s} />)}
    </span>
  );
}

/** A part shows only: no picker. */
export const showOnly = (s: AgentSeg | null): AgentSeg | null => s && { ...s, onPick: undefined, picker: undefined };

/** A thread's parts. The provider only shows: another provider (and its model and effort) is Hand over
 *  (V0.model-choice). The model, on this provider, and the effort change with thread.setModel unless `reason` says why
 *  not (then it is in their title). V0.120 (5): a model the owner turned off stays, marked "(off)", until another is
 *  picked. */
export function threadAgent(
  t: Pick<Thread, "model" | "effort">,
  profile: Profile | undefined,
  provider: { name: string; color?: string; title: string },
  reason: string | undefined,
  set: (args: { model?: string; effort?: string }) => void,
): AgentSeg[] {
  const models = profile?.models ?? [];
  const off = !!profile && !!t.model && !models.includes(t.model);
  const why = reason ? ` · ${reason}` : "";
  const efforts = effortsFor(profile, t.model);
  const cur = startEffort(profile, t.model, t.effort);
  const name = (m: string) => modelName(profile, m);
  const segs: AgentSeg[] = [
    {
      k: "agv",
      text: (
        <>
          <span class="pdot" style={provider.color ? { background: provider.color } : undefined} />
          <span class="agt">{provider.name}</span>
        </>
      ),
      title: provider.title,
    },
    {
      k: "agm",
      text: <span class="agt">{off ? offLabel(t.model) : name(t.model)}</span>,
      title: `Model ${t.model}${why}`,
      aria: "Model",
      value: t.model,
      options: models.length ? [...(off ? [{ value: t.model, label: offLabel(t.model) }] : []), ...models.map((m) => ({ value: m, label: name(m) }))] : undefined,
      onPick: reason ? undefined : (model) => set({ model }),
    },
  ];
  if (efforts.length)
    segs.push({
      k: "age",
      text: effortText(efforts, cur),
      title: `Effort ${cur || "default"}${why}`,
      aria: "Effort",
      value: cur,
      options: [...(cur ? [] : [{ value: "", label: "CLI default" }]), ...efforts.map((x) => ({ value: x, label: x }))],
      onPick: reason ? undefined : (effort) => set({ effort }),
    });
  return segs;
}

/** The New thread screens' parts (both fronts, from useNewThreadForm): provider · model as one part over
 *  V0.model-choice's ModelSelect (grouped by provider, with each one's quota), and the effort over its EffortSelect. */
export function newThreadAgent(
  o: { profile: string; model: string; effort: string; efforts: string[] },
  profiles: Profile[],
  pickModel: (profile: string, model: string) => void,
  pickEffort: (e: string) => void,
): AgentSeg[] {
  const prof = profiles.find((x) => x.id === o.profile);
  const name = prof?.name ?? o.profile;
  const model = o.model ? modelName(prof, o.model) : "its default model";
  const segs: AgentSeg[] = [
    {
      k: "agv",
      text: (
        <>
          <span class="pdot" style={prof?.color ? { background: prof.color } : undefined} />
          <span class="agt">{name}</span>
          <span class="agsep" aria-hidden="true" />
          <span class="agt agmn">{model}</span>
        </>
      ),
      title: `${name} · ${model}`,
      picker: <ModelSelect profiles={profiles} profile={o.profile} model={o.model} onPick={pickModel} label="Provider and model" />,
    },
  ];
  if (o.efforts.length)
    segs.push({ k: "age", text: effortText(o.efforts, o.effort), title: `Effort ${o.effort || "default"}`, picker: <EffortSelect efforts={o.efforts} effort={o.effort} onPick={pickEffort} /> });
  return segs;
}
