// The inbox rows (V0.91 shape), shared by the desktop list and the phone list so the two stay the same.
// Each front adds only its wrapper (selection, swipes); the phone sets its own sizes in phone.css.

import type { ComponentChildren } from "preact";
import type { Change, ChangeNeed, Thread } from "./types.ts";
import { useApi } from "./api-context.ts";
import { useAsk } from "./confirm-ui.tsx";
import { press } from "./confirm.ts";
import { ThreadCPU, ThreadJob, ThreadSlot, useThreadJob } from "./load-ui.tsx";
import { RowUndeclared, useRowUndeclared } from "./undeclared-ui.tsx";
import { RowProject, RowProjectName, StartedBy } from "./project-ui.tsx";
import { useHosts } from "./host-pick-ui.tsx";
import { HostMark } from "./host-mark-ui.tsx";
import { hostOf, multiHost } from "./hosts.ts";
import { StateIcon, stateClass } from "./icons.tsx";
import { rowMeta, rowState } from "./rows.ts";
import { relTime, whenLabel } from "./format.ts";
import { snoozeLine } from "./snooze.ts";
import { prChip } from "./pr.ts";
import { changeChip, needLook, rowNeed } from "./offers.ts";
import { groupTasks, workingLine } from "./tasks.ts";

/** What differs between the fronts, each for a reason. */
export interface RowLook {
  /** the phone has no keyboard, so its rows name no keys */
  keys: boolean;
  /** a tap on the phone opens the row, so "started by" is not a link there */
  link: boolean;
  /** the project icon before the title: the phone's title is 15 px, so its icon is 16 (V0.135) */
  icon: number;
}

export const DESKTOP_LOOK: RowLook = { keys: true, link: true, icon: 14 };
export const PHONE_LOOK: RowLook = { keys: false, link: false, icon: 16 };

interface BodyProps {
  t: Thread;
  now: number;
  look: RowLook;
  /** extra classes on the row, with a leading space (" sel picked") */
  extra?: string;
  /** replaces the state icon: the phone's selection box */
  lead?: ComponentChildren;
  /** before the started-by and the detail: the host on the phone */
  pre?: ComponentChildren;
  /** the desktop list's handlers and listbox attributes */
  attrs?: Record<string, unknown>;
}

export function PrBadge({ t, short }: { t: Thread; short?: boolean }) {
  const c = changeChip(t, short); // V0.59 (8): a change that is not a PR, when no PR is open
  if (c)
    return (
      <span class={`pr chgchip ${c.cls}`} title={c.title}>
        {c.text}
      </span>
    );
  if (!t.pr) return <span class="pr none" aria-label="no pull request" />;
  const l = prChip(t.pr, short);
  return (
    <span class={`pr ${l.cls}`} title={l.title}>
      {l.text}
    </span>
  );
}

/** A row has one shape in every section (V0.91, V0.135): the project icon and the title, which wraps to two lines before
 * it is cut and always keeps two lines of room (no jump while a title streams in), with the age at the end; then what
 * the thread does, after the project's name (only without an image icon or a filter) and a quiet ↳ for a thread
 * another started; its chips (PR or change, subagents) after that, only when it has one. */
/** V0.104: the pin mark (Lucide "pin", ISC). */
export const PIN_ICON = (
  <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d="M12 17v5" />
    <path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z" />
  </svg>
);

function RowHead({ t, look }: { t: Thread; look: RowLook }) {
  // V0.170: with more than one host, the host's mark comes first, then the project's icon (circles are hosts, squares
  // are projects); --rh moves the icon, the pin and the title's indent past it.
  const { hosts } = useHosts();
  const host = multiHost(hosts) ? hostOf(t, hosts) : undefined;
  return (
    // The icons and the pin sit over the title's first-line indent (app.css): inside the clamped box an element would
    // become a block of its own (-webkit-box), and Safari clamps only plain text.
    <span class={`hd${t.pinnedAt ? " pinned" : ""}`} style={{ "--ri": `${look.icon}px`, ...(host ? { "--rh": `${look.icon + 4}px` } : {}) }}>
      {host && (
        <span class="rhm">
          <HostMark name={host} size={look.icon} label />
        </span>
      )}
      <RowProject t={t} size={look.icon} />
      {t.pinnedAt && (
        <span class="pin" title="Pinned" aria-label="Pinned" role="img">
          {PIN_ICON}
        </span>
      )}
      <span class="t" title={t.title}>
        {t.title || "Untitled"}
      </span>
    </span>
  );
}

/** V0.135: the start of a row's second line: the project's name when the icon does not say it, and ↳ for a thread
 *  another thread started. */
