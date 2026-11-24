// The sections of a host's page that the host itself reports and tunes: Updates, Load, Diagnostics (V0.settings-ia;
// Automation keeps only what Fyr does without a press). settings-hosts.tsx lays them
// out and states the one folding rule. The same sections on the desktop and the phone.

import { type SettingsProps } from "./settings-ui.tsx";
import { useEffect, useState } from "preact/hooks";
import type { HostId } from "./hostid.ts";
import { hostUrl } from "./urls.ts";
import { updateAction, updateRows, updating, type HostInfo } from "./hosts.ts";
import { budgetLine, jobLines, type HostLoad } from "./host-load.ts";
import { useUnd } from "./undeclared-ctx.tsx";
import { UndIcon } from "./undeclared-ctx.tsx";
import { GuardChart } from "./guard-ui.tsx";
import { guardLog, guardSummary, rerunRows } from "./guard.ts";
import { clockTime } from "./format.ts";
import { leftoverRows, tempRows } from "./leftovers.ts";
import { invariantRows } from "./invariants.ts";
import { addedRows, parseThreshold, thresholdError, thresholdRows, thresholdSummary, undeclaredRows } from "./undeclared.ts";
import { refreshLoad, useLoad } from "./load-ui.tsx";
import { changeLines, changesLabel, githubPause, githubRows, githubState, releaseSource, updateRow, updateState } from "./updates.ts";
import { type PageRef } from "./settings-nav.ts";
import { ConfirmButton, Empty, Group, Row, SaveField, Section, Skeleton, SubCaption, Switch, useSet } from "./settings-rows.tsx";
import { type Data } from "./settings-load.ts";

/** V0.111: the Auto-update switch. It exists once, on Automation (what Fyr does without a press); a host's Updates
 *  section shows its state as a line that leads there (AutoUpdateLine). */
export function AutoUpdateRow({ d }: { d: Data }) {
  const [err, setErr] = useState<string | null>(null);
  if (d.deploy === null)
    return (
      <Row
        title="Auto-update Fyr"
        desc={d.deployErr ? undefined : "Reading the setting…"}
        error={d.deployErr}
        end={
          d.deployErr ? (
            <button type="button" class="btn" onClick={() => void d.loadDeploy()}>
              Try again
            </button>
          ) : undefined
        }
      />
    );
  return (
    <Row
      label
      class="autoupd"
      title="Auto-update Fyr"
      desc="Fyr installs each signed release by itself, and rolls back one that is not healthy."
      error={err}
      end={<Switch on={d.deploy} label="Auto-update Fyr" onChange={async (on) => setErr(await d.runDeploy(on))} />}
    />
  );
}

/** The state of the Auto-update switch on a host's page: one line that opens Automation on this host's tab, where the
 *  switch is. Not a second switch: two controls for one setting had two descriptions to keep true. */
function AutoUpdateLine({ d, go }: { d: Data; go(r: PageRef): void }) {
  const state = d.deploy === null ? (d.deployErr ? "Could not read the setting." : "Reading the setting…") : d.deploy ? "On: Fyr installs each signed release by itself." : "Off: a release waits for your press here.";
  return <Row class="autoline" onClick={() => go({ page: "automation", ...(d.host.name ? { host: d.host.name } : {}) })} title="Auto-update" desc={`${state} The switch is on Automation.`} end={<span class="chev">›</span>} />;
}

/** V0.settings-ia: the Updates section of a host's page. First the one row that asks for a press, with why and the
 *  button (hosts.ts updateAction: something to install, to try again or to go back to; a counted one carries the amber
 *  chip); then what runs, the newest release and the last update; then the Auto-update line. The history follows in
 *  its own folded group, open while a release is held back. An offline host shows the same rows without the chip and
 *  the button: nothing can be pressed there, and it is not counted. */
