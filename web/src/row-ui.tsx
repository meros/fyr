// The inbox rows (V0.91 shape), shared by the desktop list and the phone list so the two stay the same.
// Each front adds only its wrapper (selection, swipes); the phone sets its own sizes in phone.css.

import type { ComponentChildren } from "preact";
import type { Thread } from "./types.ts";
import { ThreadCPU, ThreadSlot } from "./load-ui.tsx";
import { StartedBy, ThreadProject } from "./project-ui.tsx";
import { StateIcon, stateClass } from "./icons.tsx";
import { rowMeta, rowState } from "./rows.ts";
import { relTime, untilLabel, whenLabel } from "./format.ts";
import { prChip } from "./pr.ts";
import { changeChip } from "./offers.ts";
import { groupTasks, workingLine } from "./tasks.ts";

/** What differs between the fronts, each for a reason. */
export interface RowLook {
  /** the phone has no keyboard, so its rows name no keys */
  keys: boolean;
  /** a tap on the phone opens the row, so "started by" is not a link there */
  link: boolean;
}

export const DESKTOP_LOOK: RowLook = { keys: true, link: true };
export const PHONE_LOOK: RowLook = { keys: false, link: false };

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

/** A row has one shape in every section (V0.91): the project tag and the title on the first line, with the age at the
 * end; what the thread does on the second; its chips (PR or change, subagents) on a third line of their own, only when
 * it has one. Nothing shares a line with the title but the project and the age, so nothing is cut to fit a chip. */
/** V0.104: the pin mark (Lucide "pin", ISC). */
export const PIN_ICON = (
  <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
    <path d="M12 17v5" />
    <path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z" />
  </svg>
);

function RowHead({ t }: { t: Thread }) {
  return (
    <span class="hd">
      {t.pinnedAt && (
        <span class="pin" title="Pinned" aria-label="Pinned" role="img">
          {PIN_ICON}
        </span>
      )}
      <ThreadProject t={t} />
      <span class="t ell" title={t.title}>
        {t.title || "Untitled"}
      </span>
    </span>
  );
}

function hasChange(t: Thread): boolean {
  return !!(t.pr || changeChip(t));
}

export function NeedsRowBody({ t, now, look, extra = "", lead, pre, attrs }: BodyProps) {
  const st = t.attention.state;
  return (
    <div {...attrs} class={`rw${st === "done" ? " dn" : ""}${extra}`}>
      {lead ?? (
        <span class="tile">
          <StateIcon state={st} />
        </span>
      )}
      <RowHead t={t} />
      <span class="age">{relTime(t.activityAt ?? t.updatedAt, now)}</span>
      <span class="m ell">
        <b class={stateClass(st)}>{rowState(t)}</b> · {pre}{t.parent && <><StartedBy parentId={t.parent} link={look.link} /> · </>}{rowMeta(t, { keys: look.keys })}
      </span>
      {hasChange(t) && (
        <span class="cl">
          <PrBadge t={t} />
        </span>
      )}
    </div>
  );
}

export function OneRowBody({ t, now, look, extra = "", lead, pre, attrs }: BodyProps) {
  const st = t.attention.state;
  const working = t.attention.section === "working";
  const agents = groupTasks(t.tasks).running;
  // The project is on the first line now, so the second never falls back to it.
  const detail = working ? (t.activity || agents.length ? workingLine(t) : "") : st === "snoozed" ? `until ${untilLabel(t.view?.snoozeUntil, now)}` : "";
  const firstTask = agents.map((x) => x.startedAt).filter(Boolean).sort()[0];
  const when = working ? relTime(t.runStartedAt ?? firstTask, now) : whenLabel(t.activityAt ?? t.updatedAt, now);
  const sub = working && !agents.length && !!t.subagents; // "0 sub" says nothing
  const chips = sub || (!working && hasChange(t));
  return (
    <div {...attrs} class={`one${agents.length ? " has-agents" : ""}${extra}`}>
      {lead ?? <StateIcon state={st} />}
      <RowHead t={t} />
      <span class="age">{when}</span>
      <span class="m">
        <i class="ell">
          {pre}
          {t.parent && (
            <>
              <StartedBy parentId={t.parent} link={look.link} />
              {detail ? " · " : ""}
            </>
          )}
          {detail}
          {working && <ThreadCPU id={t.id} />}
          <ThreadSlot id={t.id} />
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

