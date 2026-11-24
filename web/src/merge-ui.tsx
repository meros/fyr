// V0.dialogs-review: the one Merge dialog of both fronts. Every merge that asks first asks here, wherever it starts:
// the PR tab's Merge…, a change card's Merge, the agent's merge ask. One call, `await merge(ask)`, from any component
// under the app (as confirm-ui.tsx's `ask`); a dialog on the desktop, the same dialog as a bottom sheet on the phone.
// The words are pr.ts's (`MergeAsk`, `mergeTitle`, `mergeConfirm`).

import { createContext, type VNode } from "preact";
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import { MERGE_METHODS, mergeConfirm, mergeMethodLine, mergeTitle, notMerged, rememberedMergeMethod, rememberMergeMethod, shortHead, type MergeAsk, type MergeMethod } from "./pr.ts";
import { sensitiveLine } from "./sensitive.ts";
import { browserStorage } from "./prefs.ts";
import { Modal } from "./dialogs.tsx";

type MergeFn = (a: MergeAsk) => Promise<boolean>;

const MergeContext = createContext<MergeFn>(async () => false);
export const MergeProvider = MergeContext.Provider;
/** The Merge dialog: resolves true when the merge started, false when the owner closed it. */
export const useMerge = () => useContext(MergeContext);

/** The app's one Merge dialog: `ask` for the context, `view` to render over everything. A second ask closes the first. */
export function useMergeHost(phone: boolean): { ask: MergeFn; view: VNode | null } {
  const [open, setOpen] = useState<{ a: MergeAsk; done(ok: boolean): void } | null>(null);
  const openRef = useRef(open);
  openRef.current = open;
  const ask = useRef<MergeFn>(
    (a) =>
      new Promise((resolve) => {
        openRef.current?.done(false);
        const entry = { a, done: (ok: boolean) => (resolve(ok), setOpen((cur) => (cur === entry ? null : cur))) };
        setOpen(entry);
      }),
  ).current;
  return { ask, view: open ? <MergeDialog a={open.a} phone={phone} onDone={open.done} /> : null };
}

/** V0.dialogs-review (1): while it is open no key of the app acts under it (app-keyboard.tsx reads the open panels),
 *  and Esc and Enter are its own. (27): it starts on the method this repository was merged with last on this device,
 *  and stays open with the host's error, on a line of its own, when the merge does not start. A sensitive merge is
 *  red and starts with the focus on Cancel, so an Enter on its way elsewhere does not merge; a held Enter that repeats
 *  into the dialog is no answer at all. The foot names the keys
 *  like the confirmation; not on the phone, which has no keyboard. */
function MergeDialog({ a, phone, onDone }: { a: MergeAsk; phone: boolean; onDone(ok: boolean): void }) {
  const [method, setMethod] = useState<MergeMethod>(() => rememberedMergeMethod(browserStorage(), a.repo ?? ""));
  const [sending, setSending] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const btn = useRef<HTMLButtonElement>(null);
  const cancel = useRef<HTMLButtonElement>(null);
  const goRef = useRef<() => void>(() => {});
  const doneRef = useRef(onDone);
  doneRef.current = onDone;
  /* eslint-disable react-hooks/exhaustive-deps -- the focus and the key handler are set once, when the dialog opens; the refs hold the newest callbacks */
  useEffect(() => {
    (a.sensitive ? cancel : btn).current?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        doneRef.current(false);
      } else if (e.key === "Enter" && !e.isComposing) {
        // A key held since before the dialog opened is not an answer to it (confirm-ui.tsx has the same guard): the
        // Enter that pressed "Merge…" repeats into the dialog, whose focus is on the button that merges.
        if (e.repeat) return (e.preventDefault(), e.stopPropagation());
        // Enter merges with the method shown, from a method's radio too; a focused Cancel or × takes Enter itself.
        const el = document.activeElement as HTMLElement | null;
        if (el?.tagName === "BUTTON" && el !== btn.current) return;
        e.preventDefault();
        e.stopPropagation();
        goRef.current();
      }
    };
    document.addEventListener("keydown", key, true);
    return () => document.removeEventListener("keydown", key, true);
  }, []);
  /* eslint-enable react-hooks/exhaustive-deps */
  const go = async () => {
    if (sending) return;
    setSending(true);
    setErr(null);
    const e = await a.run(a.choose ? method : undefined);
    if (!e) {
      if (a.choose && a.repo) rememberMergeMethod(browserStorage(), a.repo, method);
      return onDone(true);
    }
    setSending(false);
    setErr(e);
  };
  goRef.current = () => void go();
  return (
    <Modal title={mergeTitle(a)} onClose={() => onDone(false)}>
      <div class="db prv-dlg" onKeyDown={(e) => e.stopPropagation()}>
        {a.sensitive && (
          <p class="chsens" role="note">
            {sensitiveLine(a.sensitive)}
          </p>
        )}
        {a.title && <p class="prv-what">{a.title}</p>}
        <p class="lede">
          {a.head && a.base ? (
            <>
              Merges <code>{a.head}</code> into <code>{a.base}</code> on GitHub.
            </>
          ) : (
            "Merges this PR on GitHub."
          )}{" "}
          {a.sha && (
            <>
              Only commit <code>{shortHead(a.sha)}</code>: if the branch moves first, Fyr refuses.{" "}
            </>
          )}
          The result shows in the conversation.
        </p>
        {a.choose ? (
          <div class="prv-methods" role="radiogroup" aria-label="Merge method">
            {MERGE_METHODS.map((m) => (
              <label class={`prv-m${method === m.id ? " on" : ""}`} key={m.id}>
                <input type="radio" name="merge-method" value={m.id} checked={method === m.id} onChange={() => setMethod(m.id)} />
                <span>
                  <b>{m.label}</b>
                  <span class="prv-mh">{m.hint}</span>
                </span>
              </label>
            ))}
          </div>
        ) : (
          <p class="lede">{mergeMethodLine(a)}</p>
        )}
      </div>
      {/* The host's refusal has a line of its own above the buttons on both fronts: beside them it has only the width
          the two buttons leave, which in the desktop's narrow dialog is one letter. */}
      <div class="df prv-df">
        {err && (
          <div class="dfn">
            <span class="err" role="alert">
              {notMerged(err)}
            </span>
          </div>
        )}
        <button ref={cancel} class="btn" onClick={() => onDone(false)}>
          Cancel{!phone && <kbd>Esc</kbd>}
        </button>
        <button ref={btn} class={`btn p${a.sensitive ? " danger" : ""}`} disabled={sending} onClick={() => void go()}>
          {sending ? "Merging…" : mergeConfirm(a, method)}
          {/* the key hint only where Enter merges: a sensitive merge starts on Cancel, and Enter there cancels */}
          {!phone && !sending && !a.sensitive && <kbd>⏎</kbd>}
        </button>
      </div>
    </Modal>
  );
}
