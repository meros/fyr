/** V0.usage-ledger: the thread's tokens in one chip, in the desktop thread header and the phone thread header alike.
 *  The phone's chip takes its chip row's class (m-chip): its height is the phone's, the only difference. */
import { type Usage, usageLabel, usageTitle } from "./usage.ts";

export function UsageChip({ u, phone }: { u: Usage | undefined; phone?: boolean }) {
  const label = usageLabel(u);
  if (!label) return null;
  return (
    <span class={phone ? "m-chip usagec" : "chip m usagec"} title={usageTitle(u)}>
      <span>{label}</span>
    </span>
  );
}
