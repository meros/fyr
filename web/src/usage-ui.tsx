/** V0.usage-ledger: the thread's tokens in one chip, in the desktop thread header and the phone thread header alike.
 *  The phone's chip takes its chip row's class (m-chip): its height is the phone's, the only difference.
 *  V0.usage-page: the token bars, one series (the total) in one hue, shared by Settings → Usage and the status home;
 *  each bar names its time and count on hover, and the page's rows are the table view. */
import { type Usage, tokens, usageLabel, usageTitle } from "./usage.ts";

export function UsageChip({ u, phone }: { u: Usage | undefined; phone?: boolean }) {
  const label = usageLabel(u);
  if (!label) return null;
  return (
    <span class={phone ? "m-chip usagec" : "chip m usagec"} title={usageTitle(u)}>
      <span>{label}</span>
    </span>
  );
}

/** Bars of one series: the tallest fills the height. `at` names each bar in its tooltip; `ticks` are a few bars' labels
 *  under the axis, each centred under its bar (never one per bar). */
export function UsageBars({ bars, label, ticks, cls }: { bars: { at: string; total: number; now?: boolean }[]; label: string; ticks?: number[]; cls?: string }) {
  const max = Math.max(1, ...bars.map((b) => b.total));
  return (
    <div class={`ug-chart${cls ? ` ${cls}` : ""}`}>
      <div class="ug-days" role="img" aria-label={label}>
        {bars.map((b) => (
          <span class={`ug-day${b.now ? " now" : ""}`} title={`${b.at}: ${tokens(b.total)} tokens`}>
            <i style={{ height: b.total ? `${Math.max(2, Math.round((b.total / max) * 100))}%` : "0" }} />
          </span>
        ))}
      </div>
      {ticks && ticks.length > 0 && (
        <div class="ug-ticks" aria-hidden="true">
          {ticks.map((i) => (
            <span style={{ left: `${((i + 0.5) / bars.length) * 100}%` }}>{bars[i]?.at}</span>
          ))}
        </div>
      )}
    </div>
  );
}
