// The inbox rows (V0.91 shape), shared by the desktop list and the phone list so the two stay the same.
// Each front adds only its wrapper (selection, swipes); the phone sets its own sizes in phone.css.

import type { ComponentChildren } from "preact";
import { useContext, useMemo } from "preact/hooks";
import type { Change, ChangeNeed, Thread } from "./types.ts";
import { useApi } from "./api-context.ts";
import { useAsk } from "./confirm-ui.tsx";
import { press } from "./confirm.ts";
import { ThreadCPU, ThreadJob, ThreadService, ThreadSlot, useRowLoad } from "./load-ui.tsx";
import { RowUndeclared, useRowUndeclared } from "./undeclared-ui.tsx";
import { RowProject, RowProjectName, StartedBy } from "./project-ui.tsx";
import { ProfilesContext, useHosts } from "./host-pick-ui.tsx";
import { HostMark } from "./host-mark-ui.tsx";
import { multiHost, rowProvider } from "./hosts.ts";
import { StateIcon, stateClass } from "./icons.tsx";
import { rowDetail, rowState, stateNamesPr } from "./rows.ts";
import { relTime, whenLabel } from "./format.ts";
import { snoozeLine } from "./snooze.ts";
import { rowPrChip } from "./pr.ts";
import { changeChip, headerPrs, needLook, rowNeed, threadPrs } from "./offers.ts";
import { groupTasks, workingLine } from "./tasks.ts";
import { WakeChip } from "./wake-ui.tsx";
import { GUEST_TEXT, type GuestWhy } from "./selection.ts";

/** What differs between the fronts, each for a reason. */
interface RowLook {
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
  /** V0.192: the row is here only because it is the current thread */
  guest?: GuestWhy;
}

/** V0.196: on a section running down to its auto-close, on both fronts: using it restarts the time. */
export function hideUse(running: boolean, use: () => void): Record<string, unknown> {
  return running ? { onPointerDown: use, onPointerMove: use, onKeyDown: use } : {};
}

/** V0.48, V0.196, V0.autoclose-clock: the small clock in the header of a section that closes by itself; it restarts on use. Its duration
 *  is read once per deadline: the list re-renders on every store update, and a new animation-duration on a running
 *  animation rescales the time already gone, so the clock would drain faster than the section closes. */
export function HideClock({ at }: { at: number }) {
  const left = useMemo(() => Math.max(0, (at - Date.now()) / 1000), [at]);
  return <span key={at} class="ahc" style={{ "--ah": `${left}s` }} title="Closes by itself" aria-hidden="true" />;
}

/** V0.section-heads: what a section header shows, on both fronts: the fold chevron, the section's icon, its name and its
 *  count. `open` is left out for a header that cannot fold (Pinned), and a section with no rows has nothing
 *  to fold; the chevron's place stays, so the icons line up. The icon is decoration here: the name says the same. */
export function SectionTitle(p: { label: string; icon: ComponentChildren; n: number; shown: string; open?: boolean; attention?: boolean }) {
  const fold = p.open !== undefined && p.n > 0;
  return (
    <span class={`sti${p.n ? "" : " none"}${p.attention && p.n ? " att" : ""}`}>
      <span class={`stc${fold && p.open ? " open" : ""}`} aria-hidden="true">
        {fold && (
          <svg viewBox="0 0 12 12" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M4.5 2.5 8 6l-3.5 3.5" />
          </svg>
        )}
      </span>
      <span class="sticon" aria-hidden="true">
        {p.icon}
      </span>
      <span class="stl ell">{p.label}</span>
      <span class="n">{p.shown}</span>
    </span>
  );
}

/** V0.186: a section header's toggle on both fronts, the only thing that folds a section from its header. */
/** V0.your-move: the label over a group of Your move, on both fronts. */
export function NeedsGroupHead({ label }: { label: string }) {
  return <div class="ngh">{label}</div>;
}

export function sectionHead(o: { n: number; open: boolean; toggle(): void }): Record<string, unknown> {
  if (!o.n) return {};
  return {
    role: "button",
    tabIndex: 0,
    "aria-expanded": o.open,
    onClick: (e: MouseEvent) => {
      o.toggle();
      // A click leaves the keys with the list, as it did when a header took no focus; Tab and Enter still reach it.
      if (e.detail) (e.currentTarget as HTMLElement).blur();
    },
    onKeyDown: (e: KeyboardEvent) => {
      if (e.target !== e.currentTarget || (e.key !== "Enter" && e.key !== " ")) return;
      e.preventDefault();
      e.stopPropagation(); // Enter on a focused header is the header's, not "open the marked row"
      o.toggle();
    },
  };
}

