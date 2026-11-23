// The Automation settings page (split from settings-ui.tsx).

import { type SettingsProps } from "./settings-ui.tsx";
import { useEffect, useState } from "preact/hooks";
import type { HostId } from "./hostid.ts";
import { hostUrl } from "./urls.ts";
import { budgetLine, jobLines, type HostLoad } from "./host-load.ts";
import { useUnd } from "./undeclared-ctx.tsx";
import { UndIcon } from "./undeclared-ctx.tsx";
import { GuardChart } from "./guard-ui.tsx";
import { guardLog, guardSummary, rerunRows } from "./guard.ts";
import { clockTime } from "./format.ts";
import { leftoverRows, tempRows } from "./leftovers.ts";
import { invariantRows } from "./invariants.ts";
import { addedRows, parseThreshold, thresholdError, thresholdRows, undeclaredRows } from "./undeclared.ts";
import { refreshLoad, useLoad } from "./load-ui.tsx";
import { repoKey, sortRepos } from "./automerge.ts";
import { changeLines, changesLabel, githubPause, githubRows, githubState, releaseSource, updateRow, updateState } from "./updates.ts";
import { Empty, Group, LoadError, Row, SaveField, Skeleton, Switch, useSet } from "./settings-rows.tsx";
import { type Data } from "./settings-load.ts";

// ---- Automation ----

/** V0.111 Auto-deploy and V0.102 Auto-merge: what Fyr does without the owner's press. Only the owner changes them here;
 *  no agent tool can. */
export function AutomationPage({ d, hasDeploy, api, focus }: { d: Data; hasDeploy: boolean; api: SettingsProps["api"]; focus?: string }) {
  const [deployErr, setDeployErr] = useState<string | null>(null);
  const [repoErr, setRepoErr] = useState<{ repo: string; text: string } | null>(null);
  const [q, setQ] = useState("");
  const repos = sortRepos(d.repos ?? []);
  const shown = q.trim() ? repos.filter((r) => r.repo.includes(q.trim().toLowerCase())) : repos;
  return (
    <>
      {hasDeploy &&
        (d.deploy === null && d.deployErr ? (
          <LoadError text={d.deployErr} onRetry={() => void d.loadDeploy()} />
        ) : d.deploy === null ? (
          <Skeleton rows={1} />
        ) : (
          <Group label="Auto-update">
            <Row
              label
              title="Update Fyr by itself"
              desc="Installs each signed release CI publishes for main, once it has stayed the newest for 10 minutes. The new version is checked before the switch and rolled back if it is not healthy. A local build of a checkout always waits for your press."
              error={deployErr}
              end={<Switch on={d.deploy} label="Auto-update Fyr" onChange={async (on) => setDeployErr(await d.runDeploy(on))} />}
            />
          </Group>
        ))}
      {d.updates && <UpdatesGroup d={d} />}
      {d.updates?.github && <GitHubGroup d={d} />}
      <HeavyJobsGroup host={d.host.name} id={d.host.id} api={api} focus={focus} />
      {d.repos === null && d.reposErr ? (
        <LoadError text={d.reposErr} onRetry={() => void d.loadRepos()} />
      ) : d.repos === null ? (
        <Skeleton rows={2} />
      ) : (
        <Group title="Merge without asking" label="Auto-merge" fold={{ id: `${d.host.name}:automerge`, open: true, summary: `${repos.filter((r) => r.on).length} of ${repos.length} repos on` }} foot="In a repo that is on, an agent's merge ask merges once the checks pass on the head it asked for. A repo shows here once a thread has a pull request in it.">
          {repos.length > 8 && (
            <div class="sr srch">
              <input class="pq" type="search" placeholder={`Search ${repos.length} repos`} aria-label="Search repos" value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} />
            </div>
          )}
          {!repos.length && <Empty text="No repos yet. A repo shows here once a thread has a pull request in it." />}
          {shown.map((r) => (
            <Row key={r.repo} label title={r.repo} mono error={repoErr?.repo === r.repo ? repoErr.text : null} end={<Switch on={r.on} label={`Auto-merge ${r.repo}`} onChange={async (on) => { const e = await d.runRepo(r.repo, on); setRepoErr(e ? { repo: r.repo, text: e } : null); }} />} />
          ))}
          <RepoAdd run={d.runRepo} />
        </Group>
      )}
    </>
  );
}

