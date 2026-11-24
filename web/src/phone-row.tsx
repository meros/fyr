// The phone's inbox row (V0.native-feel): swipes (right settles, left snoozes), long press, selection, the host note.

import { haptic } from "./native.ts";
import { springAway, springTo, x3d } from "./motion-ui.tsx";
import { addSample, releaseVelocity, type Sample, SNAP_SPRING } from "./motion.ts";
import { type VNode } from "preact";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import type { Thread } from "./types.ts";
import { StateIcon } from "./icons.tsx";
import { stateLabel } from "./rows.ts";
import { LONG_PRESS_MS, idle, longPressCancelled, swipeAvail, swipeArmed, swipeEnd, swipeMove, swipeOffset, swipeStart, type Swipe, type SwipeAvail } from "./gesture.ts";
import { NeedsRowBody, OneRowBody, PHONE_LOOK } from "./row-ui.tsx";
import { GUEST_TEXT } from "./selection.ts";
import { type PhoneProps } from "./phone.tsx";

export function workingHint(list: Thread[]): string {
  const waiting = list.filter((t) => t.attention.state === "waiting").length;
  return waiting ? `${waiting} waiting` : "";
}

/** V0.54, V0.170: an offline host says so on a row, in words (it is state). Which host a row is on is its mark at the
 *  title's start (row-ui.tsx), as on the desktop, so the words for an online host are gone. */
function MHost({ t }: { t: Thread }) {
  return t.hostOffline ? <><b class="m-off">{t.host || "host"} offline</b> · </> : null;
}

/** A row with swipes (right settles, left snoozes), long press (actions) and selection. V0.native-feel: the row follows
 *  the finger without a render, a flick commits, and it leaves or returns on a spring that starts at the finger's speed. */