/** V0.192: says why a row is in the list although the filters or a fold would hide it (Lucide "eye-off", ISC). */
function GuestTag({ why }: { why: GuestWhy }) {
  const x = GUEST_TEXT[why];
  return (
    <span class="gst" title={x.hint}>
      <svg viewBox="0 0 24 24" width="12" height="12" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49" />
        <path d="M14.084 14.158a3 3 0 0 1-4.242-4.242" />
        <path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143" />
        <path d="m2 2 20 20" />
      </svg>
      {x.tag}
    </span>
  );
}

function PrBadge({ t, short }: { t: Thread; short?: boolean }) {
  const c = changeChip(t, short); // V0.59 (8): a change that is not a PR, when no PR is open
  if (c)
    return (
      <span class={`pr chgchip ${c.cls}`} title={c.title}>
        {c.text}
      </span>
    );
  const open = headerPrs(t);
  const l = rowPrChip(open.length ? open : threadPrs(t), short);
  if (!l) return <span class="pr none" aria-label="no pull request" />;
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
  const host = multiHost(hosts) ? t.host : undefined; // slice 2b: the host whose state the thread is in
  return (
    // The icons and the pin sit over the title's first-line indent (app.css): inside the clamped box an element would
    // become a block of its own (-webkit-box), and Safari clamps only plain text.
    <span class={`hd${t.pinnedAt ? " pinned" : ""}${t.title ? "" : " untitled"}`} style={{ "--ri": `${look.icon}px`, ...(host ? { "--rh": `${look.icon + 4}px` } : {}) }}>
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

/** V0.135: the start of a row's second line: the project's name when the icon does not say it, the provider when there
 *  is more than one (V0.206), and ↳ for a thread another thread started. */
function RowFrom({ t, look, more }: { t: Thread; look: RowLook; more: boolean }) {
  const provider = rowProvider(t, useContext(ProfilesContext));
  return (
    <>
      <RowProjectName t={t} sep={!!provider || !!t.parent || more} />
      {provider && (
        <>
          <span class="rpv" title={provider.title}>
            <span class="pdot" style={provider.color ? { background: provider.color } : undefined} aria-hidden="true" />
            {provider.name}
          </span>
          {t.parent || more ? " · " : ""}
        </>
      )}
      {t.parent && <StartedBy parentId={t.parent} link={look.link} mark />}
    </>
  );
}

function hasChange(t: Thread): boolean {
  return !!(threadPrs(t).length || changeChip(t));
}

export function NeedsRowBody({ t, now, look, extra = "", lead, pre, attrs, guest }: BodyProps) {
  const st = t.attention.state;
  const need = rowNeed(t); // V0.101: the host's cause, read only
  // V0.row-calm: the chip is left out when the state word already names that PR ("Merge #4170" over "#4170 ready").
  const chip = hasChange(t) && !stateNamesPr(t);
  const wake = !!t.wakes?.length; // V0.wakeups-via-watch
  // V0.row-calm: when the button says the state word ("Merge" by "Merge"), the line starts with what it acts on.
  const word = rowState(t);
  const said = !!need && needButton(need.change, need.need) === word;
  return (
    <div {...attrs} class={`rw${st === "done" ? " dn" : ""}${need ? " hasneed" : ""}${guest ? " guest" : ""}${extra}`}>
      {lead ?? (
        <span class="tile">
          <StateIcon state={st} />
        </span>
      )}
      <RowHead t={t} look={look} />
      <span class="age">{relTime(t.activityAt ?? t.updatedAt, now)}</span>
      <span class="m ell">
        {guest && <GuestTag why={guest} />}
        {!said && (
          <>
            <b class={stateClass(st)}>{word}</b> ·{" "}
          </>
        )}
        {pre}
        <RowFrom t={t} look={look} more />
        {rowDetail(t, { keys: look.keys })}
      </span>
      {(chip || wake) && (
        <span class="cl">
          {chip && <PrBadge t={t} />}
          <WakeChip t={t} now={now} cls="pr" />
        </span>
      )}
      {/* V0.row-calm: the button sits at the right of the state line, under the age, not on a line of its own */}
      {need && <NeedButton t={t} {...need} />}
    </div>
  );
}

/** V0.101 (decision 0024 section 9): a Needs you cause adds the beam dot and its button to the row: "Merge" for a
 *  Ready change, bound to the head the row shows (V0.97; V0.merge-label: the head is in its title, not on the button),
 *  and "Answer" for an ask, which opens the thread. */
/** The button a Needs you cause shows on its row ("Merge", "Answer"), or "" when it shows only the beam dot. */
function needButton(change: Change, need: ChangeNeed): string {
  const merge = (change.offers ?? []).some((o) => o.run === "fyr" && o.effect === "pr.merge");
  const canMerge = need.cause === "ready" && merge && !!need.head;
  return need.cause === "ask" || canMerge ? needLook(need).button : "";
}

function NeedButton({ t, change, need }: { t: Thread; change: Change; need: ChangeNeed }) {
  const api = useApi();
  const ask = useAsk();
  const look = needLook(need);
  const merge = (change.offers ?? []).find((o) => o.run === "fyr" && o.effect === "pr.merge");
  const canMerge = need.cause === "ready" && !!merge && !!need.head;
  if (!needButton(change, need)) return <span class="beamdot" aria-hidden="true" />;
  return (
    <span class="rneed">
      <span class="beamdot" aria-hidden="true" />
      <button
        class={`btn sm${canMerge ? " p" : ""}`}
        title={look.line}
        onClick={(e) => {
          if (!canMerge) return; // Answer: the row's own click opens the thread
          e.stopPropagation();
          void press(change.id, merge!, undefined, ask, (action, args) => void api.command(t.hostId, action, t.id, args), need.head);
        }}
      >
        {look.button}
      </button>
    </span>
  );
}

export function OneRowBody({ t, now, look, extra = "", lead, pre, attrs, guest }: BodyProps) {
  const st = t.attention.state;
  const working = t.attention.section === "working";
  const agents = groupTasks(t.tasks).running;
  // The project is on the first line now, so the second never falls back to it.
  // V0.ci-wait-moves: a row in Moving always says why (Waiting for CI on #846, Starting, Waiting for the machine).
  const detail = working ? workingLine(t) : st === "snoozed" ? snoozeLine(t, now) : "";
  const firstTask = agents.map((x) => x.startedAt).filter(Boolean).sort()[0];
  const when = working ? relTime(t.runStartedAt ?? firstTask, now) : whenLabel(t.activityAt ?? t.updatedAt, now);
  const sub = working && !agents.length && !!t.subagents; // "0 sub" says nothing
  // One load subscription per row: its CPU reading, its job (decision 0029 S1: first on the line, which is cut at its
  // end) and its services (V0.services-not-jobs).
  const { hasCPU, job, service } = useRowLoad(t.id);
  // V0.164: heavy work Fyr did not admit starts the line, in the beam ink, and says the use itself (same on both fronts).
  const und = useRowUndeclared(t.id, now);
  const cpu = working && !und && hasCPU;
  // V0.wakeups-via-watch: a thread that waits on the agent's wake-up says so, apart from a finished one. On the row the chip
  // only shows: a tap on a row opens the thread (and the phone's row swipes), where the same chip cancels it.
  const wake = !working && !!t.wakes?.length;
  const chips = sub || cpu || (!working && hasChange(t)) || wake;
  return (
    <div {...attrs} class={`one${agents.length ? " has-agents" : ""}${guest ? " guest" : ""}${extra}`}>
      {lead ?? <StateIcon state={st} />}
      <RowHead t={t} look={look} />
      <span class="age">{when}</span>
      <span class="m">
        <i class="ell">
          {guest && <GuestTag why={guest} />}
          {pre}
          <RowFrom t={t} look={look} more={!!detail || !!job || und} />
          {und ? <RowUndeclared id={t.id} now={now} /> : job && <ThreadJob text={job} />}
          {(und || job) && detail ? " · " : ""}
          {detail}
          {!job && !und && <ThreadSlot id={t.id} />}
          <ThreadService text={service} />
        </i>
        {chips && (
          <span class="cl">
            {cpu && <ThreadCPU id={t.id} chip />}
            {sub && <span class="sb">{t.subagents ?? 0} sub</span>}
            {!working && hasChange(t) && <PrBadge t={t} short />}
            {wake && <WakeChip t={t} now={now} cls="pr" />}
          </span>
        )}
      </span>
    </div>
  );
}