/** Decision 0029 S1 (#185), V0.164, V0.209: heavy jobs on this host, which run by themselves: what runs undeclared now,
 *  the commands Fyr learned, when a thread counts as heavy, and the classes the host measured.
 *  Desktop and phone share it; a read-only front shows it all with its buttons disabled. */
function HeavyJobsGroup({ host, id, api, focus }: { host: string; id?: HostId; api: SettingsProps["api"]; focus?: string }) {
  const env = useSet();
  const list = useLoad();
  const h = list.find((x) => x.host === host) ?? (list.length === 1 ? list[0] : undefined);
  if (!h) return null;
  return (
    <>
      <LeftoverGroups h={h} host={host} id={id} api={api} focus={focus} />
      {h.provider && <UndeclaredGroups h={h} host={host} id={id} api={api} />}
      {h.provider && <MeasuredJobs h={h} host={host} id={id} api={api} env={env} />}
      <InvariantGroup h={h} host={host} />
    </>
  );
}

/** V0.load-invariants, V0.thread-invariants: the load guard's and thread state machine's invariants, each with its last
 *  violation and how often it broke. A broken one is a card in Needs you (invariants-ui.tsx). Desktop and phone share it. */
function InvariantGroup({ h, host }: { h: HostLoad; host: string }) {
  const und = useUnd();
  const now = und.now();
  const loadRows = invariantRows(h, now, "load");
  const threadRows = invariantRows(h, now, "thread");
  if (!loadRows.length && !threadRows.length) return null;
  const loadBroken = loadRows.filter((r) => r.desc !== "never broken").length;
  const threadBroken = threadRows.filter((r) => r.desc !== "never broken").length;
  return (
    <>
      {loadRows.length > 0 && (
        <Group label="Load invariants" title="Load invariants" fold={{ id: `${host}:invariants`, open: false, summary: loadBroken ? `${loadBroken} of ${loadRows.length} broken` : `${loadRows.length} held` }} foot="Fyr checks these on each tick of the memory guard. A broken one writes an invariant line to fyrd's log and, at most once a day, a card in Needs you with the evidence; its Fix it starts a thread in this host's Fyr checkout. Fyr never starts the fix by itself.">
          {loadRows.map((r) => (
            <Row key={r.id} class="invrow" title={r.id} mono desc={`${r.text} ${r.desc}.`} />
          ))}
        </Group>
      )}
      {threadRows.length > 0 && (
        <Group label="Thread invariants" title="Thread invariants" fold={{ id: `${host}:thread-invariants`, open: false, summary: threadBroken ? `${threadBroken} of ${threadRows.length} broken` : `${threadRows.length} held` }} foot="Fyr checks these on every thread transition, on host restart, and on each tick. A broken one writes an invariant line to fyrd's log and, at most once a day, a card in Needs you with the evidence and recent transitions; its Fix it starts a thread in this host's Fyr checkout.">
          {threadRows.map((r) => (
            <Row key={r.id} class="invrow" title={r.id} mono desc={`${r.text} ${r.desc}.`} />
          ))}
        </Group>
      )}
    </>
  );
}

/** V0.176: the processes threads left running on this host (End now, Keep for a day) and the worktrees nobody
 *  works in (Remove when pushed and clean; V0.worktree-sweep: Fyr removes them by itself). Absent when there is nothing. The rail's count lands here (sub
 *  "leftovers"). Desktop and phone share it. */
