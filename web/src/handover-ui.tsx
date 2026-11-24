// SPEC 0.5 V0.45, decision 0013; V0.trans-host-handover, decision 0039: the handover dialog.
// Pick host (when several exist), provider, model, effort, and (across hosts) target folder & worktree option;
// see git limitations and which rung runs; confirm. Shared between desktop modal and phone sheet.
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import type { Profile, Project, Thread } from "./types.ts";
import {
  effectivePlan,
  gitLimitationNote,
  handoverTargetsForHost,
  planGuess,
  rungText,
  suggestTargetFolder,
  type CrossHostHandoverArgs,
  type HandoverPlan,
} from "./handover.ts";
import { handoverPick, mappingLine, modelName } from "./model-choice.ts";
import { EffortSelect, ModelSelect } from "./model-choice-ui.tsx";
import { effortsFor } from "./providers.ts";
import { Modal } from "./dialogs.tsx";
import { useApi } from "./api-context.ts";
import { HostField, ProfilesContext, useHosts } from "./host-pick-ui.tsx";
import { hostChoices, hostOfflineNote, profilesOf } from "./hosts.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { useDirectory } from "./project-ui.tsx";
import { SCRATCH_LABEL } from "./projects.ts";
import { tildePath } from "./paths.ts";
import { ref } from "./hostfeed.ts";
import type { HostId } from "./hostid.ts";

interface HandoverProps {
  t: Thread;
  /** the profiles of the thread's host (fallback when ProfilesContext is empty in isolated tests) */
  profiles: Profile[];
  /** all known projects (for linked-project / matching-repo target folder suggestions across hosts) */
  projects?: Project[];
  onClose(): void;
  /** runs thread.handover on the same host; resolves to an error text, or null */
  onConfirm(profile: string, model: string, effort: string): Promise<string | null>;
  /** runs front.threadHandover across hosts (decision 0039); resolves to an error text, or null */
  onConfirmHost?(args: CrossHostHandoverArgs): Promise<string | null>;
}

async function fetchPlan(api: ReturnType<typeof useApi>, t: Thread, profile: string): Promise<HandoverPlan> {
  // The mock client and old hosts have no plan: a guess stands in.
  const r = await api.handoverPlan?.(t.hostId, t.id, profile);
  return r?.ok ? r.data : planGuess(t);
}