export function MRow({ t, p, needs, lead: own }: { t: Thread; p: PhoneProps; needs: boolean; /** V0.137: the queue's "#2" */ lead?: VNode }) {
  const avail: SwipeAvail = useMemo(() => swipeAvail(t), [t]);
  const availRef = useRef(avail);
  availRef.current = avail;
  const swRef = useRef<Swipe>(idle);
  const [under, setUnderState] = useState<{ dir: 1 | -1; armed: boolean } | null>(null);
  const underRef = useRef(under);
  const [moving, setMoving] = useState(false);
  const [hint, setHint] = useState<string | null>(null);
  const rowRef = useRef<HTMLDivElement>(null);
  const boxRef = useRef<HTMLDivElement>(null);
  const samples = useRef<Sample[]>([]);
  const leaving = useRef<{ timer: ReturnType<typeof setTimeout> } | null>(null);
  const lp = useRef<{ timer: ReturnType<typeof setTimeout> | null; x: number; y: number; fired: boolean }>({ timer: null, x: 0, y: 0, fired: false });
  const suppressClick = useRef(false);

  const setUnder = (u: { dir: 1 | -1; armed: boolean } | null) => {
    const c = underRef.current;
    if (c?.dir === u?.dir && c?.armed === u?.armed) return;
    if (u?.armed && !c?.armed) haptic(); // the tick that says "let go now"
    underRef.current = u;
    setUnderState(u);
  };
  /** The row under the finger: a style write per move, a render only when what is under it changes. */
  const paint = (s: Swipe) => {
    swRef.current = s;
    const el = rowRef.current;
    if (!el) return;
    const off = swipeOffset(s, availRef.current);
    el.style.transform = off ? x3d(off) : "";
    setUnder(off ? { dir: off > 0 ? 1 : -1, armed: swipeArmed(s, el.offsetWidth, availRef.current) } : null);
  };
  const clearLp = () => {
    if (lp.current.timer) clearTimeout(lp.current.timer);
    lp.current.timer = null;
  };
  useEffect(() => () => clearLp(), []);
  /** A row that flew off is shown again: its thread changed section while the row stayed mounted (the list's hold,
   *  an undo), or nothing happened to it at all (a refused command). */
  const comeBack = () => {
    const l = leaving.current;
    if (!l) return;
    leaving.current = null;
    clearTimeout(l.timer);
    if (rowRef.current) rowRef.current.style.transform = "";
    setUnder(null);
  };
  useLayoutEffect(comeBack, [t.attention.section]);
  useEffect(() => () => clearTimeout(leaving.current?.timer), []);

  const onDown = (e: PointerEvent) => {
    if (leaving.current || (e.pointerType === "mouse" && e.button !== 0)) return;
    rowRef.current?.getAnimations().forEach((a) => a.cancel());
    swRef.current = p.list.selecting ? idle : swipeStart(e.clientX, e.clientY);
    samples.current = [];
    lp.current = { timer: null, x: e.clientX, y: e.clientY, fired: false };
    if (!p.list.selecting) {
      lp.current.timer = setTimeout(() => {
        lp.current.fired = true;
        suppressClick.current = true;
        paint(idle);
        haptic();
        p.nav.go({ ...p.nav.at, sheet: { kind: "actions", ids: [t.id] } });
      }, LONG_PRESS_MS);
    }
  };
  const onMove = (e: PointerEvent) => {
    if (lp.current.timer && longPressCancelled(lp.current.x, lp.current.y, e.clientX, e.clientY)) clearLp();
    const cur = swRef.current;
    const next = swipeMove(cur, e.clientX, e.clientY);
    if (next.phase === "swipe" && cur.phase !== "swipe") {
      clearLp();
      setMoving(true);
      try {
        rowRef.current?.setPointerCapture(e.pointerId);
      } catch {
        /* capture unsupported */
      }
    }
    if (next.phase === "swipe") {
      samples.current = addSample(samples.current, e.timeStamp, e.clientX);
      paint(next);
    } else swRef.current = next;
  };
  /** Back to rest from where the finger left it; what is under the row stays until the row covers it. */
  const snapBack = (from: number, v: number) => {
    const el = rowRef.current;
    const a = el && from ? springTo(el, from, 0, v, SNAP_SPRING, x3d) : null;
    if (el && !a) el.style.transform = "";
    const done = () => setUnder(null);
    if (a) a.finished.then(done, done);
    else done();
  };
  /** The row flies off in its direction and the thread settles. V0.list-motion (5): the list then lets the row leave
   *  as every row leaves: it tucks into the Settled band when that is in view, else fades, while the rows under it close
   *  the gap on the list spring. Its height is never animated. */
  const flyOff = (from: number, v: number, then: () => void) => {
    const el = rowRef.current;
    const fly = el && springAway(el, from, el.offsetWidth + 24, Math.max(v, 0), x3d);
    if (!el || !fly) {
      then();
      if (el) el.style.transform = "";
      setUnder(null);
      return;
    }
    const go = () => {
      leaving.current = { timer: setTimeout(comeBack, 4000) };
      then();
    };
    fly.finished.then(go, go);
  };
  const onUp = (e: PointerEvent) => {
    clearLp();
    const cur = swRef.current;
    swRef.current = idle;
    if (cur.phase !== "swipe") return;
    setMoving(false);
    suppressClick.current = true;
    const el = rowRef.current;
    const off = swipeOffset(cur, avail);
    const v = releaseVelocity(samples.current, e.timeStamp);
    const r = swipeEnd(cur, el?.offsetWidth ?? 390, avail, v);
    if (r.kind === "settle") {
      haptic("firm");
      flyOff(off, v, () => p.actions.settle([t.id]));
      return;
    }
    snapBack(off, r.kind === "none" && !r.reason ? v : 0);
    if (r.kind === "snooze") (haptic(), p.nav.go({ ...p.nav.at, sheet: { kind: "snooze", ids: [t.id] } }));
    else if (r.reason) {
      setHint(r.reason);
      setTimeout(() => setHint(null), 1400);
    }
  };
  const onCancel = () => {
    clearLp();
    const cur = swRef.current;
    swRef.current = idle;
    setMoving(false);
    snapBack(swipeOffset(cur, avail), 0);
  };
  const onClick = () => {
    if (suppressClick.current) {
      suppressClick.current = false;
      return;
    }
    if (p.list.selecting) {
      const n = new Set(p.list.picked);
      if (n.has(t.id)) n.delete(t.id);
      else n.add(t.id);
      p.list.setPicked(n);
      return;
    }
    p.nav.go({ screen: { name: "thread", id: t.id }, sheet: null });
  };

  const side = under ? (under.dir > 0 ? avail.right : avail.left) : null;
  const st = t.attention.state;
  const picked = p.list.picked.has(t.id);
  const lead = p.list.selecting ? <i class={`m-ck${picked ? " on" : ""}`} /> : own;
  const pre = <MHost t={t} />;
  // V0.109: no row is marked here. The phone shows the inbox or one thread, never both, so there is no open thread
  // beside the list for a mark to follow; the desktop marks the open thread's row (app.tsx, selection.ts).
  const guest = p.inbox.guest?.id === t.id ? p.inbox.guest.why : undefined; // V0.192: as on the desktop
  const body = needs ? <NeedsRowBody t={t} now={p.now} look={PHONE_LOOK} lead={lead} pre={pre} guest={guest} /> : <OneRowBody t={t} now={p.now} look={PHONE_LOOK} lead={lead} pre={pre} guest={guest} />;
  return (
    <div ref={boxRef} class={`m-swp${picked ? " picked" : ""}${moving ? " moving" : ""}`} data-mk={`row:${t.id}`} data-sec={t.attention.section}>
      {under && side && (
        <div class={`m-under ${under.dir > 0 ? "l" : "r"} ${!side.ok ? "no" : under.dir > 0 ? "set" : "snz"}${under.armed ? " armed" : ""}`}>
          {side.ok && under.dir > 0 && <StateIcon state="settled" size={16} />}
          <span class="ell">{side.label}</span>
          {side.ok && under.dir < 0 && <StateIcon state="snoozed" size={16} />}
        </div>
      )}
      {hint && <div class="m-under l no show">{hint}</div>}
      <div
        ref={rowRef}
        data-id={t.id}
        class="m-mv"
        onPointerDown={onDown}
        onPointerMove={onMove}
        onPointerUp={onUp}
        onPointerCancel={onCancel}
        onClick={onClick}
        onContextMenu={(e) => e.preventDefault()}
        role="button"
        aria-label={`${t.title}, ${stateLabel(st)}${guest ? `, ${GUEST_TEXT[guest].tag}` : ""}`}
      >
        {body}
      </div>
    </div>
  );
}

// ---- search ----
