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
import { useBackLayer } from "./back-layer.ts";

/** A project's one folder (V0.55): the short path and what it does with "Change…" through the host folder picker, or
 *  that threads are filed by hand with "Choose folder…". × leaves the folder out where onClear is given.
 *  V0.dialogs-review (13): its label is "Project folder", so it is never a second "Folder" beside the thread's own in
 *  New thread; the picker it opens has the same title. */
function FolderField({ folder, title, host, onPick, onClear, none }: { folder: string; title: string; /** V0.132: whose folders */ host?: HostId; onPick(path: string): void; onClear?(): void; /** the words for no folder, where the usual ones do not fit */ none?: string }) {
  const picker = useFolderPick();
  const pick = async () => {
    const path = await picker.pick({ title, ...(host ? { host } : {}) });
    if (path) onPick(path);
  };
  return (
    <div class="fld">
      <span>Project folder</span>
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
        <p class="lede">{none ?? `${NO_FOLDER_LINE}.`}</p>
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
 *  that host's, and a new host drops a folder picked on another. `plain` (V0.dialogs-review (11)): the form around it
 *  has the primary button (New thread's "Create project and start"), so this step's own is a plain one. */
export function NewProjectStep({ initialName, initialFolder, fromThread, many, host: fixed, onCreate, onCancel, cancelLabel, onDraft, plain, note, onError, onEnter }: { initialName: string; initialFolder: string; fromThread: boolean; /** several threads are filed in it */ many?: boolean; host?: HostId; onCreate(name: string, folder: string, host: HostId): Promise<string | null>; onCancel(): void; /** V0.settings-ia: "Cancel" where the step is a page of its own (Settings); "Back" where it is a step of the picker */ cancelLabel?: string; /** V0.63: the name and folder as they are typed */ onDraft?(name: string, folder: string): void; plain?: boolean; /** a line above the buttons (New thread: where the thread starts) */ note?: string; /** the form around it shows the step's errors (New thread's foot, always in view); else they show here */ onError?(e: string | null): void; /** what Enter in Name does instead of this step's own button (New thread: its one primary button) */ onEnter?(): void }) {
  const hs = useHosts();
  // Slice 2b: the picker starts on the first host in the owner's order (shown in it), the only one with one host.
  // Until one is picked it follows the list: a link can open the form before the host list has arrived.
  const [picked, setOwn] = useState<HostId | undefined>(undefined);
  const own = picked ?? pickerStart(hs.hosts);
  const host = fixed ?? own;
  const down = hostOfflineNote(host, hs.hosts, hs.now);
  const [name, setName] = useState(initialName);
  const [folder, setFolder] = useState(initialFolder);
  // eslint-disable-next-line react-hooks/exhaustive-deps -- reports the draft when the name or folder changes; onDraft is the parent's callback, new on each of its renders
  useEffect(() => onDraft?.(name, folder), [name, folder]);
  // V0.dialogs-review (12): the caller's host changed under the open step (New thread's Host field): the name stays,
  // the folder is a path on the other host and goes (the form says so under Host).
  const was = useRef(fixed);
  useEffect(() => {
    if (was.current !== fixed) setFolder("");
    was.current = fixed;
  }, [fixed]);
  const [ownErr, setOwnErr] = useState<string | null>(null);
  const [noName, setNoName] = useState(false);
  const setErr = (e: string | null) => (onError ? onError(e) : setOwnErr(e));
  const err = onError ? null : ownErr;
  const [busy, setBusy] = useState(false);
  const nameRef = useRef<HTMLInputElement>(null);
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    nameRef.current?.focus();
    nameRef.current?.select();
    // The step opens under the control that opened it, often at the lower edge of what scrolls (the phone's New
    // thread, above its bottom bar): its own buttons are its end and must be in view, not cut by that edge.
    root.current?.scrollIntoView({ block: "nearest" });
  }, []);
  const submit = async () => {
    if (busy) return;
    if (!name.trim()) {
      nameRef.current?.focus();
      setNoName(true);
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
    <div class="pform" ref={root}>
      <label class="fld">
        <span>Name</span>
        <input
          ref={nameRef}
          value={name}
          onInput={(e) => (setNoName(false), setName((e.target as HTMLInputElement).value))}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.isComposing && !e.metaKey && !e.ctrlKey) (e.preventDefault(), onEnter && name.trim() ? onEnter() : void submit());
          }}
          placeholder="Name"
          aria-invalid={noName || undefined}
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
      <FolderField folder={folder} title="Project folder" host={host} onPick={setFolder} onClear={() => setFolder("")} none={plain ? "No folder: threads are filed in it by hand, as this one will be." : undefined} />
      <p class="lede">Icon, colour and defaults are on the project screen, later.</p>
      {err && (
        <div class="err prose" role="alert">
          {err}
        </div>
      )}
      {note && <p class="lede pnote">{note}</p>}
      <div class="pformb">
        <button class="btn" onClick={onCancel}>
          {cancelLabel ?? "Back"}
        </button>
        <button class={plain ? "btn" : "btn p"} disabled={busy} onClick={() => void submit()}>
          {fromThread ? (many ? "Create and file these threads" : "Create and file this thread") : "Create project"}
        </button>
      </div>
    </div>
  );
}