function LeftoverGroups({ h, host, id, api, focus }: { h: HostLoad; host: string; id?: HostId; api: SettingsProps["api"]; focus?: string }) {
  const env = useSet();
  const und = useUnd();
  const run = useHostCommand(id, api);
  const [err, setErr] = useState<{ key: string; text: string } | null>(null);
  const act = async (key: string, action: string, args: Record<string, unknown>) => {
    const e = await run(action, args);
    setErr(e ? { key, text: e } : null);
  };
  const rows = leftoverRows(h, und.lookup, und.now());
  const temps = tempRows(h, und.lookup, und.now());
  // V0.host-tabs: the rail's count opens automation/<host>/leftovers; the group is there once this host's load is read.
  useEffect(() => {
    if (focus === "leftovers") document.querySelector(".lftg")?.scrollIntoView({ block: "start" });
  }, [focus, rows.length]);
  return (
    <>
      {rows.length > 0 && (
        <Group class="lftg" label="Leftover processes" title="Leftover processes" fold={{ id: `${host}:leftovers`, open: true, force: focus === "leftovers", summary: `${rows.length} running` }} foot="What threads left running outside their agent: a browser, a server, a port-forward. Fyr ends a thread's leftovers 30 min after its run ends and all of them when it settles; one the agent kept, or one under timeout, stays until then. End ends one now; Keep holds it for a day, through a settle too. Each end leaves a line on its thread.">
          {rows.map((r) => (
            <Row
              key={r.id}
              class="lftrow"
              title={r.title}
              desc={r.desc}
              error={err?.key === `l:${r.id}` ? err.text : null}
              end={
                <>
                  {r.thread && (
                    <button type="button" class="btn" onClick={() => und.openThread(r.thread!)}>
                      Open
                    </button>
                  )}
                  {r.canKeep && (
                    <button type="button" class="btn" disabled={env.readOnly} onClick={() => void act(`l:${r.id}`, "host.leftoverKeep", { id: r.id, hours: 24 })}>
                      Keep
                    </button>
                  )}
                  <button type="button" class="btn" disabled={env.readOnly} onClick={() => void act(`l:${r.id}`, "host.leftoverEnd", { id: r.id })}>
                    End
                  </button>
                </>
              }
            />
          ))}
        </Group>
      )}
      {temps.length > 0 && (
        <Group label="Worktrees nobody works in" title="Worktrees nobody works in" fold={{ id: `${host}:worktrees`, open: false, summary: `${temps.length} · Fyr removes them by itself` }} foot="Fyr removes a worktree a day after its last commit or checkout when nothing in it is uncommitted and its commit is on the remote (a branch or a pull request), and makes it again when a thread works there; the branch stays. Remove does that now. One with uncommitted or unpushed work, or locked with git worktree lock, stays.">
          {temps.map((t) => (
            <Row
              key={t.path}
              title={t.title}
              mono
              desc={t.desc}
              error={err?.key === `t:${t.path}` ? err.text : null}
              end={
                t.canRemove ? (
                  <button type="button" class="btn" disabled={env.readOnly} onClick={() => void act(`t:${t.path}`, "host.tempRemove", { path: t.path })}>
                    Remove
                  </button>
                ) : undefined
              }
            />
          ))}
        </Group>
      )}
    </>
  );
}

/** A command to the host, with the Saved mark and a fresh read of the load. A host with no install id cannot be named. */
export function useHostCommand(host: HostId | undefined, api: SettingsProps["api"]): (action: string, args: Record<string, unknown>) => Promise<string | null> {
  const env = useSet();
  return async (action, args) => {
    if (!host) return "This host reports no install id, so nothing can be sent to it until it updates";
    const r = await api.command(host, action, undefined, args);
    if (r.ok) (env.saved(), refreshLoad());
    return r.ok ? null : (r.error ?? "That did not work");
  };
}

/** V0.164 (SPEC 8.5). A group is absent when it has nothing to show; the thresholds are absent on a host that does not
 *  send them. */
