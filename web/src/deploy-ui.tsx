// SPEC 0.5 V0.39: the update banner. A spinner and a sentence while Fyr updates; green when it is
// updated; red with the reason when it failed. The page reloads by itself onto the new build.
import { useEffect, useState } from "preact/hooks";
import { deployLine, type DeployProgress } from "./deploy.ts";
import type { ThreadDTO } from "./types.ts";

export function DeployBanner({ p, threads, conn, phone }: { p: DeployProgress | null; threads: Record<string, ThreadDTO>; conn: string; phone?: boolean }) {
  const [now, setNow] = useState(Date.now());
  const [hidden, setHidden] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  useEffect(() => {
    const iv = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(iv);
  }, []);
  const line = deployLine(p, threads, now, conn);
  const key = p ? `${p.phase}:${p.build ?? ""}:${p.at ?? ""}` : "";
  if (!line || hidden === key) return null;
  return (
    <div class={`dbn ${line.tone}${phone ? " m" : ""}`} role="status" aria-live="polite">
      {line.tone === "busy" ? <span class="dbn-spin" aria-hidden="true" /> : <span class="dbn-dot" aria-hidden="true" />}
      <span class="dbn-t">{line.text}</span>
      {line.more && (
        <button class="dbn-b" onClick={() => setOpen(!open)}>
          {open ? "Hide" : "Details"}
        </button>
      )}
      {line.tone !== "busy" && (
        <button class="dbn-b" aria-label="Dismiss" onClick={() => setHidden(key)}>
          ×
        </button>
      )}
      {open && line.more && <pre class="dbn-more">{line.more}</pre>}
    </div>
  );
}
