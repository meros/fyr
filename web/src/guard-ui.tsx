// Decision 0032, V0.188: the memory guard's chart. One component for the desktop and the phone (Settings → Automation):
// a band for when the host's memory was short and one lane per thread the guard froze, hatched while frozen, with the
// freeze's number at its start. Plain SVG that scales to the width; the colours are the app's tokens.

import { useId } from "preact/hooks";
import { GUARD_WINDOW, guardChart, type Guard } from "./guard.ts";

const W = 360; // the phone is 360 wide: the viewBox is that, so the figures are 10 px there and grow on the desktop
const LANE = 20;
const TOP = 16;
const AXIS = 14;

export function GuardChart({ guard, now, titleOf }: { guard: Guard | undefined; now: number; titleOf: (thread: string) => string }) {
  const id = useId().replace(/[^a-z0-9]/gi, "");
  const c = guardChart(guard, now, titleOf);
  const n = Math.max(1, c.lanes.length);
  const H = TOP + n * LANE + AXIS;
  const x = (v: number) => (v * W).toFixed(1);
  const ticks = [0, 5, 10, 15].map((m) => ({ m, v: 1 - (m * 60_000) / GUARD_WINDOW }));
  return (
    <figure class="gchart" data-guard-chart>
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
