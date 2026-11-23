// Linking a project across hosts (decision 0034): what the project screen needs, and the Host group's rows.
import { useState } from "preact/hooks";
import type { Project } from "./types.ts";
import { addHosts, linkCandidates, sameRepo } from "./projects.ts";
import { tildePath } from "./paths.ts";
import { Row, useSet } from "./settings-rows.tsx";
import { useFolderPick } from "./folder-picker.tsx";
import { HostChip, useHosts } from "./host-pick-ui.tsx";
import { hostChip } from "./hosts.ts";
import type { HostId } from "./hostid.ts";

/** Decision 0034: what the project screen needs to show and change a project's links. */
export interface ProjectLinking {
  /** the project as the lists show it */
  group: Project;
  candidates: Project[];
  link(other: string): Promise<string | null>;
  unlink(id: string): Promise<string | null>;
  add(host: HostId, folder: string): Promise<string | null>;
  /** open a member's page: its folder and defaults are its own */
  open(id: string): void;
}

export function linkingFor(group: Project, projects: Project[], actions: { link(id: string, other: string): Promise<string | null>; unlink(id: string): Promise<string | null>; add(id: string, host: HostId, folder: string): Promise<string | null> }, open: (id: string) => void): ProjectLinking {
  return { group, candidates: linkCandidates(group, projects), link: (other) => actions.link(group.id!, other), unlink: actions.unlink, add: (host, folder) => actions.add(group.id!, host, folder), open };
}

/** The Host group's rows for a linked project: each host's record (omitted when HostTabs renders them above), and what
 *  can join. The same on the desktop and the phone. */
export function LinkRows({ project, linking, tabbed }: { project: Project; linking: ProjectLinking; tabbed?: boolean }) {
  const hs = useHosts();
  const env = useSet();
  const [pick, setPick] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const run = async (f: () => Promise<string | null>) => {
    setBusy(true);
    const e = await f();
    setBusy(false);
    setErr(e);
    if (!e) env.saved();
    return e;
  };
  const picker = useFolderPick();
  const [hostPick, setHostPick] = useState("");
  const addable = addHosts(linking.group, hs.hosts ?? []);
  const addTo = addable.find((h) => h.id === hostPick) ?? addable[0];
  const members = linking.group.members ?? [];
  const chosen = linking.candidates.some((c) => c.id === pick) ? pick : (linking.candidates[0]?.id ?? "");
  return (
    <>
      {!tabbed &&
        members
          .filter((m) => m.id !== project.id)
          .map((m) => {
            const chip = hostChip(m.hostId, hs.hosts, hs.now);
            return (
              <Row
                key={m.id}
                class="hostrow"
                title={chip ? <HostChip chip={chip} /> : m.host}
                desc={`Also here: ${m.folder ? tildePath(m.folder) : "no folder"}`}
                end={
                  <button type="button" class="btn" onClick={() => linking.open(m.id!)}>
                    Open
                  </button>
                }
              />
            );
          })}
      {members.length > 1 && (
        <Row
          title="Unlink this host"
          desc="This host's project stands alone again, with its folder and threads."
          end={
            <button type="button" class="btn" disabled={busy || env.readOnly} onClick={() => void run(() => linking.unlink(project.id!))}>
              Unlink
            </button>
          }
        />
      )}
      {linking.candidates.length > 0 && (
        <Row
          title="Same project on another host"
          desc={sameRepo(linking.group, linking.candidates[0]) ? `${linking.candidates[0].name} on ${linking.candidates[0].host} looks like the same repository. Link it to show them as one project.` : "Show them as one project. Each host keeps its own folder and threads."}
          stack
          class="two"
          end={
            <>
              <select aria-label="Project on another host" value={chosen} disabled={busy || env.readOnly} onChange={(e) => setPick((e.target as HTMLSelectElement).value)}>
                {linking.candidates.map((c) => (
                  <option value={c.id}>
                    {c.name} · {c.host}
                    {sameRepo(linking.group, c) ? " · same repository" : ""}
                  </option>
                ))}
              </select>
              <button type="button" class="btn" disabled={busy || env.readOnly || !chosen} onClick={() => void run(() => linking.link(chosen))}>
                Link
              </button>
            </>
          }
        />
      )}
      {addTo && (
        <Row
          title="Add on another host"
          desc="Choose a folder there. The project keeps its name, colour and quiet hours."
          stack
          class="two"
          end={
            <>
              <select aria-label="Host to add the project on" value={addTo.id} disabled={busy || env.readOnly} onChange={(e) => setHostPick((e.target as HTMLSelectElement).value)}>
                {addable.map((h) => (
                  <option value={h.id}>{h.name}</option>
                ))}
              </select>
              <button
                type="button"
                class="btn"
                disabled={busy || env.readOnly}
                onClick={async () => {
                  const path = await picker.pick({ title: `Folder for ${linking.group.name} on ${addTo.name}`, host: addTo.id });
                  if (path) await run(() => linking.add(addTo.id!, path));
                }}
              >
                Choose folder…
              </button>
            </>
          }
        />
      )}
      {err && (
        <span class="sr-d err" role="alert">
          {err}
        </span>
      )}
    </>
  );
}