function UndeclaredGroups({ h, host, id, api }: { h: HostLoad; host: string; id?: HostId; api: SettingsProps["api"] }) {
  const env = useSet();
  const und = useUnd();
  const run = useHostCommand(id, api);
  const [err, setErr] = useState<{ key: string; text: string } | null>(null);
  const fail = (key: string) => (text: string | null) => setErr(text ? { key, text } : null);
  const act = async (key: string, action: string, args: Record<string, unknown>) => fail(key)(await run(action, args));
  const now = und.now();
  const rows = undeclaredRows(h, now, und.lookup);
  const added = addedRows(h);
  const th = thresholdRows(h);
  return (
    <>
      {rows.length > 0 && (
        <Group label="Undeclared now" title="Undeclared now" fold={{ id: `${host}:undeclared`, open: true, summary: `${rows.length} ${rows.length === 1 ? "thread" : "threads"}` }} foot="Threads that use clearly more than their idle level in work Fyr did not recognise as a heavy job. There is nothing to do: Fyr counts the use against this host's heavy jobs, so admitted jobs wait instead of overloading it, and learns a command it sees again. Only when memory is short does Fyr pause the newest one; nothing is killed.">
          {rows.map((r) => (
            <Row
              key={r.thread}
              class="undrow"
              title={
                <>
                  <UndIcon size={13} />
                  {r.title}
                </>
              }
              desc={
                <>
                  {r.project && `${r.project} · `}
                  <span class="mono">{r.command}</span> · {r.usage}
                </>
              }
              end={
                <button type="button" class="btn" onClick={() => und.openThread(r.thread)}>
                  Open
                </button>
              }
            />
          ))}
        </Group>
      )}
      {h.slots?.guard && (
        <Group
          label="Memory guard"
          title="Memory guard"
          fold={{ id: `${host}:guard`, open: false, summary: guardSummary(h.slots.guard, now) }}
          foot="What Fyr did when this host's memory ran short: the shaded band is the time memory was short, a hatched stretch is work Fyr held frozen (never killed), and the number is how many times in a row it froze that thread. Frozen work runs again once memory has been fine for 30 s; one long episode is paused at most 8 times, 60 s apart."
        >
          <Row title="Now" desc={guardSummary(h.slots.guard, now)} />
          <div class="gwrap">
            <GuardChart guard={h.slots.guard} now={now} titleOf={(t) => und.lookup(t).title} />
          </div>
          {guardLog(h.slots.guard, (t) => und.lookup(t).title, clockTime).map((l, i) => (
            <Row key={i} title={l.text} desc={l.at} />
          ))}
        </Group>
      )}
      {added.length > 0 && (
        <Group label="Learned heavy jobs" title="Learned heavy jobs" fold={{ id: `${host}:learned`, open: false, summary: `${added.length} ${added.length === 1 ? "command" : "commands"}` }} foot="Besides the built-in ones (make test, go test ./..., playwright, nix build and the like). Fyr adds a command here after seeing it run heavy three times, and admits it when there is room. Remove one and Fyr does not learn it again.">
          {added.map((a) => (
            <Row
              key={a.pattern}
              title={a.pattern}
              mono
              desc={a.line}
              error={err?.key === `a:${a.pattern}` ? err.text : null}
              end={
                <button type="button" class="btn" disabled={env.readOnly} onClick={() => void act(`a:${a.pattern}`, "host.heavyRemove", { pattern: a.pattern })}>
                  Remove
                </button>
              }
            />
          ))}
        </Group>
      )}
      {h.servicePatterns && <ServiceGroup h={h} host={host} run={run} readOnly={env.readOnly} />}
      {h.rerun && (() => {
        const reruns = rerunRows(h, clockTime);
        return (
          <Group
            label="Kill and rerun"
            title="Kill and rerun"
            fold={{ id: `${host}:rerun`, open: false, summary: reruns.length ? `${reruns.length} ${reruns.length === 1 ? "command" : "commands"}` : "none" }}
            foot="When memory runs short, Fyr may kill a job of one of these instead of freezing it: when it was frozen for 2 minutes and could not resume, or when it usually ends within 5 minutes. The agent is told to run it again, and Fyr admits the rerun once memory has recovered and the job fits. A command killed 3 times in an hour is frozen instead for the rest of that hour. Nothing is here unless you add it: add only commands that are safe to run twice, such as tests, lint or a type check, never a dev server."
          >
            {reruns.map((r) => (
              <Row
                key={r.class}
                title={r.class}
                mono
                desc={r.line}
                error={err?.key === `r:${r.class}` ? err.text : null}
                end={
                  <button type="button" class="btn" disabled={env.readOnly} onClick={() => void act(`r:${r.class}`, "host.rerunClasses", { remove: r.class })}>
                    Remove
                  </button>
                }
              />
            ))}
            {!env.readOnly && (
              <Row error={err?.key === "r:add" ? err.text : null} class="fieldrow">
                <SaveField value="" label="Command to rerun" placeholder="make test" mono clears alwaysButton button="Add" onError={fail("r:add")} check={(v) => (v.trim() ? null : "Write the command, e.g. make test.")} onSave={(v) => run("host.rerunClasses", { add: v.trim() })} />
              </Row>
            )}
          </Group>
        );
      })()}
      {th && (
        <Group label="When a thread counts as heavy" title="When a thread counts as heavy" fold={{ id: `${host}:thresholds`, open: false }} foot="A thread's idle level is what its agent uses between jobs (the CLI and the servers it keeps running), learned per thread.">
          {th.map((r) => (
            <Row
              key={r.key}
              label
              title={r.title}
              desc={r.now}
              error={err?.key === `t:${r.key}` ? err.text : null}
              end={
                <>
                  <SaveField
                    cls="pq num"
                    numeric
                    value={r.value}
                    label={r.title}
                    check={(v) => thresholdError(parseThreshold(r.key, v))}
                    onError={(e) => fail(`t:${r.key}`)(e)}
                    onSave={(v) => {
                      const p = parseThreshold(r.key, v);
                      return thresholdError(p) ? Promise.resolve(thresholdError(p)) : run("host.undeclaredSettings", p as Record<string, number>);
                    }}
                  />
                  <span class="unit">{r.unit}</span>
                </>
              }
            />
          ))}
        </Group>
      )}
    </>
  );
}

