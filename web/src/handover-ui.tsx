// SPEC 0.5 V0.45, decision 0013: the handover dialog. Pick the profile and model, see which rung the host will
// use, confirm. The same form sits in a desktop modal and in a phone sheet.
import { useHosts } from "./host-pick-ui.tsx";
import { hostOf, profilesOfHost } from "./hosts.ts";
import { useEffect, useState } from "preact/hooks";
import type { Profile, Thread } from "./types.ts";
import { handoverTargets, parsePlan, planGuess, rungText, type HandoverPlan } from "./handover.ts";
import { handoverPick, mappingLine, modelName } from "./model-choice.ts";
import { EffortSelect, ModelSelect } from "./model-choice-ui.tsx";
import { effortsFor } from "./providers.ts";
import { Modal } from "./dialogs.tsx";
import { hostPath } from "./urls.ts";

export interface HandoverProps {
  t: Thread;
  profiles: Profile[];
  onClose(): void;
  /** runs thread.handover; resolves to an error text, or null */
  onConfirm(profile: string, model: string, effort: string): Promise<string | null>;
}

async function fetchPlan(t: Thread, profile: string): Promise<HandoverPlan> {
  try {
    const r = await fetch(hostPath(t.hostId ?? "", `/threads/${encodeURIComponent(t.id)}/handover?profile=${encodeURIComponent(profile)}`), { headers: { Accept: "application/json" } });
    if (r.ok) return parsePlan(await r.json()) ?? planGuess(t);
  } catch {
    /* the mock client and old hosts have no plan */
  }
  return planGuess(t);
}

export function HandoverForm(p: HandoverProps & { phone?: boolean }) {
  // V0.139: a thread is handed over on its host, to that host's providers.
  const hs = useHosts();
  const targets = handoverTargets(p.t, profilesOfHost(hostOf(p.t, hs.hosts), hs.hosts, p.profiles));
  const fromProf = p.profiles.find((x) => x.id === p.t.profile);
  const [profile, setProfile] = useState(targets.find((x) => !x.unavailable)?.id ?? "");
  const prof = targets.find((x) => x.id === profile);
  // V0.model-choice (#23): the target starts on the nearest model and effort to the thread's, and the dialog names the mapping
  const [pick, setPick] = useState(() => handoverPick(p.t, fromProf, prof));
  const { model, effort } = pick;
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
  const from = fromProf?.name ?? p.t.profile;
  // V0.120 (5): the thread's model, marked off when the owner turned it off (it keeps running until handed over).
  const fromModel = p.t.model ? (fromProf && !fromProf.models.includes(p.t.model) ? `${modelName(fromProf, p.t.model)} (off)` : modelName(fromProf, p.t.model)) : "";
  const choose = (id: string, m: string) => {
    const to = targets.find((x) => x.id === id);
    if (id !== profile) {
      setProfile(id);
      const near = handoverPick(p.t, fromProf, to);
      // a model the owner picked keeps the nearest effort it takes
      return setPick(m === near.model ? near : handoverPick({ ...p.t, model: m }, fromProf, to));
    }
    setPick(handoverPick({ model: m, effort: effort || p.t.effort }, to, to));
  };
  const confirm = async () => {
    if (busy || blocked || !profile) return;
    setBusy(true);
    setErr(null);
    const e = await p.onConfirm(profile, model, effort);
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
      <div class={effortsFor(prof, model).length > 0 ? "row2" : "row1"}>
        <label class="fld">
          <span>To</span>
          <ModelSelect profiles={targets} profile={profile} model={model} onPick={choose} label="To" />
        </label>
        {effortsFor(prof, model).length > 0 && (
          <label class="fld">
            <span>Effort</span>
            <EffortSelect efforts={effortsFor(prof, model)} effort={effort} onPick={(x) => setPick({ model, effort: x })} />
          </label>
        )}
      </div>
      <div class="ho-map srcl">{mappingLine(p.t, fromProf, prof, pick)}</div>
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
