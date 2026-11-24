// The Automation settings page. V0.settings-ia: only what Fyr does without a press: Auto-update, Auto-merge, and where
// the per-project switches are. What a host reports and tunes (updates, load, diagnostics) is on that host's page
// (settings-host-groups.tsx, shown by settings-hosts.tsx). The same page on the desktop and the phone.

import { useState } from "preact/hooks";
import { repoKey, sortRepos } from "./automerge.ts";
import { updateTrouble, type HostInfo } from "./hosts.ts";
import { updateState } from "./updates.ts";
import { type PageRef } from "./settings-nav.ts";
import { Empty, Group, LoadError, Row, SaveField, Skeleton, Switch } from "./settings-rows.tsx";
import { AutoUpdateRow } from "./settings-host-groups.tsx";
import { type Data } from "./settings-load.ts";

/** V0.111 Auto-update and V0.102 Auto-merge: what Fyr does without the owner's press. Only the owner changes them here;
 *  no agent tool can. Each has one name, in its group, its switch, the nav's line and its errors. The Auto-update
 *  switch is here only (a host's Updates section shows its state and leads here). `row` is the host's row in the host
 *  list, for the one reading of a held-back release (hosts.ts updateTrouble). */
export function AutomationPage({ d, row, hasDeploy, go }: { d: Data; row: HostInfo | undefined; hasDeploy: boolean; go(r: PageRef): void }) {
  const [repoErr, setRepoErr] = useState<{ repo: string; text: string } | null>(null);
  const [q, setQ] = useState("");
  const repos = sortRepos(d.repos ?? []);
  const shown = q.trim() ? repos.filter((r) => r.repo.includes(q.trim().toLowerCase())) : repos;
  const state = d.updates ? updateState(d.updates) : "";
  const trouble = updateTrouble(row, d.updates);
  const held = trouble?.held?.commit.slice(0, 7);
  const what = held ? `Release ${held} is held back` : trouble ? "The last update failed" : "";
  const on = d.host.label;
  return (
    <>
      {hasDeploy && (
        <Group title="Auto-update" label="Auto-update" how="A release is installed once it has been the newest for 10 minutes. A local build always waits for your press.">
          <AutoUpdateRow d={d} />
          {/* The one line of state; the press (Update now, Try again), the history and the logs are on the host's
              page, where the amber chip of a counted update is. Here the row is red and leads there. */}
          {on && (state || what) && (
            <Row
              class={trouble && d.host.online ? "updstate bad" : "updstate"}
              onClick={() => go({ page: "hosts", sub: on, focus: "updates" })}
              title={what || state}
              desc={`${what && state ? `${state} ` : ""}${trouble ? (held ? "Try again" : "Update now") : "Update now"}, the history and the logs are on ${on}'s page.`}
              end={<span class="chev">›</span>}
            />
          )}
        </Group>
      )}
      {d.repos === null && d.reposErr ? (
        <LoadError text={d.reposErr} onRetry={() => void d.loadRepos()} />
      ) : d.repos === null ? (
        <Skeleton rows={2} />
      ) : (
        // The scope, in one line: each host keeps its own list, and a thread's merge ask is judged by the host the
        // thread runs on (internal/core/automerge.go; the engine of that host holds the setting).
        <Group
          title="Auto-merge"
          label="Auto-merge"
          foot={
            d.host.name
              ? `For threads that run on ${on}: in a repo that is on, an agent's merge ask merges once the checks pass on the head it asked for. A thread on another host follows that host's list.`
              : "In a repo that is on, an agent's merge ask merges once the checks pass on the head it asked for."
          }
        >
          {repos.length > 8 && (
            <div class="sr srch">
              <input class="pq" type="search" placeholder={`Search ${repos.length} repos`} aria-label="Search repos" value={q} onInput={(e) => setQ((e.target as HTMLInputElement).value)} />
            </div>
          )}
          {!repos.length && <Empty text={d.host.name ? `No repos on ${on} yet: no thread there has a pull request. A repo shows here once one does, or add it below.` : "No repos yet. A repo shows here once a thread has a pull request in it, or add it below."} />}
          {shown.map((r) => (
            <Row key={r.repo} label title={r.repo} mono error={repoErr?.repo === r.repo ? repoErr.text : null} end={<AutoMergeSwitch repo={r.repo} on={r.on} run={d.runRepo} onError={(e) => setRepoErr(e ? { repo: r.repo, text: e } : null)} />} />
          ))}
          <RepoAdd run={d.runRepo} />
        </Group>
      )}
      {/* V0.settings-ia: the two switches that are per project are named here, so this page lists everything Fyr does
          without a press. */}
      <Group title="Per project" label="Per project">
        <Row class="perproj" onClick={() => go({ page: "projects" })} title="Agent says done, Tool approvals" desc="Whether Fyr settles a thread and judges tool calls without asking is set on each project's page, under Without asking." end={<span class="chev">›</span>} />
      </Group>
    </>
  );
}


function AutoMergeSwitch({ repo, on, run, onError }: { repo: string; on: boolean; run(repo: string, on: boolean): Promise<string | null>; onError(e: string | null): void }) {
  return <Switch on={on} label={`Auto-merge ${repo}`} onChange={async (v) => onError(await run(repo, v))} />;
}

function RepoAdd({ run }: { run(repo: string, on: boolean): Promise<string | null> }) {
  const [err, setErr] = useState<string | null>(null);
  return (
    <Row error={err} class="fieldrow">
      <SaveField value="" label="Repo to add" placeholder="owner/repo" mono clears alwaysButton button="Add" onError={setErr} check={(v) => (repoKey(v) ? null : "Write it as owner/repo.")} onSave={(v) => run(repoKey(v), true)} />
    </Row>
  );
}
