// The Playbook group of a project's page (V0.project-playbook), the same on the desktop (Settings → Projects → a
// project) and the phone (the project screen): what agents learned about the project, each entry to correct or remove,
// and the owner's yes for a stage's probe.
import { useState } from "preact/hooks";
import type { PlaybookEntry, Project } from "./types.ts";
import { entryKey, entryTitle, learnArgs, playbookEntries, playbookSummary, probeLine, probesWaiting } from "./playbook.ts";
import { Group, Row, SaveField, Switch, useSet } from "./settings-rows.tsx";
import { useApi } from "./api-context.ts";

/** `project` is the page's (a linked project shows the playbook written last); `target` is the one record a change
 *  goes to: this host's, else the first online member. The front copies it to the other members. */
export function PlaybookGroup({ project, target }: { project: Project; target?: Project }) {
  const env = useSet();
  const api = useApi();
  const [errs, setErrs] = useState<Record<string, string | null>>({});
  const [busy, setBusy] = useState(false);
  const [run, setRun] = useState<string | null>(null);
  const entries = playbookEntries(project);
  const off = env.readOnly || !target?.hostId;
  const send = async (key: string, action: string, args: Record<string, unknown>): Promise<string | null> => {
    if (!target?.hostId) return "No host of this project is online";
    const r = await api.command(target.hostId, action, undefined, args);
    const e = r.ok ? null : (r.error ?? "That did not work");
    setErrs((x) => ({ ...x, [key]: e }));
    if (!e) env.saved();
    return e;
  };
  const learn = (e: PlaybookEntry, patch: Parameters<typeof learnArgs>[2]) => send(entryKey(e), "project.learn", learnArgs(target?.id ?? "", e, patch));
  const ref = (e: PlaybookEntry) => ({ id: target?.id, slot: e.slot, ...(e.name ? { name: e.name } : {}) });
  const start = async () => {
    setBusy(true);
    const e = await send("run", "project.playbookRun", { id: target?.id });
    setBusy(false);
    setRun(e ? null : "Started: the thread is in the inbox, and what it learns shows here.");
  };
  const waits = probesWaiting(project);
  return (
    <Group
      title="Playbook"
      label="Playbook"
      class="pbook"
      fold={{ id: `project:${project.id}:playbook`, open: false, summary: playbookSummary(project), force: waits > 0, bad: waits > 0 }}
      foot="What agents here are told about the project, and what Fyr checks itself after a merge."
      how="Agents write an entry when you tell them how the project works or when they had to work it out. An entry you change is yours, and an agent cannot change it again. A stage's probe is a shell command Fyr runs in the project's folder after each merge, with {sha} as the merge commit: when it exits 0 the change is on that stage. Fyr runs a probe only after you allowed it, and a changed probe waits again."
    >
      {entries.map((e) => {
        const key = entryKey(e);
        const stage = e.slot === "stage";
        return (
          <div key={key} class={`sr col pbe${errs[key] ? " bad" : ""}`} data-entry={key}>
            <div class="sbar">
              <span class="sr-t">
                {entryTitle(e)}
                {e.by === "agent" && (
                  <em class="setby" title="Written by an agent. Change it and it is yours.">
                    Learned by agent
                  </em>
                )}
              </span>
              <span class="end">
                <button type="button" class="btn sm" disabled={off} aria-label={`Remove ${entryTitle(e)}`} onClick={() => void send(key, "project.learn", { ...ref(e), remove: true })}>
                  Remove
                </button>
              </span>
            </div>
            <SaveField value={e.how ?? ""} label={`${entryTitle(e)}: how`} placeholder={stage ? "How a merged change gets here" : "In a sentence or two"} onError={(x) => setErrs((v) => ({ ...v, [key]: x }))} onSave={(v) => learn(e, { how: v.trim() })} />
            {stage && (
              <>
                <SaveField value={e.url ?? ""} label={`${entryTitle(e)}: address`} placeholder="https://… where it is looked at" onError={(x) => setErrs((v) => ({ ...v, [key]: x }))} onSave={(v) => learn(e, { url: v.trim() })} />
                <SaveField
                  mono
                  value={e.probe ?? ""}
                  label={`${entryTitle(e)}: probe`}
                  placeholder="A command with {sha}; exit 0: the stage holds it"
                  onError={(x) => setErrs((v) => ({ ...v, [key]: x }))}
                  // The probe you write is yours: it is allowed with the same press.
                  onSave={async (v) => (await learn(e, { probe: v.trim() })) ?? (v.trim() ? send(key, "project.learn", { ...ref(e), allow: true }) : null)}
                />
                <label class="pbrun">
                  <span class={`sr-d${e.probe && !e.probeOk ? " warn" : ""}`}>{probeLine(e)}</span>
                  {e.probe && <Switch on={!!e.probeOk} label={`Run the probe of ${e.name}`} disabled={off} onChange={(on) => void send(key, "project.learn", { ...ref(e), allow: on })} />}
                </label>
              </>
            )}
            {e.evidence && <span class="sr-d">From: {e.evidence}</span>}
            {errs[key] && (
              <span class="sr-d err" role="alert">
                {errs[key]}
              </span>
            )}
          </div>
        );
      })}
      <Row
        title="Learn from the repository"
        class="pblearn"
        desc={run ?? (entries.length ? "An agent reads the workflows and deploy scripts again and records what it can back with evidence. Your entries are kept." : "Nothing learned yet. An agent reads the workflows and deploy scripts and records what it can back with evidence.")}
        error={errs.run}
        end={
          <button type="button" class="btn" disabled={busy || off || !project.folder} title={project.folder ? undefined : "Needs a folder"} onClick={() => void start()}>
            Run
          </button>
        }
      />
    </Group>
  );
}
