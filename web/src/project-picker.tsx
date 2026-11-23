// The one project picker (V0.30) and the one-step New project (V0.30, V0.55, V0.132).
import { useEffect, useRef, useState } from "preact/hooks";
import type { Project, Thread } from "./types.ts";
import { FOLDER_LINE, NO_FOLDER_LINE, newProjectDraft, projectOf } from "./projects.ts";
import { tildePath } from "./paths.ts";
import { useFolderPick } from "./folder-picker.tsx";
import { HostField, useHosts } from "./host-pick-ui.tsx";
import { hostOfflineNote, multiHost, pickerStart } from "./hosts.ts";
import type { HostId } from "./hostid.ts";
import { ProjectList } from "./project-ui.tsx";

/** A project's one folder (V0.55): the short path and what it does with "Change…" through the host folder picker, or
 *  that threads are filed by hand with "Choose folder…". × leaves the folder out where onClear is given. */
function FolderField({ folder, title, host, onPick, onClear }: { folder: string; title: string; /** V0.132: whose folders */ host?: HostId; onPick(path: string): void; onClear?(): void }) {
  const picker = useFolderPick();
  const pick = async () => {
    const path = await picker.pick({ title, ...(host ? { host } : {}) });
    if (path) onPick(path);
  };
  return (
    <div class="fld">
      <span>Folder</span>
      {folder ? (
        <div class="src pfolder">
          <span class="srcb">
            <span class="ell" title={folder}>
              {tildePath(folder)}
            </span>
            <span class="srcl">{FOLDER_LINE}</span>
          </span>
          {onClear && (
            <button class="src-x" aria-label={`Leave out ${tildePath(folder)}`} title="Leave out" onClick={onClear}>
              ×
            </button>
          )}
        </div>
      ) : (
        <p class="lede">{NO_FOLDER_LINE}.</p>
      )}
      <button class="btn sm pfchg" onClick={() => void pick()}>
        {folder ? "Change…" : "Choose folder…"}
      </button>
    </div>
  );
}

/** New project, one step (V0.30, V0.55): a name (required) and the one folder, prefilled from the thread. The rest is
 *  on the project screen. V0.132: with several hosts, the host it lives on comes first: picked here (Settings), or set
 *  by the caller (`host`: the New thread form's host, or the threads' host when it is made from them); the folder is
 *  that host's, and a new host drops a folder picked on another. */
export function NewProjectStep({ initialName, initialFolder, fromThread, host: fixed, onCreate, onCancel, onDraft }: { initialName: string; initialFolder: string; fromThread: boolean; host?: HostId; onCreate(name: string, folder: string, host: HostId): Promise<string | null>; onCancel(): void; /** V0.63: the name and folder as they are typed */ onDraft?(name: string, folder: string): void }) {
  const hs = useHosts();
  // Slice 2b: the picker starts on the first host in the owner's order (shown in it), the only one with one host.
  const [own, setOwn] = useState<HostId | undefined>(() => pickerStart(hs.hosts));
  const host = fixed ?? own;
  const down = hostOfflineNote(host, hs.hosts, hs.now);
  const [name, setName] = useState(initialName);
  const [folder, setFolder] = useState(initialFolder);
  useEffect(() => onDraft?.(name, folder), [name, folder]);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    nameRef.current?.focus();
    nameRef.current?.select();
  }, []);
  const submit = async () => {
    if (busy) return;
    if (!name.trim()) {
      nameRef.current?.focus();
      return setErr("A project needs a name.");
    }
    if (down || !host) return setErr(down); // V0.132: never on another host instead
    setBusy(true);
    setErr(null);
    const e = await onCreate(name.trim(), folder, host);
    setBusy(false);
    if (e) setErr(e);
  };
  return (
    <div class="pform">
      <label class="fld">
        <span>Name</span>
        <input
          ref={nameRef}
          value={name}
          onInput={(e) => setName((e.target as HTMLInputElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.isComposing) (e.preventDefault(), void submit());
          }}
          placeholder="Garden"
          autocapitalize="sentences"
        />
      </label>
      {fixed === undefined ? (
        <HostField
          value={own}
          what="The project lives there: its folder, its threads and its providers are that host's."
          onPick={(h) => {
            if (h !== own) setFolder(""); // a folder of another host does not come along
            setOwn(h);
          }}
        />
      ) : (
        multiHost(hs.hosts) && fixed && <p class="lede">On {hs.hosts.find((h) => h.id === fixed)?.name ?? "a host this Fyr does not follow"}.</p>
      )}
      <FolderField folder={folder} title="Project folder" host={host} onPick={setFolder} onClear={() => setFolder("")} />
      <p class="lede">Icon, colour and defaults are on the project screen, later.</p>
      {err && <div class="err prose">{err}</div>}
      <div class="pformb">
        <button class="btn" onClick={onCancel}>
          Back
        </button>
        <button class="btn p" disabled={busy} onClick={() => void submit()}>
          {fromThread ? "Create and file this thread" : "Create project"}
        </button>
      </div>
    </div>
  );
}

/** The one project picker (V0.30): file a thread, unfile it, or make a project from it in one step. */
export function ProjectPicker({ projects, threads, onPick, onCreate, onEdit }: { projects: Project[]; threads: Thread[]; onPick(p: Project | null): void; onCreate(name: string, folder: string): Promise<string | null>; onEdit?(p: Project): void }) {
  const one = threads.length === 1 ? threads[0] : undefined;
  const base = one ? newProjectDraft(one) : { name: "", folder: "" };
  const [draft, setDraft] = useState<{ name: string; folder: string } | null>(null);
  const cur = one ? projectOf(one, projects) : undefined;
  // V0.132: a project made from threads lives on their host.
  if (draft) return <NewProjectStep initialName={draft.name} initialFolder={draft.folder} fromThread={threads.length > 0} host={threads[0]?.hostId} onCreate={onCreate} onCancel={() => setDraft(null)} />;
  const sub = base.folder ? `${base.name} · threads in ${tildePath(base.folder)} land there` : "Just a name; file threads here by hand";
  return (
    <>
      <ProjectList
        projects={projects}
        selectedId={one ? (cur?.id ?? null) : undefined}
        autoFocus
        allLabel="No project"
        newRow={{ fromThread: !!one, sub, onNew: (q) => setDraft({ name: q.trim() || base.name, folder: base.folder }) }}
        onPick={onPick}
      />
      {cur && onEdit && (
        <button class="plfoot" onClick={() => onEdit(cur)}>
          Edit {cur.name}…
        </button>
      )}
    </>
  );
}
