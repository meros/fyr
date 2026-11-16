// The inbox rows (V0.91 shape), shared by the desktop list and the phone list so the two stay the same.
// Each front adds only its wrapper (selection, swipes); the phone sets its own sizes in phone.css.

import type { ComponentChildren } from "preact";
import type { Change, ChangeNeed, Thread } from "./types.ts";
import { useApi } from "./api-context.ts";
import { useAsk } from "./confirm-ui.tsx";
import { press } from "./confirm.ts";
import { ThreadCPU, ThreadSlot } from "./load-ui.tsx";
import { StartedBy, ThreadProject } from "./project-ui.tsx";
import { StateIcon, stateClass } from "./icons.tsx";
import { rowMeta, rowState } from "./rows.ts";
import { relTime, untilLabel, whenLabel } from "./format.ts";
import { prChip } from "./pr.ts";
import { changeChip, needLook, rowNeed } from "./offers.ts";
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
function RowHead({ t }: { t: Thread }) {
  return (
    <span class="hd">
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
  const need = rowNeed(t); // V0.101: the host's cause, read only
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
          void press(change.id, merge!, undefined, ask, (action, args) => void api.command(action, t.id, args), need.head);
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