/** Decision 0029 S1 (#185): the heavy jobs this host measured, per class (project and command), the newest first. */
function MeasuredJobs({ h, host, id, api, env }: { h: HostLoad; host: string; id?: HostId; api: SettingsProps["api"]; env: { readOnly: boolean } }) {
  const [err, setErr] = useState<{ class: string; text: string } | null>(null);
  const lines = jobLines(h);
  const budget = budgetLine(h); // V0.163: the budget as percentages of the host
  // Decision 0029 S3: Reset forgets a class's learned cost on this host (owner only); it counts as 1.0 again.
  const reset = async (c: string) => {
    if (!id) return setErr({ class: c, text: "This host reports no install id" });
    const r = await api.command(id, "host.jobReset", undefined, { class: c });
    setErr(r.ok ? null : { class: c, text: r.error ?? "Not reset" });
    if (r.ok) refreshLoad();
  };
  return (
    <Group title="Heavy jobs" label="Heavy jobs" fold={{ id: `${host}:jobs`, open: false, summary: budget || `${lines.length} measured` }} foot="What each kind of heavy job used the last time it ran, measured from its thread: its share of the CPU and the memory this host gives its agents. A job starts when both fit; a CPU-heavy job runs beside a memory-heavy one. Until three runs are learned, a job counts as half the host.">
      {budget && <Row title="Budget" desc={`${budget}${h.slots?.budgetWhy ? ` · ${h.slots.budgetWhy}` : ""}`} />}
      {!lines.length && <Empty text="No heavy job measured yet. A job is measured while it is admitted." />}
      {lines.slice(0, 12).map((l) => (
        <Row
          key={l.class}
          title={l.class}
          mono
          desc={l.cost ? `${l.text} · ${l.cost}` : l.text}
          error={err?.class === l.class ? err.text : null}
          end={
            l.learned && (
              <button type="button" class="btn" disabled={env.readOnly} aria-label={`Reset ${l.class}`} onClick={() => void reset(l.class)}>
                Reset
              </button>
            )
          }
        />
      ))}
    </Group>
  );
}

/** V0.126 (#151): what runs, Update now or Back to release, and the newest updates with their logs. Desktop and phone
 *  share it; the logs open in a new tab on both. */
function UpdatesGroup({ d }: { d: Data }) {
  const env = useSet();
  const [err, setErr] = useState<string | null>(null);
  const [all, setAll] = useState(false);
  const [open, setOpen] = useState<string | null>(null); // V0.update-changelog: the record whose changelog is open
  const v = d.updates!;
  const now = Date.now();
  const list = all ? v.updates : v.updates.slice(0, 8);
  const press = v.paused ? "Back to release" : v.bad?.commit ? "Update now · retry" : "Update now"; // V0.host-updates: the press clears the bad mark
  const src = releaseSource(v); // V0.release-without-gh
  return (
    <Group title="Updates" label="Updates" fold={{ id: `${d.host.name}:updates`, open: !!v.bad?.commit, summary: updateState(v) }} foot={v.bad?.commit ? `Release ${v.bad.commit.slice(0, 7)} is held back here (${v.bad.why}); Fyr waits for a newer one. Update now · retry tries it again.` : undefined}>
      <Row
        title={updateState(v)}
        error={err}
        end={
          <button type="button" class="btn" disabled={env.readOnly} onClick={async () => setErr(await d.runUpdateNow())}>
            {press}
          </button>
        }
      />
      {src && <Row title={src.title} mono desc={src.desc} class={src.bad ? "bad" : undefined} />}
      {!v.updates.length && <Empty text="No updates yet on this host." />}
      {list.map((u) => {
        const r = updateRow(u, now);
        const key = `${u.build}-${u.startedAt}`;
        const ch = changesLabel(u);
        return (
          <Row
            key={key}
            title={r.title}
            desc={
              <>
                {r.desc}
                {ch && (
                  <>
                    {" · "}
                    <button type="button" class="lnk upd-chb" aria-expanded={open === key} onClick={() => setOpen(open === key ? null : key)}>
                      {ch}
                    </button>
                  </>
                )}
                {(u.logs ?? []).map((n) => (
                  <>
                    {" · "}
                    <a href={hostUrl(d.host.id, `/updates/${encodeURIComponent(u.build)}/${n}`) /* from its host */} target="_blank" rel="noopener noreferrer">
                      {n}
                    </a>
                  </>
                ))}
                {open === key && (
                  <span class="upd-ch" role="list" aria-label={`Changes in ${r.title}`}>
                    {changeLines(u, v.repo).map((c) => (
                      <span role="listitem" key={c.url}>
                        <a href={c.url} target="_blank" rel="noopener noreferrer" class={c.sha ? undefined : "upd-older"}>
                          {c.title}
                        </a>
                        {c.prUrl && (
                          <>
                            {" "}
                            <a href={c.prUrl} target="_blank" rel="noopener noreferrer" class="upd-pr">
                              #{c.pr}
                            </a>
                          </>
                        )}
                      </span>
                    ))}
                  </span>
                )}
              </>
            }
            class={r.fail ? "bad" : undefined}
          />
        );
      })}
      {v.updates.length > 8 && (
        <Row end={<button type="button" class="btn" onClick={() => setAll(!all)}>{all ? "Fewer" : `All ${v.updates.length}`}</button>} />
      )}
    </Group>
  );
}

