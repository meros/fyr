// SPEC V0.136: the section "Waiting for a slot", one component for the desktop list and the phone inbox. Each front
// draws its own row (it passes `row`); this draws the header, one part per host with a queue, the priority waits
// pinned on top, the place of each thread ("#2") and the reorder control of V0.119 (4).

import type { ComponentChildren, VNode } from "preact";
import { useMemo, useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import type { Slots } from "./load.ts";
import { patchSlots, refreshLoad, useLoad } from "./load-ui.tsx";
import { MoveButtons, useReorder } from "./settings-rows.tsx";
import { priorityLabel, queueParts, reorderOwners, slotQueue, withQueue, type QueueEntry, type QueuePart } from "./slot-queue.ts";
import { count } from "./format.ts";
import type { Thread } from "./types.ts";

/** Every host's queue, from the one load poll (5 s; at once after a reorder). */
export function useSlotQueue(): QueueEntry[] {
  const load = useLoad();
  return useMemo(() => slotQueue(load), [load]);
}

export interface SlotRowArgs {
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
  row(t: Thread, a: SlotRowArgs): ComponentChildren;
}

const HINT = "Heavy jobs (test suites, builds, e2e runs) take a slot; the first in the queue gets the next free one";

export function SlotQueueSection(p: Props) {
  // The section shows only while a queue has anyone in it (V0.136 (1)); it has no collapse: it is short and passing.
  const [reordering, setReordering] = useState(false);
  if (!p.queue.length) return null;
  const parts = queueParts(p.queue);
  const shown = Object.fromEntries(p.threads.map((t) => [t.id, t]));
  const movable = p.threads.length > 1;
  const n = p.threads.length;
  const body = parts.map((part) => <Part key={part.host} part={part} shown={shown} phone={p.phone} reordering={reordering && movable} row={p.row} />);
  if (p.phone)
    return (
      <section id="m-sec-slots" class="sq">
        <div class="m-sec hasbtn">
          Waiting for a slot
          <span class="n">{count(n)}</span>
          {movable && (
            // Phone: touch has no drag handle that fits beside a swipe row and no hover, so the order changes in a
            // Reorder mode with ↑ and ↓ of 44 px (V0.121's control).
            <button class="m-sbtn" aria-pressed={reordering} onClick={() => setReordering(!reordering)}>
              {reordering ? "Done" : "Reorder"}
            </button>
          )}
        </div>
        {body}
      </section>
    );
  return (
    <div id="sec-slots" class="sq">
      <div class="sec" title={HINT}>
        Waiting for a slot
        <span class="n">{count(n)}</span>
        {movable && <span class="h ell">drag to reorder</span>}
      </div>
      {body}
    </div>
  );
}

function Part({ part, shown, phone, reordering, row }: { part: QueuePart; shown: Record<string, Thread>; phone: boolean; reordering: boolean; row: Props["row"] }) {
  const api = useApi();
  const [err, setErr] = useState<string | null>(null);
  const rows = withQueue(part.entries, shown);
  const threads = rows.filter((r) => r.thread);
  const ids = threads.map((r) => r.entry.owner);
  const all = part.entries.filter((e) => !e.priority).map((e) => e.owner);
  const send = async (from: number, to: number) => {
    const owners = reorderOwners(all, ids, from, to);
    const pri = part.entries.filter((e) => e.priority).map((e) => ({ owner: e.owner, kind: e.kind, priority: true }));
    const kinds = new Map(part.entries.map((e) => [e.owner, e.kind]));
    // Shown on this device first; the receipt (the host's queue as it is now) or the next poll corrects it.
    patchSlots(part.host, (s) => ({ ...s, waiting: [...pri, ...owners.map((o) => ({ owner: o, kind: kinds.get(o) ?? "" }))] }));
    setErr(null);
    const r = await api.command("host.slotOrder", undefined, { host: part.host, owners });
    if (r.ok && r.result && typeof r.result === "object" && "waiting" in (r.result as object)) patchSlots(part.host, r.result as unknown as Slots);
    else if (!r.ok) setErr(r.error ?? "The order did not change");
    refreshLoad();
  };
  const ro = useReorder(ids, (f, t) => void send(f, t));
  let i = 0;
  return (
    <div class="sqh" data-host={part.host}>
      {part.label && <div class={phone ? "sqhn m" : "sqhn"}>{part.label}</div>}
      {err && (
        <div class="sqerr" role="alert">
          {err}
        </div>
      )}
      {rows.map((r) => {
        if (!r.thread)
          return (
            // The deploy's or the updater's tests go ahead of every thread (V0.86 (1)); nobody moves them.
            <div class={phone ? "sqp m" : "sqp"} data-owner={r.entry.owner} title="Fyr's own tests go first and cannot be moved">
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
        const ra = ro.row(k) as SlotRowArgs["attrs"];
        const el = row(t, { lead, attrs: ra });
        return phone && reordering ? (
          <div class="sqmv" data-id={t.id}>
            <div class="sqrow">{el}</div>
            <MoveButtons name={name} i={k} n={ids.length} onMove={(f, to) => void send(f, to)} />
          </div>
        ) : (
          el
        );
      })}
    </div>
  );
}
