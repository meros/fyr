// SPEC 0.5 V0.39: the update banner. A spinner and a sentence while Fyr updates; green when it is
// updated; red with the reason when it failed. The page reloads by itself onto the new build.
import { useEffect, useState } from "preact/hooks";
import { bannerFolded, canRestartNow, deployLine, updateKey, type DeployProgress } from "./deploy.ts";
import type { Thread } from "./types.ts";

const DISMISSED_KEY = "fyr.deploy.dismissed";

/** #85: the update result the owner dismissed on this device; it outlives a reload and the next start. */
function readDismissed(): string | null {
  try {
    return localStorage.getItem(DISMISSED_KEY);
  } catch {
    return null;
  }
}

function saveDismissed(key: string): void {
  try {
    localStorage.setItem(DISMISSED_KEY, key);
  } catch {
    /* storage blocked: this page only */
  }
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
  phone?: boolean;
  /** V0.83: confirm, then stop the drain and switch now */
  onRestartNow?: () => void;
}) {
  const [now, setNow] = useState(Date.now());
  const [hidden, setHidden] = useState<string | null>(readDismissed); // #85: a dismissed result stays dismissed on reload
  const [open, setOpen] = useState(false);
  const [folded, setFolded] = useState<string | null>(null); // #81: the update the owner folded
  useEffect(() => {
    const iv = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(iv);
  }, []);
  const line = deployLine(p, threads, now, conn);
  // #85: a result is one update: its phase and build, so the same result read again after a restart stays dismissed
  const key = p ? `${p.phase}:${p.build ?? ""}` : "";
  if (!line || hidden === key) return null;
  if (bannerFolded(p, line.tone, folded)) {
    return (
      <button class={`dbn dbn-chip${phone ? " m" : ""}`} title={line.text} aria-label={`${line.text}. Show the update`} onClick={() => setFolded(null)}>
        <span class="dbn-spin" aria-hidden="true" />
        <span class="dbn-t">Updating</span>
      </button>
    );
  }
  return (
    <div class={`dbn ${line.tone}${phone ? " m" : ""}`} role="status" aria-live="polite">
      {line.tone === "busy" ? <span class="dbn-spin" aria-hidden="true" /> : <span class="dbn-dot" aria-hidden="true" />}
      <span class="dbn-t">{line.text}</span>
      {line.more && (
        <button class="dbn-b" onClick={() => setOpen(!open)}>
          {open ? "Hide" : "Details"}
        </button>
      )}
      {onRestartNow && canRestartNow(p, conn) && (
        <button class="dbn-b dbn-now" title="Stop waiting: the running turns end and each gets “continue” after the restart" onClick={onRestartNow}>
          Restart now
        </button>
      )}
      {line.tone === "busy" && (
        <button class="dbn-b" aria-label="Fold" title="Fold to a small chip" onClick={() => setFolded(updateKey(p))}>
          –
        </button>
      )}
      {line.tone !== "busy" && (
        <button class="dbn-b" aria-label="Dismiss" onClick={() => (setHidden(key), saveDismissed(key))}>
          ×
        </button>
      )}
      {open && line.more && <pre class="dbn-more">{line.more}</pre>}
    </div>
  );
}