/** The one project picker (V0.30): file a thread, unfile it, or make a project from it in one step. `phone`: the
 *  front it is on (the sheet of the phone, the dialog of the desktop). */
export function ProjectPicker({ projects, threads, onPick, onCreate, onEdit, phone }: { projects: Project[]; threads: Thread[]; onPick(p: Project | null): void; onCreate(name: string, folder: string): Promise<string | null>; onEdit?(p: Project): void; phone?: boolean }) {
  const one = threads.length === 1 ? threads[0] : undefined;
  const base = one ? newProjectDraft(one) : { name: "", folder: "" };
  // V0.dialogs-review (12): what was typed in the New project step stays while the picker is open: Back shows the
  // list again with the name in its search, and the step opens again as it was left.
  const [open, setOpen] = useState(false);
  const [kept, setKept] = useState<{ name: string; folder: string } | null>(null);
  const [first, setFirst] = useState("");
  const cur = one ? projectOf(one, projects) : undefined;
  const back = () => setOpen(false);
  // V0.dialogs-review (4): on the phone the step is a layer over the Project sheet, so Back returns to the list and
  // the sheet (a navigation entry) stays. On the desktop Esc does the same, as in New thread: one level up, the name
  // and folder kept; the next Esc closes the dialog.
  useBackLayer(!!phone && open, back);
  const onKey = (e: KeyboardEvent) => {
    if (e.key !== "Escape" || e.defaultPrevented) return;
    e.preventDefault();
    e.stopPropagation();
    back();
  };
  // V0.132: a project made from threads lives on their host.
  if (open)
    return (
      <div class="ppstep" onKeyDown={onKey}>
        <NewProjectStep initialName={first || kept?.name || base.name} initialFolder={kept ? kept.folder : base.folder} fromThread={threads.length > 0} many={threads.length > 1} host={threads[0]?.hostId} onCreate={onCreate} onCancel={back} onDraft={(name, folder) => setKept({ name, folder })} />
      </div>
    );
  const sub = base.folder ? `${base.name} · threads in ${tildePath(base.folder)} land there` : "Just a name; file threads here by hand";
  return (
    <>
      <ProjectList
        projects={projects}
        selectedId={one ? (cur?.id ?? null) : undefined}
        // V0.dialogs-review (14): the search takes the focus on the desktop only. On the phone a focused field raises
        // the keyboard over the list, whose usual use is one tap on a project (the rule of every sheet, motion-ui.tsx).
        autoFocus={!phone}
        allLabel="No project"
        initialQuery={kept && kept.name.trim() !== base.name ? kept.name.trim() : ""}
        newRow={{ fromThread: !!one, sub, onNew: (q) => (setFirst(q.trim()), setOpen(true)) }}
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