export function HostUpdates({ h, d, hasDeploy, api, now, focus, go }: { h: HostInfo; d: Data | undefined; hasDeploy: boolean; api: SettingsProps["api"]; now: number; focus?: string; go(r: PageRef): void }) {
  const env = useSet();
  const hostRun = useHostCommand(h.id, api);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const u = h.update;
  const v = d?.updates ?? null;
  if (!u && !v && !(hasDeploy && d)) return null;
  const act = updateAction(h, v, now);
  const press = async () => {
    setBusy(true);
    setErr(d ? await d.runUpdateNow() : await hostRun("host.updateNow", {}));
    setBusy(false);
  };
  const state = v ? updateState(v) : "";
  return (
    <Section title="Updates" id="updates">
      <Group label="Updates" class="updg">
        {act && (
          <Row
            class={`updnow${act.chip ? " bad" : ""}`}
            acts
            title={act.title}
            desc={h.online ? act.desc : `${act.desc} Nothing can be pressed while ${h.name} is offline.`}
            error={err}
            end={
              h.online ? (
                <>
                  {act.chip && <span class="pst needs-login">{act.chip}</span>}
                  <button type="button" class="btn" disabled={env.readOnly || busy || updating(h)} onClick={() => void press()}>
                    {act.button}
                  </button>
                </>
              ) : undefined
            }
          />
        )}
        {u ? updateRows(u, now).map((r) => <Row key={r.title} title={r.title} desc={r.desc} class={r.bad ? "bad" : undefined} />) : state && <Row title={state} />}
        {hasDeploy && d && <AutoUpdateLine d={d} go={go} />}
      </Group>
      {d && v && <UpdateHistory d={d} force={focus === "updates"} held={!!act?.chip} />}
    </Section>
  );
}

/** A host's load reading for its page: its own by name. A front with one host has one reading whatever its name; with
 *  several hosts a host without a reading shows none (it showed the other host's, and End went to the wrong one). */
export function useHostLoad(host: string, single: boolean): HostLoad | undefined {
  const list = useLoad();
  return list.find((x) => x.host === host) ?? (single && list.length === 1 ? list[0] : undefined);
}

/** Whether a host's Load section has anything to show. */
export function hasLoad(h: HostLoad | undefined): boolean {
  return !!h && (!!h.provider || !!h.leftovers?.length || !!h.temps?.length);
}

/** Decision 0029 S1 (#185), V0.164, V0.209, V0.settings-ia: the Load section of a host's page, under two captions.
 *  Memory now is its first row (what the guard did is in Diagnostics). "Running now": what threads left running, the
 *  heavy work Fyr did not expect, the worktrees nobody uses. "Rules": what counts as heavy, what may be killed and
 *  rerun, the service commands, the commands Fyr learned and the jobs it measured: everything that tunes how much the
 *  host's agents may use is under that one caption. `reading`: the load was not read yet, so the section keeps its
 *  place with one still row. `part` keys the folds (the host's name; "" for the one part of a front with one host). */
export function HostLoadSection({ h, reading, part, id, api, focus, jump }: { h: HostLoad | undefined; reading: boolean; part: string; id?: HostId; api: SettingsProps["api"]; focus?: string; jump(section: "diagnostics"): void }) {
  const env = useSet();
  const und = useUnd();
  if (!h || !hasLoad(h))
    return reading ? (
      <Section title="Load" id="load">
        <Skeleton rows={1} label="Reading the load" />
      </Section>
    ) : null;
  const guard = h.slots?.guard;
  const rules = !!h.provider;
  return (
    <Section title="Load" id="load">
      {guard && (
        <Group label="Memory now">
          <Row class={`memnow${guard.short ? " bad" : ""}`} onClick={() => jump("diagnostics")} title="Memory now" desc={`${guardSummary(guard, und.now())}. What the guard did is in Diagnostics.`} end={<span class="chev">›</span>} />
        </Group>
      )}
      {(!!h.leftovers?.length || !!h.temps?.length || (rules && undeclaredRows(h, und.now(), und.lookup).length > 0)) && <SubCaption>Running now</SubCaption>}
      <LeftoverGroups h={h} host={part} id={id} api={api} focus={focus} />
      {rules && <UnexpectedGroup h={h} host={part} />}
      <WorktreeGroup h={h} host={part} id={id} api={api} />
      {rules && <SubCaption>Rules</SubCaption>}
      {rules && <RuleGroups h={h} host={part} id={id} api={api} />}
      {rules && <MeasuredJobs h={h} host={part} id={id} api={api} env={env} />}
    </Section>
  );
}

/** What a host's Diagnostics say in one line, and whether something is wrong now: memory short, an invariant broken,
 *  the GitHub limit low, a release key that cannot be read. */
