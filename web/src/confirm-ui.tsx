// SPEC 0.5 V0.68: Fyr's own confirmation, never the browser's confirm(). One call, `await ask({...})`, from any
// component under the app; a bottom sheet on the phone, a dialog on the desktop. The words come from confirm.ts.

import { createContext, type VNode } from "preact";
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import type { Ask } from "./confirm.ts";
import { copyToClipboard } from "./copy.ts";
import { BottomSheet, DialogScrim } from "./motion-ui.tsx";

type AskFn = (a: Ask) => Promise<boolean>;

const ConfirmContext = createContext<AskFn>(async () => false);
export const ConfirmProvider = ConfirmContext.Provider;
/** The confirmation: resolves true when the owner pressed the primary button. */
export const useAsk = () => useContext(ConfirmContext);

/** The app's one confirmation: `ask` for the context, `view` to render over everything. A second ask cancels the first. */
export function useConfirmHost(phone: boolean): { ask: AskFn; view: VNode | null } {
  const [open, setOpen] = useState<{ a: Ask; done(ok: boolean): void } | null>(null);
  const openRef = useRef(open);
  openRef.current = open;
  const ask = useRef<AskFn>(
    (a) =>
      new Promise((resolve) => {
        openRef.current?.done(false);
        const entry = { a, done: (ok: boolean) => (resolve(ok), setOpen((cur) => (cur === entry ? null : cur))) };
        setOpen(entry);
      }),
  ).current;
  return { ask, view: open ? <ConfirmView key={open.a.title} a={open.a} phone={phone} onDone={open.done} /> : null };
}

function ConfirmView({ a, phone, onDone }: { a: Ask; phone: boolean; onDone(ok: boolean): void }) {
  const box = useRef<HTMLDivElement>(null);
  const ok = useRef<HTMLButtonElement>(null);
  const [full, setFull] = useState(false);
  const [copied, setCopied] = useState(false);
  const [pick, setPick] = useState(a.choice?.value ?? "");
  const [why, setWhy] = useState(a.reason?.value ?? "");
  const doneRef = useRef(onDone);
  doneRef.current = onDone;

  useEffect(() => {
    ok.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      e.stopPropagation(); // the inbox keys must not act under the confirmation
      const k = e.key;
      if (k === "Escape") {
        e.preventDefault();
        doneRef.current(false);
      } else if (k === "Enter" && !phone && !e.isComposing) {
        const el = document.activeElement as HTMLElement | null;
        if (el?.tagName === "BUTTON" && el !== ok.current && box.current?.contains(el)) return; // a focused Cancel or Copy takes Enter itself
        e.preventDefault();
        doneRef.current(true);
      } else if (k === "Tab") {
        // Focus stays inside.
        const list = [...(box.current?.querySelectorAll<HTMLElement>("button, input") ?? [])];
        if (!list.length) return;
        e.preventDefault();
        const i = list.indexOf(document.activeElement as HTMLElement);
        list[(i + (e.shiftKey ? -1 : 1) + list.length) % list.length].focus();
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, []);

  const c = a.command;
  const copy = async () => {
    if (c && (await copyToClipboard(c.full))) {
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    }
  };
  const body = (
    <div class="cfm-b">
      {a.text && <p class="cfm-text">{a.text}</p>}
      {a.choice && (
        <div class="cfm-pick" role="radiogroup" aria-label={a.choice.label}>
          <span class="cfm-in">{a.choice.label}</span>
          {a.choice.options.map((o) => (
            <button
              key={o.value}
              role="radio"
              aria-checked={pick === o.value}
              class={`cfm-opt${pick === o.value ? " on" : ""}`}
              onClick={() => {
                a.choice!.value = o.value;
                setPick(o.value);
              }}
            >
              {o.label}
            </button>
          ))}
        </div>
      )}
      {a.reason && (
        <label class="cfm-why">
          <span class="cfm-in">{a.reason.label}</span>
          <input
            value={why}
            maxLength={a.reason.max}
            onInput={(e) => {
              const v = (e.target as HTMLInputElement).value;
              a.reason!.value = v;
              setWhy(v);
            }}
          />
        </label>
      )}
      {c && (
        <div class="cfm-cmd">
          <button class="cfm-code" aria-expanded={full} title={full ? undefined : [c.full, c.folderFull && `in ${c.folderFull}`].filter(Boolean).join("\n")} onClick={() => setFull(!full)}>
            <code>{full ? c.full : c.short}</code>
            {c.folder && <span class="cfm-in">in {full ? c.folderFull : c.folder}</span>}
            {c.note && <span class="cfm-in">{c.note}</span>}
          </button>
          <button class="cfm-copy" onClick={() => void copy()}>
            {copied ? "Copied" : "Copy"}
          </button>
        </div>
      )}
    </div>
  );
  const primary = `${a.danger ? " danger" : ""}`;

  if (phone) {
    return (
      <BottomSheet onClose={() => onDone(false)} label={a.title} cls="cfm" scrimCls="cfm-scrim" role="alertdialog" panelRef={box}>
        <div class="m-shh cfm-t">{a.title}</div>
        {body}
        <div class="cfm-f">
          <button class="m-bt" onClick={() => onDone(false)}>
            Cancel
          </button>
          <button class={`m-bt p${primary}`} ref={ok} onClick={() => onDone(true)}>
            {a.confirm}
          </button>
        </div>
      </BottomSheet>
    );
  }
  return (
    <DialogScrim label={a.title} cls="cfm" scrimCls="cfm-scrim" role="alertdialog" panelRef={box}>
      <div class="dh">
        <b class="cfm-t">{a.title}</b>
      </div>
      {body}
      <div class="df">
        <button class="btn" onClick={() => onDone(false)}>
          Cancel <kbd>Esc</kbd>
        </button>
        <button class={`btn p${primary}`} ref={ok} onClick={() => onDone(true)}>
          {a.confirm} <kbd>⏎</kbd>
        </button>
      </div>
    </DialogScrim>
  );
}
