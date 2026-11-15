// The Changes board (SPEC 0.5 V0.64): every open change of every thread, by stage, with its actions. The same body on
// desktop (a dialog) and phone (a screen).

import { useState } from "preact/hooks";
import type { Project, Thread } from "./types.ts";
import { boardItems, boardProjects, columns, type BoardItem } from "./board.ts";
import { ChangeCard } from "./offers-ui.tsx";
import { ProjectIcon } from "./project-ui.tsx";
import { colorVar } from "./projects.ts";

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
  const items = boardItems(p.threads, p.projects, { projectId, q, ended: showEnded });
  const cols = columns(items, showEnded);
  const chips = boardProjects(p.threads, p.projects);
  const total = items.length;
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
        </div>
      </div>
      {total === 0 ? (
        <div class="bdempty">{q || projectId ? "No change matches." : "No changes in flight. Agents record them with change_open; Fyr records git branches and PRs itself."}</div>
      ) : (
        <div class="bdcols">
          <div class="bdsum">{cols.map((c) => `${c.label} ${c.count}`).join(" · ")}</div>
          {cols.filter((c) => c.count > 0).map((col) => (
            <section key={col.key} class={`bdcol ${col.key}`} aria-label={col.label}>
              <h3>
                {col.label} <span class="bdn">{col.count}</span>
              </h3>
              {col.entries.map((e) =>
                e.kind === "one" ? (
                  <Card key={e.item.change.id + e.item.thread.id} i={e.item} p={p} />
                ) : (
                  <div key={`g-${e.group}`} class="bdgroup">
                    <div class="bdgh">
                      Group <b>{e.group}</b> · {e.items.length}
                    </div>
                    {e.items.map((i) => (
                      <Card key={i.change.id + i.thread.id} i={i} p={p} />
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

function Card({ i, p }: { i: BoardItem; p: BoardProps }) {
  const from = (
    <button class="bdfrom" onClick={() => p.onOpenThread(i.thread.id)} title="Open the thread">
      {i.project && <ProjectIcon project={i.project} name={i.project.name} size={14} />}
      <span class="ell">{i.thread.title || "Untitled thread"}</span>
      <span aria-hidden="true">›</span>
    </button>
  );
  return <ChangeCard c={i.change} cwd={i.thread.cwd} from={from} onRun={(a, args) => p.onRun(i.thread.id, a, args)} />;
}
