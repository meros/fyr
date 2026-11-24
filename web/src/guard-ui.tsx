// Decision 0032, V0.188: the memory guard's chart. One component for the desktop and the phone (Settings → Hosts → a host):
// a band for when the host's memory was short and one lane per thread the guard froze, hatched while frozen, with the
// freeze's number at its start. Plain SVG that scales to the width; the colours are the app's tokens.

import { useEffect, useId, useRef, useState } from "preact/hooks";
import { GUARD_WINDOW, guardChart, type Guard } from "./guard.ts";

const W0 = 360; // the width before the figure is measured (a phone's)
const LANE = 20;
const TOP = 16;
const AXIS = 14;

export function GuardChart({ guard, now, titleOf }: { guard: Guard | undefined; now: number; titleOf: (thread: string) => string }) {
  const id = useId().replace(/[^a-z0-9]/gi, "");
  // V0.settings-ia: the viewBox is as wide as the figure, so one unit is one CSS pixel and the labels keep the size
  // the stylesheet gives them on every width (a fixed 360 scaled them up with the dialog: "15 min" at 22 px).
  const fig = useRef<HTMLElement>(null);
  const [W, setW] = useState(W0);
  useEffect(() => {
    const el = fig.current;
    if (!el) return;
    const read = () => setW(Math.max(240, Math.round(el.clientWidth) || W0));
    read();
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(read);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);
  const c = guardChart(guard, now, titleOf);
  const n = Math.max(1, c.lanes.length);
  const H = TOP + n * LANE + AXIS;
  const x = (v: number) => (v * W).toFixed(1);
  const ticks = [0, 5, 10, 15].map((m) => ({ m, v: 1 - (m * 60_000) / GUARD_WINDOW }));
  return (
    <figure class="gchart" data-guard-chart ref={fig}>
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={c.alt} preserveAspectRatio="xMidYMid meet">
        <defs>
          <pattern id={`gh${id}`} width="6" height="6" patternUnits="userSpaceOnUse" patternTransform="rotate(135)">
            <rect width="6" height="6" class="g-hatch-bg" />
            <rect width="2.5" height="6" class="g-hatch" />
          </pattern>
        </defs>
        {c.short.map((s, i) => (
          <rect key={i} class="g-short" x={x(s.a)} y={0} width={Math.max(1, (s.b - s.a) * W).toFixed(1)} height={H - AXIS} />
        ))}
        {c.lanes.map((l, i) => {
          const y = TOP + i * LANE;
          return (
            <g key={l.thread} data-lane={l.thread}>
              <line class="g-run" x1={0} x2={W} y1={y + LANE / 2} y2={y + LANE / 2} />
              {l.frozen.map((s, k) => (
                <rect key={k} class="g-frozen" x={x(s.a)} y={y + 2} width={Math.max(2, (s.b - s.a) * W).toFixed(1)} height={LANE - 4} fill={`url(#gh${id})`} />
              ))}
              {l.marks.map((m) => (
                <text key={m.x} class="g-n" x={Math.min(W - 10, m.x * W + 3).toFixed(1)} y={y + 4} dominant-baseline="hanging">
                  {m.n || ""}
                </text>
              ))}
            </g>
          );
        })}
        {ticks.map((t) => (
          <text key={t.m} class="g-tick" x={x(t.v)} y={H - 2} text-anchor={t.m === 15 ? "start" : t.m === 0 ? "end" : "middle"}>
            {t.m === 0 ? "now" : `${t.m} min`}
          </text>
        ))}
      </svg>
      <ul class="g-names">
        {c.lanes.map((l) => (
          <li key={l.thread}>
            <span class={`g-dot${l.live ? " live" : ""}`} aria-hidden="true" /> {l.title}
            {l.live ? " · frozen now" : ""}
          </li>
        ))}
      </ul>
      <figcaption class="g-alt">{c.alt}</figcaption>
    </figure>
  );
}
