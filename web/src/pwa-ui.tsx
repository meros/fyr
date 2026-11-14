// The bell (Web Push) and Install controls, and the Notifications dialog (SPEC 0.5 V0.15).

import { useState } from "preact/hooks";
import { Modal } from "./dialogs.tsx";
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

export interface PushDialogProps {
  status: PushStatus;
  reason: string | null;
  onClose(): void;
  onOn(): Promise<string | null>;
  onOff(): Promise<string | null>;
  onTest(): Promise<string | null>;
}

export function PushDialog(p: PushDialogProps) {
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ text: string; err: boolean } | null>(null);
  const run = async (fn: () => Promise<string | null>, okText?: string) => {
    setBusy(true);
    setMsg(null);
    const err = await fn();
    setBusy(false);
    if (err) setMsg({ text: err, err: true });
    else if (okText) setMsg({ text: okText, err: false });
  };
  return (
    <Modal title="Notifications" onClose={p.onClose}>
      <div class="db">
        <p class="lede">
          {p.status === "on" && "This device gets a notification when a thread asks you something or fails. A newer one replaces the older one for the same thread."}
          {p.status === "off" && "Get a notification on this device when a thread asks you something or fails."}
          {p.status === "denied" && "Notifications are blocked for this site. Allow them in the browser's site settings, then reload Fyr."}
          {p.status === "unsupported" && (p.reason ?? "Notifications are not available in this browser.")}
        </p>
        {msg && <div class={msg.err ? "err prose" : "lede"}>{msg.text}</div>}
      </div>
      <div class="df">
        {p.status === "on" && (
          <>
            <button class="btn" disabled={busy} onClick={() => void run(p.onTest, "Sent. It should arrive in a few seconds.")}>
              Send test
            </button>
            <button class="btn" disabled={busy} onClick={() => void run(p.onOff)}>
              Turn off
            </button>
          </>
        )}
        {p.status === "off" && (
          <button class="btn p" disabled={busy} onClick={() => void run(p.onOn)}>
            Turn on
          </button>
        )}
        {(p.status === "denied" || p.status === "unsupported") && (
          <button class="btn" onClick={p.onClose}>
            Close
          </button>
        )}
      </div>
    </Modal>
  );
}
