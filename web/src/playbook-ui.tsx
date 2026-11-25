// The Playbook group of a project's page (V0.project-playbook, V0.playbook-moments), the same on the desktop (Settings →
// Projects → a project) and the phone (the project screen): what agents learned about the project, each entry to
// correct or remove, an agent's suggestion to take or dismiss, the owner's yes for a stage's probe, and Add.
import { useEffect, useRef, useState } from "preact/hooks";
import type { PlaybookEntry, Project } from "./types.ts";
import { addableSlots, broken, doubted, entryKey, entryTitle, isLiveStage, learnArgs, lessonNameError, pendingLesson, playbookEntries, playbookSummary, playbookWaiting, probeLine, removeEntryAsk, slotTitle, stageHealth, stageNameError, stagesOf, suggestionLines } from "./playbook.ts";
import { Group, Row, Switch, useSet } from "./settings-rows.tsx";
import { useApi } from "./api-context.ts";
import { useAsk } from "./confirm-ui.tsx";
import { whenLabel } from "./format.ts";

/** A text that may run over several lines (a sentence or two, a command) and is read whole on a phone: as tall as its
 *  text. Save shows while it differs; Ctrl or ⌘ Enter saves, Esc puts the text back. A one-line field cut these off. */
function SaveArea({ value, label, name, placeholder, mono, onSave }: { value: string; label: string; /** V0.playbook-probe-health: the field's name over it, so a filled field still says what it is */ name?: string; placeholder?: string; mono?: boolean; onSave(v: string): Promise<string | null> }) {
  const env = useSet();
  const [text, setText] = useState(value);
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => setText(value), [value]);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight + 2}px`;
  }, [text]);
  const differs = text.trim() !== value.trim();
  const save = async () => {
    if (!differs || busy) return;
    setBusy(true);
    await onSave(text.trim());
    setBusy(false);
  };
  const area = (
    <span class="pbarea">
      <textarea
        ref={ref}
        rows={1}
        class={mono ? "mono" : undefined}
        value={text}
        placeholder={placeholder}
        aria-label={label}
        disabled={env.readOnly}
        spellcheck={!mono}
        onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) (e.preventDefault(), void save());
          if (e.key === "Escape" && differs) (e.stopPropagation(), setText(value));
        }}
      />
      {differs && (
        <button type="button" class="btn sm p" disabled={busy} onClick={() => void save()}>
          Save
        </button>
      )}
    </span>
  );
  return name ? (
    <label class="pbf">
      <span class="pbl">{name}</span>
      {area}
    </label>
  ) : (
    area
  );
}

/** `project` is the page's (a linked project shows the playbook written last); `target` is the one record a change
 *  goes to: this host's, else the first online member. The front copies it to the other members. */
export function PlaybookGroup({ project, target }: { project: Project; target?: Project }) {
  const env = useSet();
  const api = useApi();
  const ask = useAsk();
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
  const ref = (e: Pick<PlaybookEntry, "slot" | "name">) => ({ id: target?.id, slot: e.slot, ...(e.name ? { name: e.name } : {}) });
  const start = async () => {
    setBusy(true);
    const e = await send("run", "project.playbookRun", { id: target?.id });
    setBusy(false);
    setRun(e ? null : "Started: the thread is in the inbox, and what it learns shows here.");
  };
  const waits = playbookWaiting(project);
  const stages = stagesOf(project);
  const root = useRef<HTMLDivElement>(null);
  const show = (key: string) => root.current?.closest("section")?.querySelector(`[data-entry="${key}"]`)?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  const now = Date.now();
  return (
    <Group
      title="Playbook"
      label="Playbook"
      class="pbook"
      fold={{ id: `project:${project.id}:playbook`, open: false, summary: playbookSummary(project), force: waits > 0, bad: waits > 0 }}
      foot="What agents here are told about the project, and what Fyr does itself after a merge."
      how="Agents write an entry when you tell them how the project works or when they had to work it out. An entry you change is yours: an agent's later finding shows beside it as a suggestion. A stage's probe is a shell command Fyr runs in the project's folder after each merge, with {sha} as the merge commit and {repo} as the pull request's repository: when it exits 0 the change is on that stage. It answers at once and does not wait for the deploy: Fyr runs it again each minute and stops it after 30 seconds. Fyr runs a probe only after you allowed it, and a changed probe waits again. A stage you check gets a question card when a change reaches it. A stage with a deploy command gets a Run card when the stage before it holds the change, after your Looks good if you check that one; Fyr never runs it itself."
    >
      {stages.length > 0 && (
        // V0.playbook-probe-health: the path of a merged change at a glance, and how Fyr follows each stage; a stage
        // goes to its entry below. The same on the desktop and the phone.
        <div class="sr col pbpipe" ref={root}>
          <ol aria-label="After a merge">
            <li class="pbp from">Merge</li>
            {stages.map((e) => {
              const h = stageHealth(e);
              return (
                <li key={e.name}>
                  <button type="button" class={`pbp ${h.state}`} onClick={() => show(entryKey(e))}>
                    <span class="pbp-n">
                      {e.name}
                      {isLiveStage(e) && <em>live</em>}
                    </span>
                    <span class="pbp-w">{h.word}</span>
                  </button>
                </li>
              );
            })}
          </ol>
        </div>
      )}
      {entries.map((e) => {
        const key = entryKey(e);
        const stage = e.slot === "stage";
        const title = entryTitle(e);
        return (
          <div key={key} class={`sr col pbe${errs[key] ? " bad" : ""}`} data-entry={key}>
            <div class="sbar">
              <span class="sr-t">
                {title}
                {e.by === "agent" && (
                  <em class="setby" title="Written by an agent. Change it and it is yours.">
                    Learned by agent
                  </em>
                )}
              </span>
              <span class="end">
                {/* V0.playbook-probe-health: a quiet control that asks first: one press no longer drops an entry's text and commands */}
                <button type="button" class="btn sm pbrm" disabled={off} aria-label={`Remove ${title}`} onClick={async () => void ((await ask(removeEntryAsk(e))) && send(key, "project.learn", { ...ref(e), remove: true }))}>
                  Remove
                </button>
              </span>
            </div>
            {/* V0.playbook-lessons: a lesson an agent only suggested has no text of its own yet: its suggestion below is all there is */}
            {!pendingLesson(e) && <SaveArea value={e.how ?? ""} label={`${title}: how`} name={stage ? "How a change gets here" : undefined} placeholder={stage ? "Automatic on merge, a release tag, a command" : "In a sentence or two"} onSave={(v) => learn(e, { how: v })} />}
            {stage && (
              <>
                <SaveArea value={e.url ?? ""} label={`${title}: address`} name="Address" placeholder="https://… where it is looked at" onSave={(v) => learn(e, { url: v })} />
                <SaveArea
                  mono
                  value={e.probe ?? ""}
                  label={`${title}: probe`}
                  name="Probe"
                  placeholder="A command that exits 0 when {sha} is here"
                  // The probe you write is yours: it is allowed with the same press.
                  onSave={async (v) => (await learn(e, { probe: v })) ?? (v ? send(key, "project.learn", { ...ref(e), allow: true }) : null)}
                />
                <label class="pbrun">
                  <span class={`sr-d pbstate ${stageHealth(e).state}${(e.probe && !e.probeOk) || doubted(e) || broken(e) ? " warn" : ""}`}>
                    {probeLine(e, e.probeOk ? whenLabel(e.last?.at, now) : "")}
                    {/* what the probe printed last, when it did not see the commit: the reason is often in it */}
                    {e.probeOk && e.last?.note && e.last.state !== "reached" && <code class="pbnote">{e.last.note}</code>}
                  </span>
                  {e.probe && <Switch on={!!e.probeOk} label={`Run the probe of ${e.name}`} disabled={off} onChange={(on) => void send(key, "project.learn", { ...ref(e), allow: on })} />}
                </label>
                <SaveArea mono value={e.deployCmd ?? ""} label={`${title}: deploy command`} name="Deploy by hand" placeholder="A command that puts {sha} here; you run it from a card" onSave={(v) => learn(e, { deployCmd: v })} />
                <label class="pbrun">
                  <span class="sr-d">{e.ownerChecks ? "Fyr asks you to look when a change gets here." : "Ask me to look when a change gets here."}</span>
                  <Switch on={!!e.ownerChecks} label={`Ask me to check each change on ${e.name}`} disabled={off} onChange={(on) => void learn(e, { ownerChecks: on })} />
                </label>
              </>
            )}
            {e.evidence && <span class="sr-d">From: {e.evidence}</span>}
            {e.suggest && (
              <div class="pbsug" role="note">
                <span class="sr-t">{pendingLesson(e) ? "An agent suggests this lesson. Agents here are told it once you take it." : "An agent suggests"}</span>
                {suggestionLines(e).map((l) => (
                  <span key={l} class="sr-d pbsugl">
                    {l}
                  </span>
                ))}
                {e.suggest.evidence && <span class="sr-d">From: {e.suggest.evidence}</span>}
                <span class="pbsugb">
                  <button type="button" class="btn sm p" disabled={off} onClick={() => void send(key, "project.learn", { ...ref(e), take: true })}>
                    Take it
                  </button>
                  <button type="button" class="btn sm" disabled={off} onClick={() => void send(key, "project.learn", { ...ref(e), dismiss: true })}>
                    Dismiss
                  </button>
                </span>
              </div>
            )}
            {errs[key] && (
              <span class="sr-d err" role="alert">
                {errs[key]}
              </span>
            )}
          </div>
        );
      })}
      <AddEntry project={project} off={off} error={errs.add} onAdd={(e) => send("add", "project.learn", learnArgs(target?.id ?? "", e))} />
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

/** V0.playbook-moments: an entry written by hand: the slot, a stage's name, and the text. It is the owner's. */
function AddEntry({ project, off, error, onAdd }: { project: Project; off: boolean; error?: string | null; onAdd(e: Pick<PlaybookEntry, "slot" | "name" | "how">): Promise<string | null> }) {
  const slots = addableSlots(project);
  const [slot, setSlot] = useState<PlaybookEntry["slot"] | "">("");
  const [name, setName] = useState("");
  const [how, setHow] = useState("");
  const [bad, setBad] = useState<string | null>(null);
  if (!slots.length) return null;
  const add = async () => {
    if (!slot) return;
    const e = slot === "stage" ? stageNameError(name) : slot === "lesson" ? lessonNameError(name) : null;
    if (e || !how.trim()) return setBad(e ?? "Say it in a sentence or two");
    setBad(null);
    if (!(await onAdd({ slot, ...(slot === "stage" || slot === "lesson" ? { name: name.trim().toLowerCase() } : {}), how: how.trim() }))) (setSlot(""), setName(""), setHow(""));
  };
  return (
    <div class={`sr col pbadd${bad || error ? " bad" : ""}`}>
      <div class="sbar">
        <span class="sr-t">Add an entry</span>
        <span class="end">
          <select aria-label="Add an entry" value={slot} disabled={off} onChange={(e) => (setSlot((e.target as HTMLSelectElement).value as PlaybookEntry["slot"] | ""), setBad(null))}>
            <option value="">Choose…</option>
            {slots.map((s) => (
              <option value={s}>{slotTitle(s)}</option>
            ))}
          </select>
        </span>
      </div>
      {slot && (
        <>
          {slot === "stage" && <input type="text" value={name} placeholder="The environment: dev, staging, prod" aria-label="Stage name" autocapitalize="off" onInput={(e) => setName((e.target as HTMLInputElement).value)} />}
          {slot === "lesson" && <input type="text" value={name} placeholder="A short name: e2e-before-done" aria-label="Lesson name" autocapitalize="off" onInput={(e) => setName((e.target as HTMLInputElement).value)} />}
          <span class="pbarea">
            <textarea rows={2} value={how} placeholder={slot === "stage" ? "How a merged change gets here" : slot === "lesson" ? "What agents here should do, in a sentence" : "In a sentence or two"} aria-label="How" onInput={(e) => setHow((e.target as HTMLTextAreaElement).value)} />
            <button type="button" class="btn sm p" disabled={off} onClick={() => void add()}>
              Add
            </button>
          </span>
        </>
      )}
      {(bad || error) && (
        <span class="sr-d err" role="alert">
          {bad ?? error}
        </span>
      )}
    </div>
  );
}
