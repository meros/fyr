// The Changes board (SPEC 0.5 V0.64): every open change of every thread, by stage, with its actions. The same body on
// desktop (a dialog) and phone (a screen).

import { useState } from "preact/hooks";
import type { Project, Thread } from "./types.ts";
import { boardItems, boardProjects, columns, greenMerges, mergeAllGreen, type BoardItem } from "./board.ts";
import { useApi } from "./api-context.ts";
import { useAsk } from "./confirm-ui.tsx";
import { shortHead } from "./pr.ts";
import { ChangeCard } from "./offers-ui.tsx";
import { ProjectIcon } from "./project-ui.tsx";
import { colorVar } from "./projects.ts";
import { boardSettledPref, setBoardSettledPref } from "./prefs.ts";

export interface BoardProps {
  threads: Record<string, Thread>;
  projects: Project[];
  /** a thread command, for example action.run */
  onRun(threadId: string, action: string, args?: Record<string, unknown>): void;
  onOpenThread(id: string): void;
  phone?: boolean;
}

export function ChangesBoard(p: BoardProps) {
  const [projectId, setProjectId] = useState<string | null>(null);
  const [q, setQ] = useState("");
  const [showEnded, setShowEnded] = useState(false);
  const [showSettled, setShowSettled] = useState(boardSettledPref); // V0.78 (#54): remembered per device
  const items = boardItems(p.threads, p.projects, { projectId, q, ended: showEnded, settled: showSettled });
  const cols = columns(items, showEnded);
  const chips = boardProjects(p.threads, p.projects);
  const total = items.length;
  // V0.97: Merge all green: one merge per ready PR, one after another, each bound to the head shown here.
  const api = useApi();
  const ask = useAsk();
  const greens = greenMerges(items);
  const [merging, setMerging] = useState(false);
  const [mergeErrs, setMergeErrs] = useState<Record<string, string>>({});
  const mergeAll = async () => {
    const list = greens;
    const names = list.map((m) => `#${m.pr.split("/").pop()} at ${shortHead(m.args.head)}`).join(", ");
    if (!(await ask({ title: `Merge ${list.length} green PR${list.length === 1 ? "" : "s"}?`, text: `Fyr merges ${names} on GitHub, one after another. A PR whose head moved is left as it is.`, confirm: "Merge all" }))) return;
    setMerging(true);
    setMergeErrs({});
    try {
      setMergeErrs(await mergeAllGreen(list, (m) => api.command("action.run", m.thread, m.args)));
    } finally {
      setMerging(false);
    }
  };
  return (
    <div class={`board${p.phone ? " phone" : ""}`}>
      <div class="bdf">
        <input class="pq" type="search" placeholder="Search changes, targets, threads" value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} aria-label="Search changes" />
        <div class="bdchips" role="group" aria-label="Project">
          <button class={`chip${projectId === null ? " on" : ""}`} aria-pressed={projectId === null} onClick={() => setProjectId(null)}>
            <span>All projects</span>
          </button>
          {chips.map((pr) => (
            <button key={pr.id} class={`chip${projectId === pr.id ? " on" : ""}`} aria-pressed={projectId === pr.id} style={{ "--pc": colorVar(pr.color, pr.name) }} onClick={() => setProjectId(projectId === pr.id ? null : pr.id!)}>
              <ProjectIcon project={pr} name={pr.name} size={14} />
              <span>{pr.name}</span>
            </button>
          ))}
          <label class="bdend">
            <input type="checkbox" checked={showEnded} onChange={(e) => setShowEnded((e.target as HTMLInputElement).checked)} />
            <span>Show ended</span>
          </label>
          <label class="bdend">
            <input type="checkbox" checked={showSettled} onChange={(e) => { const on = (e.target as HTMLInputElement).checked; setShowSettled(on); setBoardSettledPref(on); }} />
            <span>Show settled</span>
          </label>
        </div>
      </div>
      {total === 0 ? (
        <div class="bdempty">{q || projectId ? "No change matches." : "No changes in flight. Agents record them with change_open; Fyr records git branches and PRs itself."}</div>
      ) : (
        <div class="bdcols">
          <div class="bdsum">
            <span>{cols.map((c) => `${c.label} ${c.count}`).join(" · ")}</span>
            {greens.length > 0 && (
              <button class="btn p sm bdgreen" disabled={merging} onClick={() => void mergeAll()}>
                {merging ? "Merging…" : `Merge all green (${greens.length})`}
              </button>
            )}
          </div>
          {cols.filter((c) => c.count > 0).map((col) => (
            <section key={col.key} class={`bdcol ${col.key}`} aria-label={col.label}>
              <h3>
                {col.label} <span class="bdn">{col.count}</span>
              </h3>
              {col.entries.map((e) =>
                e.kind === "one" ? (
                  <Card key={e.item.change.id + e.item.thread.id} i={e.item} p={p} err={mergeErrs[`${e.item.thread.id}/${e.item.change.id}`]} />
                ) : (
                  <div key={`g-${e.group}`} class="bdgroup">
                    <div class="bdgh">
                      Group <b>{e.group}</b> · {e.items.length}
                    </div>
                    {e.items.map((i) => (
                      <Card key={i.change.id + i.thread.id} i={i} p={p} err={mergeErrs[`${i.thread.id}/${i.change.id}`]} />
                    ))}
                  </div>
                ),
              )}
            </section>
          ))}
        </div>
      )}
    </div>
  );
}

function Card({ i, p, err }: { i: BoardItem; p: BoardProps; err?: string }) {
  const from = (
    <button class="bdfrom" onClick={() => p.onOpenThread(i.thread.id)} title="Open the thread">
      {i.project && <ProjectIcon project={i.project} name={i.project.name} size={14} />}
      <span class="ell">{i.thread.title || "Untitled thread"}</span>
      <span aria-hidden="true">›</span>
    </button>
  );
  const card = <ChangeCard c={i.change} cwd={i.thread.cwd} from={from} onRun={(a, args) => p.onRun(i.thread.id, a, args)} />;
  if (!err) return card;
  return (
    <div class="bdmerged">
      {card}
      <div class="bderr" role="alert">
        Not merged: {err}
      </div>
    </div>
  );
}