export function diagnosticsLine(h: HostLoad | undefined, d: Data | undefined, now: number): { summary: string; bad: boolean; any: boolean } {
  const guard = h?.slots?.guard;
  const inv = h ? [...invariantRows(h, now, "load"), ...invariantRows(h, now, "thread")] : [];
  const gh = d?.updates?.github ? githubState(d.updates.github) : null;
  const src = d?.updates ? releaseSource(d.updates) : null;
  const broken = inv.filter((r) => r.desc !== "never broken").length;
  const summary = [guard ? guardSummary(guard, now) : "", inv.length ? (broken ? `${broken} of ${inv.length} invariants broken` : `${inv.length} invariants held`) : "", gh?.title ?? "", src?.bad ? "the release key cannot be read" : ""].filter(Boolean).join(" · ") || "Release source and key";
  return { summary, bad: !!guard?.short || broken > 0 || !!gh?.low || !!src?.bad, any: !!guard || inv.length > 0 || !!gh || !!src };
}

/** V0.settings-ia: the Diagnostics section of a host's page: what Fyr measured and checked, with nothing to set:
 *  the memory guard, the invariants, the GitHub limit, the release source and its key. Folded to one line while all
 *  is well; while something is wrong the line is in the error colour and the section is open. Its groups show whole
 *  (no second fold). Absent when the host reports none of it. */
export function HostDiagnostics({ h, part, d, focus }: { h: HostLoad | undefined; part: string; d: Data | undefined; focus?: string }) {
  const und = useUnd();
  const s = diagnosticsLine(h, d, und.now());
  if (!s.any) return null;
  const src = d?.updates ? releaseSource(d.updates) : null; // V0.release-without-gh
  return (
    <Section title="Diagnostics" id="diagnostics" fold={{ id: `${part}:diagnostics`, open: false, force: s.bad || focus === "diagnostics", bad: s.bad, summary: s.summary }}>
      {h && h.slots?.guard && <MemoryGuardGroup h={h} host={part} />}
      {h && <InvariantGroup h={h} host={part} />}
      {d?.updates?.github && <GitHubGroup d={d} />}
      {src && (
        <Group title="Release source" label="Release source" foot="Where this host's updates come from, and the key they are verified with.">
          <Row title={src.title} mono desc={src.desc} class={src.bad ? "bad" : undefined} />
        </Group>
      )}
    </Section>
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
        <Group label="Load invariants" title="Load invariants" fold={{ id: `${host}:invariants`, open: false, summary: loadBroken ? `${loadBroken} of ${loadRows.length} broken` : `${loadRows.length} held` }} foot="Fyr checks these on each tick of the memory guard. A broken one writes an invariant line to Fyr's log and, at most once a day, a card in Needs you with the evidence; its Fix it starts a thread in this host's Fyr checkout. Fyr never starts the fix by itself.">
          {loadRows.map((r) => (
            <Row key={r.id} class="invrow" title={r.id} mono desc={`${r.text} ${r.desc}.`} />
          ))}
        </Group>
      )}
      {threadRows.length > 0 && (
        <Group label="Thread invariants" title="Thread invariants" fold={{ id: `${host}:thread-invariants`, open: false, summary: threadBroken ? `${threadBroken} of ${threadRows.length} broken` : `${threadRows.length} held` }} foot="Fyr checks these on every thread transition, on host restart, and on each tick. A broken one writes an invariant line to Fyr's log and, at most once a day, a card in Needs you with the evidence and recent transitions; its Fix it starts a thread in this host's Fyr checkout.">
          {threadRows.map((r) => (
            <Row key={r.id} class="invrow" title={r.id} mono desc={`${r.text} ${r.desc}.`} />
          ))}
        </Group>
      )}
    </>
  );
}

