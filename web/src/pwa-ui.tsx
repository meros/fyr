// The bell (Web Push) and Install controls (SPEC 0.5 V0.15). V0.119: the bell opens Settings → This device, where
// Notifications are switched; the Notifications dialog is gone.

import { PUSH_TITLE, type PushStatus } from "./push.ts";

const BELL = (
  <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
    <path d="M4 11.5V7.2a4 4 0 0 1 8 0v4.3l1.2 1.2H2.8Z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
    <path d="M6.6 14.2a1.5 1.5 0 0 0 2.8 0" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
  </svg>
);
const BELL_ON = (
  <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
    <path d="M4 11.5V7.2a4 4 0 0 1 8 0v4.3l1.2 1.2H2.8Z" fill="currentColor" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
    <path d="M6.6 14.2a1.5 1.5 0 0 0 2.8 0" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
  </svg>
);
const BELL_OFF = (
  <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
    <path d="M4 11.5V7.2a4 4 0 0 1 8 0v4.3l1.2 1.2H2.8Z" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linejoin="round" />
    <path d="M2.5 2.5l11 11" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" />
  </svg>
);
const INSTALL = (
  <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
    <rect x="4" y="1.8" width="8" height="12.4" rx="1.8" fill="none" stroke="currentColor" stroke-width="1.4" />
    <path d="M8 4.8v5M6 8l2 2 2-2" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" />
  </svg>
);

export interface PwaControlsProps {
  push: PushStatus;
  /** why push is unavailable; shown as the title. A reason is only rendered as a dimmed bell when `showUnsupported`. */
  pushReason: string | null;
  showUnsupported: boolean;
  canInstall: boolean;
  onBell(): void;
  onInstall(): void;
}

export function PwaControls(p: PwaControlsProps) {
  const hideBell = p.push === "unsupported" && !p.showUnsupported;
  return (
    <>
      {p.canInstall && (
        <button class="ibtn pwa-install" onClick={p.onInstall} title="Install Fyr as an app" aria-label="Install Fyr">
          {INSTALL}
        </button>
      )}
      {!hideBell && (
        <button
          class={`ibtn bell ${p.push}`}
          onClick={p.onBell}
          title={p.push === "unsupported" ? (p.pushReason ?? PUSH_TITLE.unsupported) : PUSH_TITLE[p.push]}
          aria-label={PUSH_TITLE[p.push]}
        >
          {p.push === "on" ? BELL_ON : p.push === "off" ? BELL : BELL_OFF}
        </button>
      )}
    </>
  );
}
