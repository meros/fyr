// SPEC 0.5 V0.45, decision 0013: the handover dialog. Pick the profile and model, see which rung the host will
// use, confirm. The same form sits in a desktop modal and in a phone sheet.
import { useEffect, useState } from "preact/hooks";
import type { Profile, Thread } from "./types.ts";
import { handoverTargets, parsePlan, planGuess, rungText, targetModel, type HandoverPlan } from "./handover.ts";
import { shortModel } from "./format.ts";
import { offLabel } from "./providers.ts";
import { Modal } from "./dialogs.tsx";

export interface HandoverProps {
  t: Thread;
  profiles: Profile[];
  onClose(): void;
  /** runs thread.handover; resolves to an error text, or null */
  onConfirm(profile: string, model: string): Promise<string | null>;
}

async function fetchPlan(t: Thread, profile: string): Promise<HandoverPlan> {
  try {
    const r = await fetch(`/api/threads/${encodeURIComponent(t.id)}/handover?profile=${encodeURIComponent(profile)}`, { headers: { Accept: "application/json" } });
    if (r.ok) return parsePlan(await r.json()) ?? planGuess(t);
  } catch {
    /* the mock client and old hosts have no plan */
  }
  return planGuess(t);
}

export function HandoverForm(p: HandoverProps & { phone?: boolean }) {
  const targets = handoverTargets(p.t, p.profiles);
  const [profile, setProfile] = useState(targets[0]?.id ?? "");
  const prof = targets.find((x) => x.id === profile);
  const [model, setModel] = useState(targetModel(prof));
  const [plan, setPlan] = useState<HandoverPlan | null>(null);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    setPlan(null);
    if (profile) void fetchPlan(p.t, profile).then((x) => live && setPlan(x));
    return () => {
      live = false;
    };
  }, [p.t.id, profile]);
  const blocked = p.t.unavailable["thread.handover"];
  const fromProf = p.profiles.find((x) => x.id === p.t.profile);
  const from = fromProf?.name ?? p.t.profile;
  // V0.120 (5): the thread's model, marked off when the owner turned it off (it keeps running until handed over).
  const fromModel = p.t.model ? (fromProf && !fromProf.models.includes(p.t.model) ? offLabel(p.t.model) : shortModel(p.t.model)) : "";
  const pick = (id: string) => {
    setProfile(id);
    setModel(targetModel(targets.find((x) => x.id === id)));
  };
  const confirm = async () => {
    if (busy || blocked || !profile) return;
    setBusy(true);
    setErr(null);
    const e = await p.onConfirm(profile, model);
    setBusy(false);
    if (e) setErr(e);
    else p.onClose();
  };
  if (!targets.length) return <div class="db ho-b">There is no other provider to hand this thread over to.</div>;
  return (
    <>
    <div class={`db ho-b${p.phone ? " ho-ph" : ""}`}>
      <div class="ho-from">
        From <b>{from}</b>
        {fromModel && <span class="ho-model"> · {fromModel}</span>}. The thread keeps its conversation, project and folder; later runs use the new provider.
      </div>
      <div class="row2">
        <label class="fld">
          <span>To</span>
          <select value={profile} onChange={(e) => pick((e.target as HTMLSelectElement).value)}>
            {targets.map((x) => (
              <option value={x.id}>{x.name}</option>
            ))}
          </select>
        </label>
        <label class="fld">
          <span>Model</span>
          <select value={model} onChange={(e) => setModel((e.target as HTMLSelectElement).value)} disabled={!prof?.models.length}>
            {!prof?.models.length && <option value="">The provider's default</option>}
            {(prof?.models ?? []).map((m) => (
              <option value={m}>{shortModel(m)}</option>
            ))}
          </select>
        </label>
      </div>
      <div class="ho-rung" aria-live="polite">
        {plan ? (
          <>
            <b>{rungText(plan.rung)}</b>
            {plan.note && <small>{plan.note}</small>}
          </>
        ) : (
          <span class="ho-wait">Checking how the context goes over…</span>
        )}
      </div>
      {blocked && <div class="err prose">{blocked}</div>}
      {err && <div class="err prose">{err}</div>}
    </div>
      <div class={`df${p.phone ? " ho-ph" : ""}`}>
        <button class="btn" onClick={p.onClose}>
          Cancel
        </button>
        <button class="btn p" disabled={busy || !!blocked || !profile} onClick={() => void confirm()}>
          Hand over
        </button>
      </div>
    </>
  );
}

export function HandoverDialog(p: HandoverProps) {
  return (
    <Modal title="Hand over" onClose={p.onClose}>
      <HandoverForm {...p} />
    </Modal>
  );
}
