// The state icons from design/screens-v1.html (shape-distinct, readable in grayscale).
// `--knock` is the surface colour used for cut-outs.

import type { AttentionState } from "./types.ts";
import { stateLabel } from "./rows.ts";

const CLS: Record<AttentionState, string> = {
  asking: "s-ask",
  failed: "s-fail",
  blocked: "s-block",
  done: "s-done",
  working: "s-work",
  waiting: "s-wait",
  seen: "s-seen",
  snoozed: "s-snz",
  settled: "s-set",
  archived: "s-arch",
};

export function stateClass(s: AttentionState): string {
  return CLS[s] ?? "";
}

function shape(s: AttentionState) {
  switch (s) {
    case "asking":
      return (
        <>
          <circle cx="8" cy="8" r="7" fill="currentColor" />
          <path d="M6.2 6.1a1.9 1.9 0 1 1 2.6 1.8c-.5.2-.8.6-.8 1.1v.4" fill="none" stroke="var(--knock)" stroke-width="1.5" stroke-linecap="round" />
          <circle cx="8" cy="11.6" r=".9" fill="var(--knock)" />
        </>
      );
    case "failed":
      return (
        <>
          <path d="M8 .9 15.1 8 8 15.1.9 8Z" fill="currentColor" />
          <path d="M8 4.6v4.2" stroke="var(--knock)" stroke-width="1.7" stroke-linecap="round" />
          <circle cx="8" cy="11.2" r=".95" fill="var(--knock)" />
        </>
      );
    case "blocked":
      return (
        <>
          <rect x="2" y="2" width="12" height="12" rx="3" fill="none" stroke="currentColor" stroke-width="1.7" />
          <rect x="5" y="7" width="6" height="2" rx="1" fill="currentColor" />
        </>
      );
    case "done":
      return <circle cx="8" cy="8" r="4" fill="currentColor" />;
    case "working":
      return (
        <>
          <circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" stroke-opacity=".3" stroke-width="1.7" />
          <path d="M8 2.5a5.5 5.5 0 0 1 5.5 5.5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" />
        </>
      );
    case "waiting":
      return <circle cx="8" cy="8" r="5.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-dasharray="2.2 2.1" />;
    case "seen":
      return <rect x="4" y="4" width="8" height="8" rx="2" fill="none" stroke="currentColor" stroke-width="1.5" />;
    case "snoozed":
      return <path d="M10.5 2.6a5.6 5.6 0 1 0 3 8.9A4.6 4.6 0 0 1 10.5 2.6Z" fill="currentColor" />;
    case "settled":
      return <path d="M3.2 8.4 6.5 11.6 12.8 4.6" fill="none" stroke="currentColor" stroke-width="1.9" stroke-linecap="round" stroke-linejoin="round" />;
    case "archived":
      return (
        <>
          <rect x="2" y="3" width="12" height="3.5" rx="1" fill="none" stroke="currentColor" stroke-width="1.4" />
          <path d="M3.2 6.5V12a1 1 0 0 0 1 1h7.6a1 1 0 0 0 1-1V6.5M6.3 9h3.4" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" />
        </>
      );
    default:
      return null;
  }
}

export function StateIcon({ state, size = 16 }: { state: AttentionState; size?: number }) {
  return (
    <svg class={`ic ${stateClass(state)}`} width={size} height={size} viewBox="0 0 16 16" aria-label={stateLabel(state)} role="img">
      {shape(state)}
    </svg>
  );
}