function RowFrom({ t, look, more }: { t: Thread; look: RowLook; more: boolean }) {
  return (
    <>
      <RowProjectName t={t} sep={!!t.parent || more} />
      {t.parent && <StartedBy parentId={t.parent} link={look.link} mark />}
    </>
  );
}

function hasChange(t: Thread): boolean {
  return !!(t.pr || changeChip(t));
}

export function NeedsRowBody({ t, now, look, extra = "", lead, pre, attrs }: BodyProps) {
  const st = t.attention.state;
  const need = rowNeed(t); // V0.101: the host's cause, read only
  return (
    <div {...attrs} class={`rw${st === "done" ? " dn" : ""}${extra}`}>
      {lead ?? (
        <span class="tile">
          <StateIcon state={st} />
        </span>
      )}
      <RowHead t={t} look={look} />
      <span class="age">{relTime(t.activityAt ?? t.updatedAt, now)}</span>
      <span class="m ell">
        <b class={stateClass(st)}>{rowState(t)}</b> · {pre}
        <RowFrom t={t} look={look} more />
        {rowMeta(t, { keys: look.keys })}
      </span>
      {(hasChange(t) || need) && (
        <span class="cl">
          {hasChange(t) && <PrBadge t={t} />}
          {need && <NeedButton t={t} {...need} />}
        </span>
      )}
    </div>
  );
}

/** V0.101 (decision 0024 section 9): a Needs you cause adds the beam dot and its button to the row: "Merge a1b2c3d"
 *  for a Ready change, bound to the head the row shows (V0.97), and "Answer" for an ask, which opens the thread. */
export function NeedButton({ t, change, need }: { t: Thread; change: Change; need: ChangeNeed }) {
  const api = useApi();
  const ask = useAsk();
  const look = needLook(need);
  const merge = (change.offers ?? []).find((o) => o.run === "fyr" && o.effect === "pr.merge");
  const canMerge = need.cause === "ready" && !!merge && !!need.head;
  if (need.cause !== "ask" && !canMerge) return <span class="beamdot" aria-hidden="true" />;
  return (
    <span class="rneed">
      <span class="beamdot" aria-hidden="true" />
      <button
        class={`btn sm${canMerge ? " p" : ""}`}
        title={look.line}
        onClick={(e) => {
          if (!canMerge) return; // Answer: the row's own click opens the thread
          e.stopPropagation();
          void press(change.id, merge!, undefined, ask, (action, args) => void api.command(t.hostId ?? "", action, t.id, args), need.head);
        }}
      >
        {look.button}
      </button>
    </span>
  );
}

export function OneRowBody({ t, now, look, extra = "", lead, pre, attrs }: BodyProps) {
  const st = t.attention.state;
  const working = t.attention.section === "working";
  const agents = groupTasks(t.tasks).running;
  // The project is on the first line now, so the second never falls back to it.
  const detail = working ? (t.activity || agents.length ? workingLine(t) : "") : st === "snoozed" ? snoozeLine(t, now) : "";
  const firstTask = agents.map((x) => x.startedAt).filter(Boolean).sort()[0];
  const when = working ? relTime(t.runStartedAt ?? firstTask, now) : whenLabel(t.activityAt ?? t.updatedAt, now);
  const sub = working && !agents.length && !!t.subagents; // "0 sub" says nothing
  const chips = sub || (!working && hasChange(t));
  const job = useThreadJob(t.id); // decision 0029 S1: first on the line, which is cut at its end
  // V0.164: heavy work Fyr did not admit starts the line, in the beam ink, and says the use itself (same on both fronts).
  const und = useRowUndeclared(t.id, now);
  return (
    <div {...attrs} class={`one${agents.length ? " has-agents" : ""}${extra}`}>
      {lead ?? <StateIcon state={st} />}
      <RowHead t={t} look={look} />
      <span class="age">{when}</span>
      <span class="m">
        <i class="ell">
          {pre}
          <RowFrom t={t} look={look} more={!!detail || !!job || und} />
          {und ? <RowUndeclared id={t.id} now={now} /> : job && <ThreadJob text={job} />}
          {(und || job) && detail ? " · " : ""}
          {detail}
          {working && !und && <ThreadCPU id={t.id} />}
          {!job && !und && <ThreadSlot id={t.id} />}
        </i>
        {chips && (
          <span class="cl">
            {sub && <span class="sb">{t.subagents ?? 0} sub</span>}
            {!working && <PrBadge t={t} short />}
          </span>
        )}
      </span>
    </div>
  );
}

