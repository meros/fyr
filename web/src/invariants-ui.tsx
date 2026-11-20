// V0.load-invariants: a broken load invariant as a card at the top of Needs you, one component for the desktop list
// and the phone inbox (CLAUDE.md): the invariant, when, the evidence, the guard's log lines folded, and two buttons.
// Fix it starts a thread in the host's Fyr checkout with the evidence; Dismiss closes the card for a day. The phone's
// buttons are 44 px (phone.css: touch); nothing else differs. The texts are invariants.ts's.
import { useState } from "preact/hooks";
import { useApi } from "./api-context.ts";
import { refreshLoad, useLoad } from "./load-ui.tsx";
import { cardHead, cardWhen, fixState, invariantCards, linesLabel, type HostCard } from "./invariants.ts";

/** The open cards of every host, drawn under the Needs you header; nothing when none is open. */
export function InvariantCards({ phone, now, onOpen }: { phone: boolean; now: number; /** opens the fix thread */ onOpen?: (id: string) => void }) {
  const load = useLoad();
  const cards = invariantCards(load);
  if (!cards.length) return null;
  return (
    <div class={`invc-list${phone ? " m" : ""}`}>
      {cards.map((c) => (
        <InvariantCardView key={`${c.host}:${c.card.id}`} c={c} hosts={load.length} now={now} onOpen={onOpen} />
      ))}
    </div>
  );
}

function InvariantCardView({ c, hosts, now, onOpen }: { c: HostCard; hosts: number; now: number; onOpen?: (id: string) => void }) {
  const api = useApi();
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const fix = fixState(c);
  const run = async (action: string) => {
    if (!c.hostId) return setErr("This host reports no install id, so nothing can be sent to it until it updates");
    setBusy(true);
    const r = await api.command(c.hostId, action, undefined, { id: c.card.id });
    setBusy(false);
    setErr(r.ok ? null : (r.error ?? "That did not work"));
    refreshLoad();
    if (r.ok && action === "host.invariantFix" && r.threadId) onOpen?.(r.threadId);
  };
  const lines = linesLabel(c.card);
  return (
    <div class="invc" role="group" aria-label={cardHead(c, hosts)}>
      <div class="invc-h">
        <span class="invc-t">{cardHead(c, hosts)}</span>
        <span class="invc-w">{cardWhen(c.card, now)}</span>
      </div>
      <div class="invc-s">{c.card.text}</div>
      <div class="invc-e">{c.card.evidence}</div>
      {lines && (
        <details class="invc-l">
          <summary>{lines}</summary>
          <pre>{(c.card.lines ?? []).join("\n")}</pre>
        </details>
      )}
      <div class="invc-n">
        {fix.note}
        {err && <span class="err"> · {err}</span>}
      </div>
      <div class="invc-a">
        <button type="button" class="btn p" disabled={busy || !fix.can} title={fix.note} onClick={() => void run("host.invariantFix")}>
          Fix it
        </button>
        <button type="button" class="btn" disabled={busy} title="Close the card; this invariant shows no card for a day" onClick={() => void run("host.invariantDismiss")}>
          Dismiss
        </button>
      </div>
    </div>
  );
}