/** V0.176: the processes threads left running on this host (End now, Keep for a day). Absent when there are none. The
 *  rail's count lands here (`hosts/<host>/leftovers`). Desktop and phone share it; on the phone the row's buttons are
 *  on a line of their own (Row `acts`). */
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
  // V0.host-tabs, V0.settings-ia: the rail's count opens hosts/<host>/leftovers; the group is there once this host's load is read.
  useEffect(() => {
    if (focus === "leftovers") document.querySelector(".lftg")?.scrollIntoView({ block: "start" });
  }, [focus, rows.length]);
  if (!rows.length) return null;
  return (
    <Group class="lftg" label="Leftover processes" title="Leftover processes" fold={{ id: `${host}:leftovers`, open: true, force: focus === "leftovers", summary: `${rows.length} running` }} foot="What threads left running outside their agent: a browser, a server, a port-forward." how="Fyr ends a thread's leftovers 30 min after its run ends and all of them when it settles; one the agent kept, or one under timeout, stays until then. End ends one now; Keep holds it for a day, through a settle too. Each end leaves a line on its thread.">
      {rows.map((r) => (
        <Row
          key={r.id}
          class="lftrow"
          acts
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
              {/* ends running work, which nothing brings back: it asks in place (settings-rows.tsx ConfirmButton) */}
              <ConfirmButton label="End" disabled={env.readOnly} onConfirm={() => void act(`l:${r.id}`, "host.leftoverEnd", { id: r.id })} />
            </>
          }
        />
      ))}
    </Group>
  );
}

/** V0.176, V0.worktree-sweep: the worktrees nobody works in (Remove when pushed and clean; Fyr removes them by itself).
 *  Status with one optional press, so it is folded. */
function WorktreeGroup({ h, host, id, api }: { h: HostLoad; host: string; id?: HostId; api: SettingsProps["api"] }) {
  const env = useSet();
  const und = useUnd();
  const run = useHostCommand(id, api);
  const [err, setErr] = useState<{ key: string; text: string } | null>(null);
  const temps = tempRows(h, und.lookup, und.now());
  if (!temps.length) return null;
  return (
    <Group label="Unused worktrees" title="Unused worktrees" fold={{ id: `${host}:worktrees`, open: false, summary: `${temps.length} · Fyr removes them by itself` }} foot="Worktrees no thread works in. Fyr removes these by itself; Remove does it now, and Fyr makes the worktree again when a thread works there." how="Fyr removes a worktree a day after its last commit or checkout when nothing in it is uncommitted and its commit is on the remote (a branch or a pull request); the branch stays. One with uncommitted or unpushed work, or locked with git worktree lock, stays.">
      {temps.map((t) => (
        <Row
          key={t.path}
          title={t.title}
          mono
          desc={t.desc}
          error={err?.key === t.path ? err.text : null}
          end={
            t.canRemove ? (
              <button
                type="button"
                class="btn"
                disabled={env.readOnly}
                onClick={async () => {
                  const e = await run("host.tempRemove", { path: t.path });
                  setErr(e ? { key: t.path, text: e } : null);
                }}
              >
                Remove
              </button>
            ) : undefined
          }
        />
      ))}
    </Group>
  );
}

/** A command to the host, with the Saved mark and a fresh read of the load. A host with no install id cannot be named. */
export function useHostCommand(host: HostId | undefined, api: SettingsProps["api"]): (action: string, args: Record<string, unknown>) => Promise<string | null> {
  const env = useSet();
  return async (action, args) => {
    if (!host) return "This host reports no id, so nothing can be sent to it until it updates";
    const r = await api.command(host, action, undefined, args);
    if (r.ok) (env.saved(), refreshLoad());
    return r.ok ? null : (r.error ?? "That did not work");
  };
}

/** V0.164 (SPEC 8.5): the threads that run heavy work Fyr did not expect (it was not declared as a heavy job). Status:
 *  there is nothing to press but Open, so it is folded and its header says how many. Absent when there are none. */
function UnexpectedGroup({ h, host }: { h: HostLoad; host: string }) {
  const und = useUnd();
  const rows = undeclaredRows(h, und.now(), und.lookup);
  if (!rows.length) return null;
  return (
    <Group label="Heavy work Fyr did not expect" title="Heavy work Fyr did not expect" fold={{ id: `${host}:undeclared`, open: false, summary: `${rows.length} ${rows.length === 1 ? "thread" : "threads"}` }} foot="Threads that use clearly more than their idle level in work Fyr did not recognise as a heavy job. There is nothing to do." how="Fyr counts the use against this host's heavy jobs, so admitted jobs wait instead of overloading it, and learns a command it sees again. Only when memory is short does Fyr pause the newest one; nothing is killed.">
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
  );
}

/** V0.164, V0.209, V0.services-not-jobs: the rules of a host's load, each folded with its state in the header: what
 *  counts as heavy (the thresholds), what may be killed and rerun, the service commands, and the commands Fyr
 *  learned. A group is absent on a host that does not send it. */