export function HandoverForm(p: HandoverProps & { phone?: boolean }) {
  const api = useApi();
  const { hosts, now } = useHosts();
  const allProfiles = useContext(ProfilesContext);
  const folderPicker = useFolderPick();
  const hostOpts = hostChoices(hosts);
  const canPickHost = !!p.onConfirmHost && hostOpts.length > 1;

  const [targetHost, setTargetHost] = useState<HostId>(p.t.hostId);
  const crossHost = targetHost !== p.t.hostId;
  const hostProfiles = crossHost ? profilesOf({ hostId: targetHost }, allProfiles) : p.profiles;
  const targets = handoverTargetsForHost(p.t, targetHost, hostProfiles);
  const fromProf = p.profiles.find((x) => x.id === p.t.profile);

  const [profile, setProfile] = useState(() => targets.find((x) => !x.unavailable)?.id ?? "");
  const prof = targets.find((x) => x.id === profile);
  // V0.model-choice (#23): the target starts on the nearest model and effort to the thread's, and the dialog names the mapping
  const [pick, setPick] = useState(() => handoverPick(p.t, fromProf, prof));
  const { model, effort } = pick;

  const [plan, setPlan] = useState<HandoverPlan | null>(null);
  const [folder, setFolder] = useState("");
  const [projectId, setProjectId] = useState("");
  const [folderReason, setFolderReason] = useState("");
  const [folderManual, setFolderManual] = useState(false);
  const [useWorktree, setUseWorktree] = useState(true);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const root = useRef<HTMLDivElement>(null);
  /** a select's list may be open (see onKey) */
  const listOpen = useRef(false);
  // V0.dialogs-review (10): on the desktop the focus starts on the target ("To"), the choice this dialog is for, not
  // on the header's ×. Not on the phone: its sheet takes the focus itself (motion-ui.tsx), with no field focused.
  /* eslint-disable react-hooks/exhaustive-deps -- the first focus, once, when the form opens */
  useEffect(() => {
    if (!p.phone) root.current?.querySelector<HTMLElement>('select[aria-label="To"]')?.focus();
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */

  // When target host changes, pick the best default provider/model on that host and suggest its folder.
  /* eslint-disable react-hooks/exhaustive-deps -- resets the targets only when the target host changes; the thread, the profiles and fromProf are read at that moment */
  useEffect(() => {
    const nextTargets = handoverTargetsForHost(p.t, targetHost, targetHost === p.t.hostId ? p.profiles : profilesOf({ hostId: targetHost }, allProfiles));
    const preferred = targetHost !== p.t.hostId ? nextTargets.find((x) => x.id === p.t.profile && !x.unavailable) : undefined;
    const def = preferred ?? nextTargets.find((x) => !x.unavailable);
    setProfile(def?.id ?? "");
    setPick(handoverPick(p.t, fromProf, def));
    setFolderManual(false);
    setErr(null);
  }, [targetHost]);
  /* eslint-enable react-hooks/exhaustive-deps */

  // Fetch the source thread's handover plan (for same-host, passes target profile; for cross-host, reads source session + git facts).
  /* eslint-disable react-hooks/exhaustive-deps -- fetches the plan again only when the thread, the cross-host choice or (on the same host) the profile changes; api is fixed for the app's life */
  useEffect(() => {
    let live = true;
    setPlan(null);
    const qProf = crossHost ? "" : profile;
    if (crossHost || profile) {
      void fetchPlan(api, p.t, qProf).then((x) => live && setPlan(x));
    }
    return () => {
      live = false;
    };
  }, [p.t.id, crossHost, crossHost ? "" : profile]);
  /* eslint-enable react-hooks/exhaustive-deps */

  // Auto-suggest target folder from linked project or matching git remote once plan (with git.repo) loads, unless the owner picked one.
  /* eslint-disable react-hooks/exhaustive-deps -- the folder suggestion follows these inputs; the thread and the project list are read at that moment */
  useEffect(() => {
    if (!crossHost || folderManual) return;
    const s = suggestTargetFolder(p.t, targetHost, p.projects ?? [], plan?.git?.repo);
    setFolder(s.cwd);
    setProjectId(s.projectId);
    setFolderReason(s.reason);
  }, [crossHost, targetHost, plan?.git?.repo, folderManual]);
  /* eslint-enable react-hooks/exhaustive-deps */

  const blocked = p.t.unavailable["thread.handover"];
  const hostDown = crossHost ? hostOfflineNote(targetHost, hosts, now) : null;
  const from = fromProf?.name ?? p.t.profile;
  const fromHostName = hosts.find((h) => h.id === p.t.hostId)?.name || p.t.host || "";
  const toHostName = hosts.find((h) => h.id === targetHost)?.name || "";
  // V0.120 (5): the thread's model, marked off when the owner turned it off (it keeps running until handed over).
  const fromModel = p.t.model ? (fromProf && !fromProf.models.includes(p.t.model) ? `${modelName(fromProf, p.t.model)} (off)` : modelName(fromProf, p.t.model)) : "";
  const shownPlan = plan ? effectivePlan(plan, p.t.hostId, targetHost, prof) : null;
  const hasFeatureBranch = !!(plan?.git?.branch && plan.git.branch !== plan.git.default);

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

  const pickFolder = async () => {
    const picked = await folderPicker.pick({ host: targetHost, title: toHostName ? `Folder on ${toHostName}` : "Folder for this thread" });
    if (picked) {
      setFolder(picked);
      setProjectId("");
      setFolderReason("Picked on " + (toHostName || "target host"));
      setFolderManual(true);
    }
  };

  const useScratch = () => {
    setFolder("");
    setProjectId("");
    setFolderReason(`${SCRATCH_LABEL} on ${toHostName || "the target host"}.`);
    setFolderManual(true);
  };

  const confirm = async () => {
    if (busy || blocked || hostDown || !profile) return;
    setBusy(true);
    setErr(null);
    const e = crossHost
      ? await p.onConfirmHost!({
          toHost: targetHost,
          profile,
          model,
          effort,
          ...(folder ? { cwd: folder } : {}),
          ...(projectId ? { projectId } : {}),
          ...(hasFeatureBranch && folder && useWorktree ? { useWorktree: true } : {}),
        })
      : await p.onConfirm(profile, model, effort);
    setBusy(false);
    if (e) setErr(e);
    else p.onClose();
  };

  if (!canPickHost && !targets.length) return <div class="db ho-b">There is no other provider to hand this thread over to.</div>;

  // V0.dialogs-review (16): Enter confirms, as in the confirmation and New thread, unless a control that uses Enter
  // itself has the focus. Desktop only: the phone has no keyboard, so its buttons carry no key hints either.
  const onKey = (e: KeyboardEvent) => {
    if (p.phone || e.isComposing || e.defaultPrevented) return;
    const el = e.target as HTMLElement | null;
    // A select whose list the owner opened (Space, F4, Alt+arrow, a click) uses Enter to take a choice, and some
    // browsers report that key to the page: that Enter must never hand the thread over.
    if (el?.tagName === "SELECT" && (e.key === " " || e.key === "F4" || (e.altKey && (e.key === "ArrowDown" || e.key === "ArrowUp")))) listOpen.current = true;
    if (e.key !== "Enter") return;
    if (el && ["BUTTON", "A", "TEXTAREA"].includes(el.tagName)) return;
    if (listOpen.current) return void (listOpen.current = false);
    e.preventDefault();
    void confirm();
  };
  const listShut = () => (listOpen.current = false);
  // Across hosts the button says what happens to this thread; the sentence above the form says it too.
  const label = crossHost && toHostName ? `Move to ${toHostName} and settle here` : "Hand over";

  return (
    <div class="ho-form" ref={root} onKeyDown={onKey} onMouseDown={(e) => (listOpen.current = (e.target as HTMLElement | null)?.tagName === "SELECT")} onChange={listShut} onFocusOut={listShut}>
      <div class={`db ho-b${p.phone ? " ho-ph" : ""}`}>
        <div class="ho-from">
          From <b>{from}</b>
          {fromModel && <span class="ho-model"> · {fromModel}</span>}
          {fromHostName && canPickHost && <span class="ho-model"> on {fromHostName}</span>}.{" "}
          {crossHost ? (
            <>
              Creates a linked thread on {toHostName || "the target host"} with this conversation copied over, <span class="ho-settle">and settles this thread</span>.
            </>
          ) : (
            "The thread keeps its conversation, project and folder; later runs use the new provider."
          )}
        </div>
        {canPickHost && (
          <HostField
            value={targetHost}
            onPick={(h) => setTargetHost(h)}
            what={crossHost ? "The thread continues on this host with its providers and folders." : "Keep on this host to switch provider only, or pick another host to move it."}
          />
        )}
        {!targets.length ? (
          <div class="err prose">{crossHost ? `No providers are configured on ${toHostName || "that host"}.` : "There is no other provider on this host."}</div>
        ) : (
          <>
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
          </>
        )}
        {crossHost && !hostDown && (
          <>
            <div class="fld">
              <span>Folder on {toHostName || "target"}</span>
              <div class="ho-dir">
                <span class={folder ? "mono ell" : "ell"} title={folder || undefined}>
                  {folder ? tildePath(folder) : SCRATCH_LABEL}
                </span>
                <button type="button" class="btn sm" onClick={() => void pickFolder()}>
                  Change…
                </button>
                {folder && (
                  <button type="button" class="btn sm" onClick={useScratch}>
                    Scratch folder
                  </button>
                )}
              </div>
              {folderReason && <span class="srcl">{folderReason}</span>}
            </div>
            {hasFeatureBranch && folder && (
              <label class="ho-wt">
                <input type="checkbox" checked={useWorktree} onChange={(e) => setUseWorktree((e.target as HTMLInputElement).checked)} />
                <span>
                  Check out <span class="mono">{plan!.git!.branch}</span> in a worktree if present on origin
                </span>
              </label>
            )}
            <div class="ho-note" role="note">
              {gitLimitationNote(plan?.git, fromHostName, toHostName)}
            </div>
          </>
        )}
        {blocked && (
          <div class="err prose" role="alert">
            {blocked}
          </div>
        )}
        {err && (
          <div class="err prose" role="alert">
            {err}
          </div>
        )}
      </div>
      {/* V0.dialogs-review (16): how the context goes over stands between the form and the buttons, outside the part
          that scrolls, so it is in view when the button is pressed. */}
      <div class={`ho-plan${p.phone ? " ho-ph" : ""}`}>
        <div class="ho-rung" aria-live="polite">
          {shownPlan ? (
            <>
              <b>{rungText(shownPlan.rung)}</b>
              {shownPlan.note && <small>{shownPlan.note}</small>}
            </>
          ) : (
            <span class="ho-wait">Checking how the context goes over…</span>
          )}
        </div>
      </div>
      <div class={`df${p.phone ? " ho-ph" : ""}`}>
        <button class="btn" onClick={p.onClose}>
          Cancel{!p.phone && <kbd>Esc</kbd>}
        </button>
        <button class="btn p" disabled={busy || !!blocked || !!hostDown || !profile} onClick={() => void confirm()}>
          {busy ? "Handing over…" : label}
          {!p.phone && !busy && <kbd>⏎</kbd>}
        </button>
      </div>
    </div>
  );
}

export function HandoverDialog(p: HandoverProps) {
  return (
    <Modal title="Hand over" onClose={p.onClose} wide>
      <HandoverForm {...p} />
    </Modal>
  );
}

/** Decision 0039, V0.trans-host-handover: quiet banner on a handed-over thread linking to its counterpart on the other host. Shared by desktop and phone. */
export function HandoverLinkBar({ t, phone }: { t: Thread; phone?: boolean }) {
  const dir = useDirectory();
  const { hosts } = useHosts();
  const to = t.handedOverTo;
  const from = t.handedOverFrom;
  if (!to && !from) return null;
  const hostName = (id: string, fallback?: string) => hosts.find((h) => h.id === id)?.name || fallback || id;
  if (to) {
    const targetRef = ref(to.hostId, to.threadId);
    const name = hostName(to.hostId, to.hostName);
    return (
      <div class={`hobar${phone ? " m-hobar" : ""}`} role="note">
        <span class="ell">
          Handed over to <b class="mono">{name}</b>
        </span>
        <button type="button" class="btn sm" onClick={() => dir.openThread(targetRef)}>
          Open thread →
        </button>
      </div>
    );
  }
  const sourceRef = ref(from!.hostId, from!.threadId);
  const name = hostName(from!.hostId, from!.hostName);
  return (
    <div class={`hobar from${phone ? " m-hobar" : ""}`} role="note">
      <span class="ell">
        Handed over from <b class="mono">{name}</b>
      </span>
      <button type="button" class="btn sm" onClick={() => dir.openThread(sourceRef)}>
        View source
      </button>
    </div>
  );
}
