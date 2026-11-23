// A project's page (V0.55, V0.58, V0.116, V0.139): the editor shared by the desktop Settings and the phone's project screen.
import { type ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { HostInfo } from "./hosts.ts";
import type { Profile, Project, QuietHours } from "./types.ts";
import { quietList, quietNote, quietSaver } from "./quiet.ts";
import { LINE_ICONS } from "./lucide.ts";
import { FOLDER_LINE, NO_FOLDER_LINE, PALETTE_NAMES, iconFileError, nameError, patchFor, priorityLine, priorityOf, type ProjectPriority, agentSettleLine, agentSettleOf, type AgentSettle, toolApprovalLine, toolApprovalOf, type ToolApproval, setByAgent, setupLine, colorName, colorVar, monogram, projectDefaults, resolveIcon } from "./projects.ts";
import { tildePath } from "./paths.ts";
import { Group, Row, SaveField, Seg, SetContext, useSet } from "./settings-rows.tsx";
import { useFolderPick } from "./folder-picker.tsx";
import { HostChip, useHosts } from "./host-pick-ui.tsx";
import { orderedMembers, type HostTabItem } from "./host-tabs.ts";
import { HostTabs } from "./host-tabs-ui.tsx";
import { hostChip } from "./hosts.ts";
import { deleteProjectAsk } from "./confirm.ts";
import { useAsk } from "./confirm-ui.tsx";
import { effortsFor, offLabel, projectModelOff } from "./providers.ts";
import { modelName } from "./model-choice.ts";
import { useApi } from "./api-context.ts";
import { LinkRows, type ProjectLinking } from "./project-linking.tsx";
import { SetBy, ProjectIcon } from "./project-ui.tsx";
import { QuietGroup } from "./quiet-ui.tsx";

const ICON_GRID = Object.keys(LINE_ICONS);

export interface ProjectPatch {
  name?: string;
  color?: string;
  icon?: { kind: string; value: string };
  profile?: string;
  model?: string;
  /** V0.46: quiet hours, a list of windows (decision 0014 addendum); null turns them off */
  quiet?: QuietHours[] | null;
  /** V0.project-priority: null is normal */
  priority?: "high" | "low" | null;
  /** V0.settle-request: null is ask */
  agentSettle?: "auto" | null;
}

type IconTab = "image" | "monogram" | "emoji" | "line";

/** A project's page (V0.55, V0.58, V0.116; V0.119 amends the Save button away): Identity, Threads, Quiet hours, Delete.
 *  Each change saves at once with only its own field (V0.116 (2)); quiet hours 600 ms after the last change and at
 *  once when the page closes. The same page in the desktop Settings and on the phone's project screen. */
type EditorProps = { project: Project; profiles: Profile[]; /** the host's clock (V0.46) */ now: number; onSave(patch: ProjectPatch): Promise<string | null>; onSetFolder(path: string): Promise<string | null>; /** V0.58: after the one question; the error, or null */ onDelete?(): Promise<string | null> };

/** V0.139, V0.host-tabs: a project lives on one host, or on several when linked (decision 0034). Linked projects show
 *  HostTabs directly under the header to switch between host records in the owner's host order; unlinked projects on a
 *  multi-host front keep their read-only Host chip. While the shown host is offline the screen is read-only and says so.
 *  The same on the desktop (Settings → Projects → a project) and the phone (the project screen). */
export function ProjectEditor(p: EditorProps & { linking?: ProjectLinking }) {
  const hs = useHosts();
  const env = useSet();
  const chip = hostChip(p.project.hostId, hs.hosts, hs.now);
  const profiles = p.profiles; // V0.139: the caller hands its host's (store.ts profilesOn)
  const rawMembers = p.linking?.group.members ?? [];
  const linked = rawMembers.length > 1;
  const members = linked ? orderedMembers(rawMembers, hs.hosts ?? []) : [];
  // A member's tab is its host, with its folder on hover. A member without an id cannot be opened or named: it is left
  // out of the strip (V0.host-tabs: every other tab names a host a call can reach).
  const memberTabs: HostTabItem[] = members
    .filter((m) => m.id)
    .map((m) => {
      const mc = hostChip(m.hostId, hs.hosts, hs.now);
      const name = mc?.name ?? m.host ?? "";
      return { id: m.id!, name, label: name, offline: !!mc?.offline, title: m.folder ? `${name} · ${tildePath(m.folder)}` : name };
    });
  return (
    <SetContext.Provider value={{ ...env, readOnly: env.readOnly || !!chip?.offline }}>
      <ProjectEditorBody
        {...p}
        profiles={profiles}
        linked={linked}
        shown={linked ? p.linking!.group : undefined}
        // V0.linked-values-sync: an image goes to this host's record (else the first online member); the front copies it,
        // or its removal, to the other members, also to one that is offline now once it is back.
        imageTargets={imageTarget(linked ? rawMembers : [p.project], p.project, hs.hosts)}
        host={
          linked && p.linking ? (
            <>
              <HostTabs items={memberTabs} active={p.project.id!} onPick={(id) => id !== p.project.id && p.linking!.open(id)} label="Project hosts" />
              {chip?.offline && (
                <div class="note hnote">
                  {chip.name} is {chip.line.replace(" · read-only", "")}. This project shows as last read and cannot be changed until it is back.
                </div>
              )}
            </>
          ) : (
            chip && (
              <Group title="Host" label="Host">
                <Row
                  class="hostrow"
                  title={<HostChip chip={chip} />}
                  desc={chip.offline ? `${chip.name} is ${chip.line.replace(" · read-only", "")}. This project shows as last read and cannot be changed until it is back.` : "The project lives here: its folder, its threads and the providers below are this host's. A project does not move to another host."}
                />
                {p.linking && <LinkRows project={p.project} linking={p.linking} />}
              </Group>
            )
          )
        }
        hostManage={
          linked && p.linking ? (
            <Group title="Hosts" label="Hosts" fold={{ id: `project:${p.linking.group.id}:hosts`, open: false, summary: `${members.length} linked hosts` }}>
              <LinkRows project={p.project} linking={p.linking} tabbed />
            </Group>
          ) : undefined
        }
      />
    </SetContext.Provider>
  );
}

/** V0.linked-values-sync: the one record an upload or Remove image goes to: this one when its host is online, else the
 *  first online member; none while every host is offline. */
function imageTarget(members: Project[], own: Project, hosts: readonly HostInfo[] | undefined): Project[] {
  const on = (m: Project) => hosts?.find((h) => h.id === m.hostId)?.online !== false;
  const t = on(own) ? own : members.find(on);
  return t ? [t] : [];
}

function ProjectEditorBody({ project, profiles, now, onSave, onSetFolder, onDelete, host, hostManage, linked, shown, imageTargets }: EditorProps & { /** V0.139, V0.host-tabs: HostTabs (linked) or the Host group (single-host), under the header */ host?: ComponentChildren; /** V0.host-tabs: compact folded linking actions for a linked project */ hostManage?: ComponentChildren; /** decision 0034: other hosts have this project too */ linked?: boolean; /** decision 0034: the linked project, whose shared values this page shows */ shown?: Project; /** V0.linked-values-sync: the record an image goes to; the front copies it to the other members */ imageTargets: Project[] }) {
  // Decision 0034: name, colour, icon, quiet hours, priority, agentSettle and toolApproval are the link's; the rest of the page is this host's record.
  if (shown) project = { ...project, name: shown.name, color: shown.color, icon: shown.icon, quiet: shown.quiet, priority: shown.priority, agentSettle: shown.agentSettle, toolApproval: shown.toolApproval, image: shown.image, imageFrom: shown.imageFrom };
  const env = useSet();
  const api = useApi();
  const ask = useAsk();
  const picker = useFolderPick();
  const [errs, setErrs] = useState<Record<string, string | null>>({});
  const err = (k: string, e: string | null) => setErrs((x) => ({ ...x, [k]: e }));
  const save = async (k: string, patch: ProjectPatch): Promise<string | null> => {
    const e = await onSave(patch);
    err(k, e);
    if (!e) env.saved();
    return e;
  };
  // What the owner just picked shows at once; the host's answer replaces it (a refusal puts it back).
  const [color, setColor] = useState<string | null>(null);
  useEffect(() => setColor(null), [project.color]);
  const shownColor = color ?? colorName(project.color, project.name);
  const icon0 = resolveIcon(project.icon, project.name);
  const [tab, setTab] = useState<IconTab>(project.image ? "image" : icon0.kind);
  const [quiet, setQuiet] = useState<QuietHours[]>(quietList(project.quiet));
  // V0.project-priority: the pick shows at once; the host's answer replaces it.
  const [prio, setPrio] = useState<ProjectPriority | null>(null);
  useEffect(() => setPrio(null), [project.priority]);
  const shownPrio = prio ?? priorityOf(project);
  // V0.settle-request: the same, for what an agent's settle_request does.
  const [ags, setAgs] = useState<AgentSettle | null>(null);
  useEffect(() => setAgs(null), [project.agentSettle]);
  const shownAgs = ags ?? agentSettleOf(project);
  // V0.tool-approval-judge: the same, for agent tool approvals.
  const [tapp, setTapp] = useState<ToolApproval | null>(null);
  useEffect(() => setTapp(null), [project.toolApproval]);
  const shownTapp = tapp ?? toolApprovalOf(project);
  const saver = useRef(quietSaver<QuietHours[]>((q) => void save("quiet", patchFor("quiet", q))));
  useEffect(() => () => saver.current.flush(), []);
  const [busy, setBusy] = useState(false);
  const file = useRef<HTMLInputElement>(null);
  const prof = profiles.find((p) => p.id === (project.profile ?? ""));
  const defEfforts = prof ? effortsFor(prof, projectDefaults(project, profiles).model ?? "") : [];
  const preview: Project = { ...project, color: shownColor };

  const upload = async (f: File | undefined) => {
    if (!f || !project.id) return;
    const bad = iconFileError(f);
    if (bad) return err("icon", bad);
    setBusy(true);
    const rs = await Promise.all(imageTargets.map((m) => (m.hostId ? api.uploadProjectIcon(m.hostId, m.id!, f) : Promise.resolve({ ok: false, error: "This host reports no install id" }))));
    setBusy(false);
    const bad2 = rs.find((r) => !r.ok);
    err("icon", bad2 ? (bad2.error ?? "The upload did not work") : null);
    if (!bad2) env.saved();
  };
  const removeImage = async () => {
    const rs = await Promise.all(imageTargets.map((m) => (m.hostId ? api.command(m.hostId, "project.update", undefined, { id: m.id, image: "" }) : Promise.resolve({ ok: false, error: "This host reports no install id" }))));
    const bad2 = rs.find((r) => !r.ok);
    err("icon", bad2 ? (bad2.error ?? "That did not work") : null);
    if (!bad2) env.saved();
  };
  const setup = async () => {
    if (!project.hostId) return err("setup", "This host reports no install id");
    setBusy(true);
    const r = await api.command(project.hostId, "project.setupRun", undefined, { id: project.id });
    setBusy(false);
    err("setup", r.ok ? null : (r.error ?? "That did not work"));
  };
  const setup0 = setupLine(project);
  const n = project.threads ?? 0;
  const iconBy = setByAgent(project, "image") && !!project.image;
  return (
    <div class="pedit" style={{ "--pc": colorVar(shownColor, project.name) }}>
      <div class="phead">
        <ProjectIcon project={preview} name={project.name} size={44} />
        <div class="plt">
          <span class="pln big">{project.name}</span>
          <span class="pls2">
            {project.folder ? tildePath(project.folder) : "No folder"} · {n === 1 ? "1 thread" : `${n} threads`}
          </span>
        </div>
      </div>
      {host}
      <Group title="Identity" label="Identity">
        <Row
          title={<>Name{setByAgent(project, "name") && <SetBy />}</>}
          error={errs.name}
          stack
          wide
          class="pname"
          end={<SaveField value={project.name} label="Name" check={nameError} onError={(e) => err("name", e)} onSave={(v) => save("name", patchFor("name", v))} />}
        />
        <Row
          title={<>Colour{setByAgent(project, "color") && <SetBy />}</>}
          error={errs.color}
          stack
          end={
            <div class="swatches" role="radiogroup" aria-label="Colour">
              {PALETTE_NAMES.map((c) => (
                <button
                  type="button"
                  class={`sw${c === shownColor ? " on" : ""}`}
                  role="radio"
                  aria-checked={c === shownColor}
                  aria-label={c}
                  title={c}
                  style={{ "--pc": `var(--p-${c})` }}
                  disabled={env.readOnly}
                  onClick={async () => {
                    if (c === shownColor) return;
                    setColor(c);
                    if (await save("color", patchFor("color", c))) setColor(null);
                  }}
                />
              ))}
            </div>
          }
        />
        <div class={`sr col picon${errs.icon ? " bad" : ""}`}>
          <div class="sbar">
            <span class="sr-t">Icon{iconBy && <SetBy />}</span>
            <span class="end">
              <Seg
                label="Icon"
                value={tab}
                options={[
                  { value: "image", label: "Image" },
                  { value: "monogram", label: "Letters" },
                  { value: "emoji", label: "Emoji" },
                  { value: "line", label: "Symbol" },
                ]}
                onChange={(t) => {
                  setTab(t);
                  // Letters has nothing more to pick: the tab is the pick.
                  if (t === "monogram" && (project.icon?.kind ?? "monogram") !== "monogram") void save("icon", patchFor("icon", { kind: "monogram", value: "" }));
                }}
              />
            </span>
          </div>
          {tab === "image" ? (
            <div class="pimg">
              {project.image && <ProjectIcon project={project} name={project.name} size={44} />}
              <button type="button" class="btn" disabled={busy || !project.id || env.readOnly} onClick={() => file.current?.click()}>
                {project.image ? "Replace…" : "Upload…"}
              </button>
              {project.image && (
                <button type="button" class="btn" disabled={busy || env.readOnly} onClick={() => void removeImage()}>
                  Remove
                </button>
              )}
              <span class="sr-d">PNG, JPEG or GIF, at most 2 MB.</span>
              <input ref={file} type="file" accept="image/png,image/jpeg,image/gif" hidden onChange={(e) => (void upload((e.target as HTMLInputElement).files?.[0]), ((e.target as HTMLInputElement).value = ""))} />
            </div>
          ) : (
            project.image && <span class="sr-d">Used when the image is removed or cannot load.</span>
          )}
          {tab === "monogram" && <span class="sr-d">{monogram(project.name)} · the first letters of the name.</span>}
          {tab === "emoji" && (
            <span class="pemoji">
              <SaveField value={project.icon?.kind === "emoji" ? (project.icon.value ?? "") : ""} label="Emoji" placeholder="🏡" onError={(e) => err("icon", e)} onSave={(v) => save("icon", patchFor("icon", { kind: "emoji", value: v.trim() }))} />
            </span>
          )}
          {tab === "line" && (
            <div class="igrid" role="radiogroup" aria-label="Symbol">
              {ICON_GRID.map((nm) => {
                const on = icon0.kind === "line" && icon0.value === nm;
                return (
                  <button type="button" class={`ig${on ? " on" : ""}`} role="radio" aria-checked={on} title={nm} aria-label={nm} disabled={env.readOnly} onClick={() => !on && void save("icon", patchFor("icon", { kind: "line", value: nm }))}>
                    <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" dangerouslySetInnerHTML={{ __html: LINE_ICONS[nm] }} />
                  </button>
                );
              })}
            </div>
          )}
          {errs.icon && (
            <span class="sr-d err" role="alert">
              {errs.icon}
            </span>
          )}
        </div>
        <Row
          title="Set up automatically"
          class="psetup"
          desc={
            !errs.setup && !setup0?.error ? (
              <>
                An agent finds the logo and the real name and fills in what you have not set.
                {!project.folder ? " Needs a folder." : setup0 ? ` ${setup0.text}` : ""}
              </>
            ) : undefined
          }
          error={errs.setup ?? (setup0?.error ? setup0.text : null)}
          end={
            <button type="button" class="btn" disabled={busy || !project.folder || env.readOnly} onClick={() => void setup()}>
              {project.setup ? "Run again" : "Run setup"}
            </button>
          }
        />
      </Group>
      <Group title="Threads" label="Threads">
        <Row
          title="Folder"
          error={errs.folder}
          desc={
            project.folder ? (
              <span class="pfolder">
                <span class="ell mono" title={project.folder}>
                  {tildePath(project.folder)}
                </span>{" "}
                · <span class="srcl">{FOLDER_LINE}</span>
              </span>
            ) : (
              `${NO_FOLDER_LINE}.`
            )
          }
          end={
            <button
              type="button"
              class="btn pfchg"
              disabled={env.readOnly}
              onClick={async () => {
                const path = await picker.pick({ title: `Folder for ${project.name}`, ...(project.hostId ? { host: project.hostId } : {}) }); // V0.132: on its host
                if (!path) return;
                const e = await onSetFolder(path);
                err("folder", e);
                if (!e) env.saved();
              }}
            >
              {project.folder ? "Change…" : "Choose…"}
            </button>
          }
        />
        <Row
          title="Default provider, model and effort"
          desc="What a new thread here starts with."
          error={errs.defaults ?? projectModelOff(project, profiles)}
          stack
          class="two"
          end={
            <>
              <select aria-label="Default provider" value={project.profile ?? ""} disabled={env.readOnly} onChange={(e) => void save("defaults", patchFor("profile", (e.target as HTMLSelectElement).value))}>
                <option value="">None</option>
                {profiles.map((p) => (
                  <option value={p.id}>{p.name}</option>
                ))}
              </select>
              <select aria-label="Default model" value={project.model ?? ""} disabled={!prof || env.readOnly} onChange={(e) => void save("defaults", patchFor("model", (e.target as HTMLSelectElement).value))}>
                <option value="">{prof ? "The provider's default" : "None"}</option>
                {prof && project.model && !prof.models.includes(project.model) && <option value={project.model}>{offLabel(project.model)}</option>}
                {(prof?.models ?? []).map((m) => (
                  <option value={m}>{modelName(prof, m)}</option>
                ))}
              </select>
              {/* V0.model-choice (#23): the default effort, only for a model that takes one */}
              {defEfforts.length > 0 && (
                <select aria-label="Default effort" value={project.effort ?? ""} disabled={env.readOnly} onChange={(e) => void save("defaults", patchFor("effort", (e.target as HTMLSelectElement).value))}>
                  <option value="">The provider's default</option>
                  {defEfforts.map((x) => (
                    <option value={x}>{x}</option>
                  ))}
                </select>
              )}
            </>
          }
        />
        <Row
          title="Priority"
          desc={priorityLine(shownPrio)}
          error={errs.priority}
          stack
          end={
            <Seg
              label="Priority"
              value={shownPrio}
              disabled={env.readOnly}
              options={[
                { value: "low", label: "Low" },
                { value: "normal", label: "Normal" },
                { value: "high", label: "High" },
              ]}
              onChange={async (v) => {
                setPrio(v);
                if (await save("priority", patchFor("priority", v))) setPrio(null);
              }}
            />
          }
        />
        <Row
          title="Agent says done"
          desc={agentSettleLine(shownAgs)}
          error={errs.agentSettle}
          stack
          end={
            <Seg
              label="Agent says done"
              value={shownAgs}
              disabled={env.readOnly}
              options={[
                { value: "ask", label: "Ask me" },
                { value: "auto", label: "Settle" },
              ]}
              onChange={async (v) => {
                setAgs(v);
                if (await save("agentSettle", patchFor("agentSettle", v))) setAgs(null);
              }}
            />
          }
        />
        <Row
          title="Tool approvals"
          desc={toolApprovalLine(shownTapp)}
          error={errs.toolApproval}
          stack
          end={
            <Seg
              label="Tool approvals"
              value={shownTapp}
              disabled={env.readOnly}
              options={[
                { value: "ask", label: "Ask me" },
                { value: "auto", label: "Auto-judge" },
              ]}
              onChange={async (v) => {
                setTapp(v);
                if (await save("toolApproval", patchFor("toolApproval", v))) setTapp(null);
              }}
            />
          }
        />
      </Group>
      <QuietGroup
        value={quiet}
        note={quietNote(project, now)}
        onChange={(q) => {
          setQuiet(q);
          saver.current.push(q);
        }}
      />
      {errs.quiet && (
        <div class="seterr" role="alert">
          {errs.quiet}
        </div>
      )}
      {hostManage}
      {onDelete && (
        <Group danger class="pdel">
          <Row
            title={linked ? `Delete from ${project.host}` : "Delete project"}
            desc={`${n ? `Its ${n === 1 ? "thread stays" : `${n} threads stay`}, without a project.` : "It has no threads."}${linked ? " The other hosts keep theirs." : ""}`}
            error={errs.delete}
            end={
              <button
                type="button"
                class="btn danger"
                disabled={env.readOnly}
                onClick={async () => {
                  if (!(await ask(deleteProjectAsk(project)))) return;
                  err("delete", await onDelete());
                }}
              >
                Delete…
              </button>
            }
          />
        </Group>
      )}
    </div>
  );
}
