// SPEC 0.5 V0.39, amended by V0.118: the update status. One line in the layout (the top of the page on the desktop,
// below the safe area on the phone), never over a control: it pushes the content down instead. A spinner and a
// sentence while Fyr updates, a press shows the whole sentence and the details; a success shows a few seconds and goes;
// a failure stays, in red, with Details, until it is dismissed. Desktop and phone share this component (CLAUDE.md).
import { useEffect, useState } from "preact/hooks";
import { storedGet, storedSet } from "./prefs.ts";
import { canRestartNow, deployLine, deployShown, OK_SHOW_MS, type DeployProgress } from "./deploy.ts";
import type { Thread } from "./types.ts";

const DISMISSED_KEY = "fyr.deploy.dismissed";

/** #85: the update result the owner dismissed on this device; it outlives a reload and the next start. */
function readDismissed(): string | null {
  return storedGet(DISMISSED_KEY);
}

function saveDismissed(key: string): void {
  storedSet(DISMISSED_KEY, key);
}

export function DeployBanner({
  p,
  threads,
  conn,
  phone,
  onRestartNow,
}: {
  p: DeployProgress | null;
  threads: Record<string, Thread>;
  conn: string;
  /** 44 px targets (touch) */
  phone?: boolean;
  /** V0.83: confirm, then stop the drain and switch now */
  onRestartNow?: () => void;
}) {
  const [now, setNow] = useState(Date.now());
  const [hidden, setHidden] = useState<string | null>(readDismissed); // #85: a dismissed result stays dismissed on reload
  const [open, setOpen] = useState(false);
  // #85: a result is one update: its phase and build, so the same result read again after a restart stays dismissed
  const key = p ? `${p.phase}:${p.build ?? ""}` : "";
  const [seen, setSeen] = useState<{ key: string; at: number } | null>(null); // V0.118 (2): when this page first saw it
  useEffect(() => {
    const iv = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(iv);
  }, []);
  useEffect(() => {
    if (key) setSeen((s) => (s?.key === key ? s : { key, at: Date.now() }));
    setOpen(false);
  }, [key]);
  const line = deployLine(p, threads, now, conn);
  const seenAt = seen?.key === key ? seen.at : null;
  // V0.118 (2): a success goes by itself after a few seconds, and stays gone after a reload (#85's key)
  useEffect(() => {
    if (line?.tone !== "ok" || seenAt === null || hidden === key) return;
    const tm = setTimeout(() => (setHidden(key), saveDismissed(key)), Math.max(0, OK_SHOW_MS - (Date.now() - seenAt)));
    return () => clearTimeout(tm);
  }, [line?.tone, seenAt, key, hidden]);
  if (!line || hidden === key || !deployShown(p, line, seenAt, now)) return null;
  return (
    <div class={`dbn ${line.tone}${phone ? " m" : ""}${open ? " open" : ""}`} role="status" aria-live="polite">
      <button class="dbn-x" aria-expanded={open} title={open ? "Show less" : line.text} onClick={() => setOpen(!open)}>
        {line.tone === "busy" ? <span class="dbn-spin" aria-hidden="true" /> : <span class="dbn-dot" aria-hidden="true" />}
        <span class="dbn-t">{line.text}</span>
      </button>
      {onRestartNow && canRestartNow(p, conn) && (
        <button class="dbn-b dbn-now" title="Restart now. Running turns stop and pick up again after the restart." onClick={onRestartNow}>
          Restart now
        </button>
      )}
      {line.more && (
        <button class="dbn-b" onClick={() => setOpen(!open)}>
          {open ? "Hide" : "Details"}
        </button>
      )}
      {line.tone !== "busy" && (
        <button class="dbn-b" aria-label="Dismiss" title="Dismiss" onClick={() => (setHidden(key), saveDismissed(key))}>
          ×
        </button>
      )}
      {open && line.more && <pre class="dbn-more">{line.more}</pre>}
    </div>
  );
}
