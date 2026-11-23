// The Projects settings page (split from settings-ui.tsx).

import type { PageView, PageUi, SettingsProps } from "./settings-ui.tsx";
import { Fragment } from "preact";
import { colorVar, projectSearchLine, projectSubtitle, searchProjects, settingsProjects } from "./projects.ts";
import { quietNote } from "./quiet.ts";
import { ProjectIcon } from "./project-ui.tsx";
import { Empty, Group, Row, useSet } from "./settings-rows.tsx";
import { Grip, MoveControls, ReorderButton, useReorder } from "./reorder-ui.tsx";
import { NewProjectStep } from "./project-picker.tsx";

// ---- Projects ----

export function projectsPage(p: SettingsProps, ui: PageUi): Omit<PageView, "title"> {
  return {
    end: <NewProjectButton ui={ui} n={settingsProjects(p.projects).length} />,
    body: <ProjectsPage p={p} ui={ui} />,
  };
}

function NewProjectButton({ ui, n }: { ui: PageUi; n: number }) {
  const env = useSet();
  if (ui.adding) return null;
  // The phone reorders in a mode (reorder-ui.tsx says why). The desktop drags the grip, so it has no button.
  if (env.phone && ui.reordering) return <ReorderButton on set={ui.setReordering} />;
  return (
    <>
      {env.phone && n > 1 && !ui.q.trim() && !env.readOnly && <ReorderButton on={false} set={ui.setReordering} />}
      <button type="button" class={`btn addp${ui.empty ? " p" : ""}`} disabled={env.readOnly} onClick={() => ui.setAdding(true)}>
        New project
      </button>
    </>
  );
}

function ProjectsPage({ p, ui }: { p: SettingsProps; ui: PageUi }) {
  const { adding, setAdding, q, setQ } = ui;
  const env = useSet();
  const all = settingsProjects(p.projects);
  const list = searchProjects(all, q);
  const ro = useReorder(
    all.map((x) => x.id!),
    async (next) => {
      const e = await p.projectActions.order(next);
      if (!e) env.saved();
      return e;
    },
  );
  const ids = ro.ids;
  const mode = env.phone && ui.reordering && ids.length > 1;
  const grips = !env.phone && !q.trim() && ids.length > 1 && !env.readOnly; // while the search has text, reorder is off
  return (
    <>
      {adding ? (
        <Group title="New project">
          <Row col>
            <NewProjectStep
              initialName=""
              initialFolder=""
              fromThread={false}
              onCancel={() => setAdding(false)}
              onCreate={async (name, folder, host) => {
                const e = await p.projectActions.create(name, folder, host);
                if (!e) setAdding(false);
                return e;
              }}
            />
          </Row>
        </Group>
      ) : (
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
                      {x.threads ?? 0}
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
      )}
    </>
  );
}