/** V0.173: the account's GitHub REST limit (every caller of the login shares it) and what this host's Fyr spent of it
 *  this hour, by kind. Desktop and phone share it. */
function GitHubGroup({ d }: { d: Data }) {
  const v = d.updates!.github!;
  const s = githubState(v);
  const pause = githubPause(v);
  return (
    <Group title="GitHub API" label="GitHub API" fold={{ id: `${d.host.name}:github`, open: false, force: s.low, summary: s.title }} foot="Every agent, every host's Fyr and your own gh share this limit. Fyr reads it every 10 minutes; a call answered 304 costs nothing.">
      <Row title={s.title} desc={s.desc} class={s.low ? "bad" : undefined} />
      {pause && <Row title={pause.title} desc={pause.desc} />}
      {githubRows(v).map((r) => (
        <Row key={r.title} title={r.title} desc={r.desc} mono />
      ))}
    </Group>
  );
}

/** V0.services-not-jobs: the commands this host treats as services, edited like the heavy commands; desktop and phone
 *  share it. */
function ServiceGroup({ h, host, run, readOnly }: { h: HostLoad; host: string; run(action: string, args: Record<string, unknown>): Promise<string | null>; readOnly: boolean }) {
  const [err, setErr] = useState<{ key: string; text: string } | null>(null);
  const act = async (key: string, args: Record<string, unknown>) => {
    const e = await run("host.servicePatterns", args);
    setErr(e ? { key, text: e } : null);
    return e;
  };
  const list = h.servicePatterns ?? [];
  return (
    <Group label="Services" title="Services" fold={{ id: `${host}:services`, open: false, summary: `${list.length} ${list.length === 1 ? "pattern" : "patterns"}` }} foot="Commands that keep running for the work beside them: dev stacks, dev servers, compose projects. Fyr reserves the memory a service needed at its peak while it runs, counts the containers a thread starts as its own, and never pauses a service. Patterns are regular expressions matched against the command.">
      {list.map((p) => (
        <Row
          key={p.pattern}
          title={p.pattern}
          mono
          desc={p.builtIn ? "built in" : "added"}
          error={err?.key === `s:${p.pattern}` ? err.text : null}
          end={
            <button type="button" class="btn" disabled={readOnly} onClick={() => void act(`s:${p.pattern}`, { remove: p.pattern })}>
              Remove
            </button>
          }
        />
      ))}
      <Row error={err?.key === "s:add" ? err.text : null} class="fieldrow">
        <SaveField value="" label="Service pattern to add" placeholder="rails\s+server" mono clears alwaysButton button="Add" onError={(e) => setErr(e ? { key: "s:add", text: e } : null)} onSave={(v) => act("s:add", { add: v.trim() })} />
      </Row>
    </Group>
  );
}

function RepoAdd({ run }: { run(repo: string, on: boolean): Promise<string | null> }) {
  const [err, setErr] = useState<string | null>(null);
  return (
    <Row error={err} class="fieldrow">
      <SaveField value="" label="Repo to add" placeholder="owner/repo" mono clears alwaysButton button="Add" onError={setErr} check={(v) => (repoKey(v) ? null : "Write it as owner/repo.")} onSave={(v) => run(repoKey(v), true)} />
    </Row>
  );
}

