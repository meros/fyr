// The Projects settings page (split from settings-ui.tsx).

import type { PageView, PageUi, SettingsProps } from "./settings-ui.tsx";
import { Fragment } from "preact";
import { colorVar, projectSearchLine, projectSubtitle, searchProjects, settingsProjects } from "./projects.ts";
import { quietNote } from "./quiet.ts";
import { ProjectIcon } from "./project-ui.tsx";
import { Empty, useSet } from "./settings-rows.tsx";
import { Grip, MoveControls, ReorderButton, useReorder } from "./reorder-ui.tsx";
import { archivedProjects, isArchived } from "./archived.ts";

// ---- Projects ----

/** V0.settings-ia: the number at the right of a project's row has its noun ("12 threads"); a bare number said nothing. */
const threadsWord = (n: number | undefined) => `${n ?? 0} ${n === 1 ? "thread" : "threads"}`;

/** V0.project-archive: the page lists the active projects; the archived ones follow under "Archived projects". */
function activeProjects(projects: SettingsProps["projects"]) {
  return settingsProjects(projects).filter((x) => !isArchived(x));
}

export function projectsPage(p: SettingsProps, ui: PageUi): Omit<PageView, "title"> {
  return {
    end: <NewProjectButton ui={ui} n={activeProjects(p.projects).length} onNew={() => p.go({ page: "projects", sub: "new" })} />,
    body: <ProjectsPage p={p} ui={ui} />,
  };
}

/** V0.settings-ia: New project opens the sub-page projects/new (settings-ui.tsx), as Add provider does. */
function NewProjectButton({ ui, n, onNew }: { ui: PageUi; n: number; onNew(): void }) {
  const env = useSet();
  // The phone reorders in a mode (reorder-ui.tsx says why). The desktop drags the grip, so it has no button.
  if (env.phone && ui.reordering) return <ReorderButton on set={ui.setReordering} />;
  return (
    <>
      {env.phone && n > 1 && !ui.q.trim() && !env.readOnly && <ReorderButton on={false} set={ui.setReordering} />}
      <button type="button" class={`btn addp${ui.empty ? " p" : ""}`} disabled={env.readOnly} data-sub="new" onClick={onNew}>
        New project
      </button>
    </>
  );
}

function ProjectsPage({ p, ui }: { p: SettingsProps; ui: PageUi }) {
  const { q, setQ } = ui;
  const env = useSet();
  const all = activeProjects(p.projects);
  const archived = archivedProjects(p.projects);
  const list = searchProjects(all, q);
  const ro = useReorder(
    all.map((x) => x.id!),
    async (next) => {
      // V0.project-archive: the archived keep their place after the active ones.
      const e = await p.projectActions.order([...next, ...archived.map((x) => x.id!)]);
      if (!e) env.saved();
      return e;
    },
  );
  const ids = ro.ids;
  const mode = env.phone && ui.reordering && ids.length > 1;
  const grips = !env.phone && !q.trim() && ids.length > 1 && !env.readOnly; // while the search has text, reorder is off
  return (
    <>
      {
        <section class="sg" aria-label="Projects">
          {all.length >= 9 && !mode && <input class="pq" type="search" placeholder={`Search ${all.length} projects`} aria-label="Search projects" value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} />}
          {ro.err && (
            <div class="seterr" role="alert">
              {ro.err}
            </div>
          )}
          {all.length === 0 ? (
            <div class="sg-c">
              <Empty title="No projects yet" text="A project gathers the threads of one folder. Fyr makes one when you start a thread in a new folder." />
            </div>
          ) : (
            <div class="pls setpl">
              {ro.sort(list, (x) => x.id!).map((x) => {
                const quiet = quietNote(x, p.now);
                const i = ids.indexOf(x.id!);
                const body = (
                  <>
                    <ProjectIcon project={x} name={x.name} size={28} />
                    <span class="plt">
                      <span class="ell pln">{x.name}</span>
                      <span class="ell pls2">
                        {quiet ? `${quiet} · ` : ""}
                        {projectSubtitle(x)}
                      </span>
                    </span>
                  </>
                );
                const style = { "--pc": colorVar(x.color, x.name) };
                const r = ro.row(i);
                if (mode)
                  return (
                    <div key={x.id} class={`pli mvrow ${r.class}`} ref={r.ref} data-sub={x.id} style={style}>
                      {body}
                      <MoveControls ro={ro} i={i} name={x.name} />
                    </div>
                  );
                const go = () => p.go({ page: "projects", sub: x.id! });
                const open = (
                  <button type="button" data-sub={x.id} class={grips ? "plb" : "pli"} style={grips ? undefined : style} onClick={go}>
                    {body}
                    <span class="plc" title={`${x.threads ?? 0} threads`}>
                      {threadsWord(x.threads)}
                    </span>
                  </button>
                );
                if (!grips) return <Fragment key={x.id}>{open}</Fragment>;
                return (
                  // The whole row opens the project, as without a grip; the grip only moves it. The button opens it from the
                  // keyboard too (V0.composer-focus-reach (3)); the row's own padding around it opens it for the pointer.
                  <div key={x.id} class={`pli g ${r.class}`} ref={r.ref} style={style} onClick={(e) => e.target === e.currentTarget && go()}>
                    <Grip {...ro.grip(i, x.name)} />
                    {open}
                  </div>
                );
              })}
              {!list.length && <div class="plempty">No project matches “{q}”.</div>}
            </div>
          )}
          {q.trim() ? <div class="sg-f">{projectSearchLine(list.length, all.length)}</div> : all.length > 1 && <div class="sg-f">This order is used in New thread, the project chips and every list of projects, on all your devices.</div>}
        </section>
      }
      {!mode && archived.length > 0 && <ArchivedList p={p} list={archived} />}
    </>
  );
}


/** V0.project-archive: the archived projects, the same on both fronts; a row opens the project's page, which has
 *  Unarchive and the project's threads. */
function ArchivedList({ p, list }: { p: SettingsProps; list: SettingsProps["projects"] }) {
  return (
    <section class="sg parchl" aria-label="Archived projects">
      <div class="sg-h">
        <span class="sg-t">Archived projects</span>
      </div>
      <div class="pls setpl">
        {list.map((x) => (
          <button key={x.id} type="button" data-sub={x.id} class="pli" style={{ "--pc": colorVar(x.color, x.name) }} onClick={() => p.go({ page: "projects", sub: x.id! })}>
            <ProjectIcon project={x} name={x.name} size={28} />
            <span class="plt">
              <span class="ell pln">{x.name}</span>
              <span class="ell pls2">{projectSubtitle(x)}</span>
            </span>
            <span class="plc" title={`${x.threads ?? 0} threads`}>
              {threadsWord(x.threads)}
            </span>
          </button>
        ))}
      </div>
      <div class="sg-f">Their threads are out of the inbox and the pickers until a new message brings one back.</div>
    </section>
  );
}
