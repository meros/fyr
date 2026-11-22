// SPEC 0.5 V0.147: the stall line, in the update banner's place and style (V0.39): red, Details shows what held the
// lock and where the dump is, × dismisses it on this device. Desktop and phone share this component (CLAUDE.md); the
// phone's targets are 44 px (touch).
import { useState } from "preact/hooks";
import { storedGet, storedSet } from "./prefs.ts";
import { shownStall, stallKey, stallLine, type StallInfo } from "./stall.ts";

const DISMISSED_KEY = "fyr.stall.dismissed";

function readDismissed(): string | null {
  return storedGet(DISMISSED_KEY);
}

export function StallBanner({ stalls, multiHost, phone }: { stalls: StallInfo[]; multiHost: boolean; phone?: boolean }) {
  const [hidden, setHidden] = useState<string | null>(readDismissed);
  const [open, setOpen] = useState(false);
  const s = shownStall(stalls, hidden);
  if (!s) return null;
  const line = stallLine(s, multiHost);
  const dismiss = () => {
    const k = stallKey(s);
    setHidden(k);
    storedSet(DISMISSED_KEY, k);
  };
  return (
    <div class={`dbn fail${phone ? " m" : ""}${open ? " open" : ""}`} role="status" aria-live="polite">
      <button class="dbn-x" aria-expanded={open} title={open ? "Show less" : line.text} onClick={() => setOpen(!open)}>
        <span class="dbn-dot" aria-hidden="true" />
        <span class="dbn-t">{line.text}</span>
      </button>
      <button class="dbn-b" onClick={() => setOpen(!open)}>
        {open ? "Hide" : "Details"}
      </button>
      <button class="dbn-b" aria-label="Dismiss" title="Dismiss" onClick={dismiss}>
        ×
      </button>
      {open && <pre class="dbn-more">{line.more}</pre>}
    </div>
  );
}