function RuleGroups({ h, host, id, api }: { h: HostLoad; host: string; id?: HostId; api: SettingsProps["api"] }) {
  const env = useSet();
  const run = useHostCommand(id, api);
  const [err, setErr] = useState<{ key: string; text: string } | null>(null);
  const fail = (key: string) => (text: string | null) => setErr(text ? { key, text } : null);
  const act = async (key: string, action: string, args: Record<string, unknown>) => fail(key)(await run(action, args));
  const added = addedRows(h);
  const th = thresholdRows(h);
  return (
    <>
      {th && (
        <Group label="What counts as heavy" title="What counts as heavy" fold={{ id: `${host}:thresholds`, open: false, summary: thresholdSummary(th) }} foot="A thread counts as heavy when it uses this much above its idle level for this long. Its idle level is what its agent uses between jobs (the CLI and the servers it keeps running), learned per thread.">
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
      {h.rerun && (() => {
        const reruns = rerunRows(h, clockTime);
        return (
          <Group
            label="Kill and rerun"
            title="Kill and rerun"
            fold={{ id: `${host}:rerun`, open: false, summary: reruns.length ? `${reruns.length} ${reruns.length === 1 ? "command" : "commands"}` : "none" }}
            foot="Commands Fyr may kill and run again when memory is short. Add only what is safe to run twice, such as tests, lint or a type check, never a dev server."
            how="Fyr kills a job of one of these instead of freezing it when it was frozen for 2 minutes and could not resume, or when it usually ends within 5 minutes. The agent is told to run it again, and Fyr admits the rerun once memory has recovered and the job fits. A command killed 3 times in an hour is frozen instead for the rest of that hour. Nothing is here unless you add it."
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
      {h.servicePatterns && <ServiceGroup h={h} host={host} run={run} readOnly={env.readOnly} />}
      {added.length > 0 && (
        <Group label="Learned commands" title="Learned commands" fold={{ id: `${host}:learned`, open: false, summary: `${added.length} ${added.length === 1 ? "command" : "commands"}` }} foot="Heavy commands Fyr learned, besides the built-in ones (make test, go test ./..., playwright, nix build and the like). Fyr adds a command here after seeing it run heavy three times, and admits it when there is room. Remove one and Fyr does not learn it again.">
          {added.map((a) => (
            <Row
              key={a.pattern}
              title={a.pattern}
              mono
              desc={a.line}
              error={err?.key === `a:${a.pattern}` ? err.text : null}
              end={
                // nothing adds a learned command back, and Fyr does not learn it again: it asks in place
                <ConfirmButton label="Remove" disabled={env.readOnly} onConfirm={() => void act(`a:${a.pattern}`, "host.heavyRemove", { pattern: a.pattern })} />
              }
            />
          ))}
        </Group>
      )}
    </>
  );
}

/** Decision 0032, V0.188: what Fyr did when this host's memory ran short. In Diagnostics (V0.settings-ia): there is
 *  nothing to set. */
function MemoryGuardGroup({ h, host }: { h: HostLoad; host: string }) {
  const und = useUnd();
  const now = und.now();
  const guard = h.slots!.guard!;
  return (
    <Group
      label="Memory guard"
      title="Memory guard"
      fold={{ id: `${host}:guard`, open: false, summary: guardSummary(guard, now) }}
      foot="What Fyr did when this host's memory ran short."
      how="The shaded band is the time memory was short, a hatched stretch is work Fyr held frozen (never killed), and the number is how many times in a row it froze that thread. Frozen work runs again once memory has been fine for 30 s; one long episode is paused at most 8 times, 60 s apart."
    >
      <Row title="Now" desc={guardSummary(guard, now)} />
      <div class="gwrap">
        <GuardChart guard={guard} now={now} titleOf={(t) => und.lookup(t).title} />
      </div>
      {guardLog(guard, (t) => und.lookup(t).title, clockTime).map((l, i) => (
        <Row key={i} title={l.text} desc={l.at} />
      ))}
    </Group>
  );
}

/** Decision 0029 S1 (#185): the heavy jobs this host measured, per class (project and command), the newest first. */
function MeasuredJobs({ h, host, id, api, env }: { h: HostLoad; host: string; id?: HostId; api: SettingsProps["api"]; env: { readOnly: boolean } }) {
  const [err, setErr] = useState<{ class: string; text: string } | null>(null);
  const lines = jobLines(h);
  const budget = budgetLine(h); // V0.163: the budget as percentages of the host
  // Decision 0029 S3: Forget drops a class's learned cost on this host (owner only); it counts as 1.0 again.
  const reset = async (c: string) => {
    if (!id) return setErr({ class: c, text: "This host reports no id" });
    const r = await api.command(id, "host.jobReset", undefined, { class: c });
    setErr(r.ok ? null : { class: c, text: r.error ?? "Not reset" });
    if (r.ok) refreshLoad();
  };
  return (
    <Group title="Measured jobs" label="Measured jobs" fold={{ id: `${host}:jobs`, open: false, summary: budget ? `Budget: ${budget} · ${lines.length} measured` : `${lines.length} measured` }} foot="What each kind of heavy job used the last time it ran, measured from its thread." how="The figures are a job's share of the CPU and the memory this host gives its agents. A job starts when both fit; a CPU-heavy job runs beside a memory-heavy one. Until three runs are learned, a job counts as half the host. Forget drops what was learned of one, and it counts as new again.">
      {/* The budget is not a setting: Fyr moves it by itself (decision 0029 S4) and the host says why. The row says so, so nobody looks for a field. */}
      {budget && <Row title="Budget" desc={`${budget}${h.slots?.budgetWhy ? ` · ${h.slots.budgetWhy}` : ""}. Not a setting: Fyr adjusts it by itself from what it measures on this host.`} />}
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
              <button type="button" class="btn" disabled={env.readOnly} aria-label={`Forget ${l.class}`} onClick={() => void reset(l.class)}>
                Forget
              </button>
            )
          }
        />
      ))}
    </Group>
  );
}

/** V0.126 (#151): the newest updates of this host with their logs and changes. Status only, so it is folded; its
 *  header says the last one. Desktop and phone share it; the logs open in a new tab on both. */
function UpdateHistory({ d, force, held }: { d: Data; force?: boolean; /** a release is held back or the last update failed (hosts.ts updateTrouble): the history is what explains it, so it is open */ held: boolean }) {
  const [all, setAll] = useState(false);
  const [open, setOpen] = useState<string | null>(null); // V0.update-changelog: the record whose changelog is open
  const v = d.updates!;
  const now = Date.now();
  const list = all ? v.updates : v.updates.slice(0, 8);
  const last = v.updates[0] ? updateRow(v.updates[0], now) : null;
  return (
    <Group title="Update history" label="Update history" fold={{ id: `${d.host.name}:updates`, open: held, force, summary: last ? `${last.title} · ${last.desc}` : "No updates yet" }}>
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
                    <a class="lnk" href={hostUrl(d.host.id, `/updates/${encodeURIComponent(u.build)}/${n}`) /* from its host */} target="_blank" rel="noopener noreferrer">
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
    <Group label="Service commands" title="Service commands" fold={{ id: `${host}:services`, open: false, summary: `${list.length} ${list.length === 1 ? "pattern" : "patterns"}` }} foot="Commands that keep running for the work beside them: dev stacks, dev servers, compose projects." how="Fyr reserves the memory a service needed at its peak while it runs, counts the containers a thread starts as its own, and never pauses a service. Patterns are regular expressions matched against the command.">
      {list.map((p) => (
        <Row
          key={p.pattern}
          title={p.pattern}
          mono
          desc={p.builtIn ? "built in" : "added"}
          error={err?.key === `s:${p.pattern}` ? err.text : null}
          end={
            p.builtIn ? (
              // a built-in pattern does not come back as built in: it asks in place. One you added goes at once: the field below adds it again.
              <ConfirmButton label="Remove" disabled={readOnly} onConfirm={() => void act(`s:${p.pattern}`, { remove: p.pattern })} />
            ) : (
              <button type="button" class="btn" disabled={readOnly} onClick={() => void act(`s:${p.pattern}`, { remove: p.pattern })}>
                Remove
              </button>
            )
          }
        />
      ))}
      <Row error={err?.key === "s:add" ? err.text : null} class="fieldrow">
        <SaveField value="" label="Service pattern to add" placeholder="rails\s+server" mono clears alwaysButton button="Add" onError={(e) => setErr(e ? { key: "s:add", text: e } : null)} onSave={(v) => act("s:add", { add: v.trim() })} />
      </Row>
    </Group>
  );
}

