// SPEC V0.137, V0.163: the section "Waiting to run" (was "Waiting for a slot"), one component for the desktop list and the phone inbox. Each front
// draws its own row (it passes `row`); this draws the header, one part per host with a queue, the priority waits
// pinned on top, the place of each thread ("#2") and the reorder control of V0.119 (4).

import type { ComponentChildren, VNode } from "preact";
import { useMemo, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { slotsOf } from "./host-load.ts";
import { patchSlots, refreshLoad, useLoad } from "./load-ui.tsx";
import { MoveControls, ReorderButton, useReorder } from "./reorder-ui.tsx";
import { priorityLabel, queueParts, reorderOwners, slotQueue, withQueue, type QueueEntry, type QueuePart } from "./slot-queue.ts";
import { count } from "./format.ts";
import { StateIcon } from "./icons.tsx";
import { SectionTitle, sectionHead } from "./row-ui.tsx";
import type { Thread } from "./types.ts";

/** Every host's queue, from the one load poll (5 s; at once after a reorder). */
export function useSlotQueue(): QueueEntry[] {
  const load = useLoad();
  return useMemo(() => slotQueue(load), [load]);
}

interface SlotRowArgs {
  /** "#2": the grip on the desktop, a plain label on the phone */
  lead: VNode;
  /** the reorder control's ref and its lift and drop class, for the row's root */
  attrs: { ref: (el: HTMLElement | null) => void; class: string };
}

interface Props {
  phone: boolean;
  /** the whole queue (useSlotQueue) */
  queue: QueueEntry[];
  /** the queued threads the list shows (sections().slots); the queue's other threads are hidden by a filter or search */
  threads: Thread[];
  /** the header's count: V0.192 leaves out a current thread the filters hide, which `threads` draws */
  n: number;
  row(t: Thread, a: SlotRowArgs): ComponentChildren;
  /** V0.waiting-folds: the section is unfolded (isOpen "slots"; open by default) */
  open: boolean;
  onToggle(): void;
  /** V0.192: the current thread, drawn alone in a folded section */
  guest: string | null;
}

const HINT = "Heavy jobs wait here until the host has room. The first in line starts next.";

export function SlotQueueSection(p: Props) {
  // The section shows only while a queue has anyone in it (V0.137 (1)). V0.waiting-folds: its header folds it, like the
  // other sections; folded, it draws only the current thread's row (V0.192), without the why line and Fyr's own tests.
  const [reordering, setReordering] = useState(false);
  const shown = Object.fromEntries(p.threads.map((t) => [t.id, t]));
  // Nothing to draw when every queued thread is hidden (a filter, a search, Needs you) and no priority wait is queued.
  if (!withQueue(p.queue, shown).length) return null;
  const parts = queueParts(p.queue);
  const n = p.n;
  const open = p.open || !n; // nothing to fold without a thread in it
  const movable = open && p.threads.length > 1;
  const drawn = open ? shown : p.guest && shown[p.guest] ? { [p.guest]: shown[p.guest] } : {};
  const body = parts.map((part) => <Part key={part.host} part={part} shown={drawn} phone={p.phone} reordering={reordering && movable} folded={!open} row={p.row} />);
  const head = sectionHead({ n, open, toggle: p.onToggle });
  if (p.phone)
    return (
      <section id="m-sec-slots" class="sq">
        <div class={`m-sec${movable ? " hasbtn" : ""}`} data-mk="sec:slots" {...head}>
          <SectionTitle label="Waiting to run" icon={<StateIcon state="waiting" size={16} />} n={n} shown={count(n)} open={open} mk="slots" />
          {movable && (
            // Phone: a row here swipes, so the order changes in the one Reorder mode (reorder-ui.tsx), as in Settings.
            // The class is the inbox header's small button, not Settings' 44 px .btn: this header is 44 px itself.
            <ReorderButton on={reordering} set={setReordering} class={`m-sbtn${reordering ? " on" : ""}`} />
          )}
        </div>
        {body}
      </section>
    );
  return (
    <div id="sec-slots" class="sq">
      <div class={`sec${n ? " tog" : ""}`} data-mk="sec:slots" title={HINT} {...head}>
        <SectionTitle label="Waiting to run" icon={<StateIcon state="waiting" size={14} />} n={n} shown={count(n)} open={open} mk="slots" />
        {movable && <span class="h ell">drag to reorder</span>}
      </div>
      {body}
    </div>
  );
}

function Part({ part, shown, phone, reordering, folded, row }: { part: QueuePart; shown: Record<string, Thread>; phone: boolean; reordering: boolean; folded: boolean; row: Props["row"] }) {
  const api = useApi();
  const rows = withQueue(part.entries, shown).filter((r) => !folded || r.thread);
  const threads = rows.filter((r) => r.thread);
  const ids = threads.map((r) => r.entry.owner);
  const all = part.entries.filter((e) => !e.priority).map((e) => e.owner);
  const load = useLoad();
  const send = async (_next: string[], from: number, to: number): Promise<string | null> => {
    const owners = reorderOwners(all, ids, from, to);
    const pri = part.entries.filter((e) => e.priority).map((e) => ({ owner: e.owner, kind: e.kind, priority: true }));
    const kinds = new Map(part.entries.map((e) => [e.owner, e.kind]));
    const host = load.find((h) => h.host === part.host)?.hostId; // the queue's own host, as its load was read
    if (!host) return "This host reports no install id, so its queue cannot be reordered from here";
    // Shown on this device first; the receipt (the host's queue as it is now) or the next poll corrects it.
    patchSlots(part.host, (s) => ({ ...s, waiting: [...pri, ...owners.map((o) => ({ owner: o, kind: kinds.get(o) ?? "" }))] }));
    const r = await api.command(host, "host.slotOrder", undefined, { owners });
    const queue = r.ok ? slotsOf(r.result) : null;
    if (queue) patchSlots(part.host, queue);
    refreshLoad();
    return r.ok ? null : (r.error ?? "The order did not change");
  };
  const ro = useReorder(ids, send);
  // Decision 0029 S2: why the next job of this host waits, on both fronts (a row's line is cut at its end).
  const why = useLoad().find((h) => h.host === part.host)?.slots?.why; // V0.163: in percent of the host
  let i = 0;
  if (folded && !rows.length) return null;
  return (
    <div class="sqh" data-host={part.host}>
      {part.label && <div class={phone ? "sqhn m" : "sqhn"}>{part.label}</div>}
      {why && !folded && <div class={phone ? "sqwhy m" : "sqwhy"}>The next waits: {why}</div>}
      {ro.err && (
        <div class="sqerr" role="alert">
          {ro.err}
        </div>
      )}
      {rows.map((r) => {
        if (!r.thread)
          return (
            // The deploy's or the updater's tests go ahead of every thread (V0.86 (1)); nobody moves them.
            <div key={r.entry.owner} class={phone ? "sqp m" : "sqp"} data-owner={r.entry.owner} title="Fyr's own tests go first and cannot be moved">
              <span class="sqn pin">#{r.entry.pos}</span>
              <span class="ell">{priorityLabel(r.entry)}</span>
              <span class="sqk">first</span>
            </div>
          );
        const k = i++;
        const t = r.thread;
        const name = t.title || "Untitled";
        const label = `#${r.entry.pos}`;
        const g = ro.grip(k, name);
        const lead =
          phone || ids.length < 2 ? (
            <span class="sqn" aria-label={`Place ${r.entry.pos} in the queue`}>
              {label}
            </span>
          ) : (
            // Desktop: the place is the grip (drag, or Alt+↑/↓); a press on it does not open the thread.
            <span {...g} class="sqn grip" onClick={(e: MouseEvent) => e.stopPropagation()}>
              {label}
            </span>
          );
        const ra = ro.row(k);
        // In the phone's mode the row and its controls move as one, so the wrapper is what the drag measures.
        if (phone && reordering)
          return (
            <div key={t.id} class={`sqmv ${ra.class}`} ref={ra.ref} data-id={t.id}>
              <div class="sqrow">{row(t, { lead, attrs: { ref: () => {}, class: "" } })}</div>
              <MoveControls ro={ro} i={k} name={name} />
            </div>
          );
        return row(t, { lead, attrs: ra });
      })}
    </div>
  );
}
