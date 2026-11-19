// V0.model-choice: one select for provider and model ("Claude · Opus 5.5"), grouped by provider with its quota, and
// the effort select that shows only for a model that takes effort. New thread (desktop) and Hand over (desktop modal
// and phone sheet) use them; the phone's New thread has the same choices behind its chip.
import type { Profile } from "./types.ts";
import { quotaSuffix, useQuotas } from "./quota-ui.tsx";
import { choiceValue, modelChoices, parseChoice } from "./model-choice.ts";

export function ModelSelect({ profiles, profile, model, onPick, label = "Model" }: { profiles: Profile[]; profile: string; model: string; onPick(profile: string, model: string): void; label?: string }) {
  const quotas = useQuotas();
  const choices = modelChoices(profiles, { profile, model });
  return (
    <select
      aria-label={label}
      value={choiceValue(profile, model)}
      onChange={(e) => {
        const c = parseChoice((e.target as HTMLSelectElement).value);
        onPick(c.profile, c.model);
      }}
    >
      {profiles.map((p) => {
        const q = quotaSuffix(quotas, p.id); // V0.44: what is left on the account, on its group
        return (
          <optgroup key={p.id} label={q ? `${p.name} · ${q}` : p.name}>
            {choices
              .filter((c) => c.profile === p.id)
              .map((c) => (
                <option key={c.value} value={c.value} disabled={!!c.why} title={c.why}>
                  {c.label}
                </option>
              ))}
          </optgroup>
        );
      })}
    </select>
  );
}

/** The effort levels of the picked model; nothing when it takes none. */
export function EffortSelect({ efforts, effort, onPick }: { efforts: string[]; effort: string; onPick(e: string): void }) {
  if (!efforts.length) return null;
  return (
    <select aria-label="Effort" value={effort} onChange={(e) => onPick((e.target as HTMLSelectElement).value)}>
      {!effort && <option value="">CLI default</option>}
      {efforts.map((x) => (
        <option value={x}>{x}</option>
      ))}
    </select>
  );
}
