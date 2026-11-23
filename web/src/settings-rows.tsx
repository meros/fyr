// SPEC V0.119 (2): the one set of controls every Settings page and the project page are built from, on the desktop
// and the phone alike: a group of rows, a row, a switch, one segmented control, a text field that saves, the Saved
// mark, and the loading, error and empty states (the one reorder control is reorder-ui.tsx). The phone's sizes (56 px rows, 44 px
// controls, 16 px inputs) come from one rule set under `.set.phone` in app.css, never from a patch per control.

import { createContext, type ComponentChildren } from "preact";
import { useContext, useEffect, useRef, useState } from "preact/hooks";
import { SlideMark } from "./motion-ui.tsx";
import { haptic } from "./native.ts";
import { readFold, writeFold } from "./settings-fold.ts";

/** What every control on a page reads: the page is read-only (no connection), it is the phone, and how to show Saved. */
interface SetEnv {
  readOnly: boolean;
  phone: boolean;
  /** a command answered ok: show "Saved" for 2 s */
  saved(): void;
}
export const SetContext = createContext<SetEnv>({ readOnly: false, phone: false, saved: () => {} });
export const useSet = () => useContext(SetContext);

/** "✓ Saved" for 2 s after a command answered ok; once per page. */
export function useSaved(): { shown: boolean; mark(): void } {
  const [shown, setShown] = useState(false);
  const t = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(t.current), []);
  return {
    shown,
    mark: () => {
      clearTimeout(t.current);
      setShown(true);
      t.current = setTimeout(() => setShown(false), 2000);
    },
  };
}

/** The live region is always there, so a screen reader hears "Saved" when it appears. Its place differs: beside the
 *  pane's title on the desktop, at the right of the header on the phone (the phone's header carries the title). */
export function SavedMark({ shown }: { shown: boolean }) {
  return (
    <span class="savedm" aria-live="polite">
      {shown && <span class="saved">Saved</span>}
    </span>
  );
}

/** How a group folds (V0.automation-folds): its id on this device, open or not by default, and the line its header
 *  shows while it is folded. */
export interface Fold {
  id: string;
  open: boolean;
  summary?: ComponentChildren;
  /** open now whatever the device remembers (a link landed on it) */
  force?: boolean;
}

/** A label, a card of rows, a foot line. Nothing sits above a group; the foot says what the whole group does.
 *  With `fold` the header is a button that folds the card and the foot away (V0.automation-folds). */
export function Group(p: { title?: ComponentChildren; end?: ComponentChildren; foot?: ComponentChildren; footErr?: boolean; danger?: boolean; label?: string; class?: string; fold?: Fold; children: ComponentChildren }) {
  const [stored, setStored] = useState(() => (p.fold ? readFold(p.fold.id, p.fold.open) : true));
  const open = !p.fold || p.fold.force || stored;
  const toggle = () => {
    if (!p.fold) return;
    writeFold(p.fold.id, !open);
    setStored(!open);
  };
  return (
    <section class={`sg${p.danger ? " danger-z" : ""}${p.fold ? " fold" : ""}${open ? "" : " shut"}${p.class ? ` ${p.class}` : ""}`} aria-label={p.label}>
      {(p.title || p.end) && (
        <div class="sg-h">
          {p.fold ? (
            <button type="button" class="sg-fb" aria-expanded={open} onClick={toggle}>
              <span class="sg-chev" aria-hidden="true">{open ? "▾" : "▸"}</span>
              {p.title && <span class="sg-t">{p.title}</span>}
              {!open && p.fold.summary && <span class="sg-sum">{p.fold.summary}</span>}
            </button>
          ) : (
            p.title && <span class="sg-t">{p.title}</span>
          )}
          {open && p.end && <span class="end">{p.end}</span>}
        </div>
      )}
      {open && <div class="sg-c">{p.children}</div>}
      {open && p.foot && <div class={`sg-f${p.footErr ? " err" : ""}`}>{p.foot}</div>}
    </section>
  );
}

export interface RowProps {
  title?: ComponentChildren;
  desc?: ComponentChildren;
  /** the row's error, in the host's words; it replaces the description and marks the row */
  error?: string | null;
  end?: ComponentChildren;
  /** before the text: a grip, a star, a dot */
  lead?: ComponentChildren;
  /** on the phone the control goes under the label (two selects, a wide field) */
  stack?: boolean;
  /** the row's body is its children, top to bottom (a login panel, quiet hours) */
  col?: boolean;
  /** the whole row is a button (a list row that opens a page) */
  onClick?(): void;
  /** the row is a label (a switch row: the whole row is the target) */
  label?: boolean;
  class?: string;
  /** the title in mono (a repo, a host) */
  mono?: boolean;
  /** one line with an ellipsis (a name in a list) */
  ell?: boolean;
  /** the end may grow (a text field) */
  wide?: boolean;
  data?: Record<string, string>;
  /** the root element (the reorder control measures rows) */
  rowRef?(el: HTMLElement | null): void;
  children?: ComponentChildren;
}

/** A title, help text under it, the control at the end. */
export function Row(p: RowProps) {
  const cls = `sr${p.stack ? " stack" : ""}${p.col ? " col" : ""}${p.error ? " bad" : ""}${p.class ? ` ${p.class}` : ""}`;
  const body = (
    <>
      {p.lead}
      {(p.title !== undefined || p.desc || p.error) && (
        <span class="sr-b">
          {p.title !== undefined && <span class={`sr-t${p.mono ? " mono" : ""}${p.ell ? " ell" : ""}`}>{p.title}</span>}
          {p.error ? (
            <span class="sr-d err" role="alert">
              {p.error}
            </span>
          ) : (
            p.desc && <span class="sr-d">{p.desc}</span>
          )}
        </span>
      )}
      {p.children}
      {p.end && <span class={`sr-e${p.wide ? " wide" : ""}`}>{p.end}</span>}
    </>
  );
  const data = Object.fromEntries(Object.entries(p.data ?? {}).map(([k, v]) => [`data-${k}`, v]));
  if (p.onClick)
    return (
      <button type="button" class={cls} onClick={p.onClick} ref={p.rowRef} {...data}>
        {body}
      </button>
    );
  if (p.label)
    return (
      <label class={cls} ref={p.rowRef} {...data}>
        {body}
      </label>
    );
  return (
    <div class={cls} ref={p.rowRef} {...data}>
      {body}
    </div>
  );
}

/** On or off. `--ink` when on, in both themes: not a link colour and not "passing". */
export function Switch({ on, label, disabled, onChange }: { on: boolean; label: string; disabled?: boolean; onChange(on: boolean): void }) {
  const env = useSet();
  return (
    <button
      type="button"
      class="swt"
      role="switch"
      aria-checked={on}
      aria-label={label}
      disabled={disabled || env.readOnly}
      onClick={(e) => {
        e.preventDefault(); // inside a label: one toggle, not two
        haptic();
        onChange(!on);
      }}
    />
  );
}

/** The one segmented control: pick one of two to four. */
export function Seg<T extends string>({ value, options, label, onChange, disabled }: { value: T; options: { value: T; label: ComponentChildren }[]; label: string; onChange(v: T): void; disabled?: boolean }) {
  const env = useSet();
  return (
    <span class="seg" role="radiogroup" aria-label={label}>
      <SlideMark at={value} />
      {options.map((o) => (
        <button type="button" role="radio" aria-checked={o.value === value} class={o.value === value ? "on" : ""} disabled={disabled || env.readOnly} onClick={() => onChange(o.value)}>
          {o.label}
        </button>
      ))}
    </span>
  );
}

interface SaveFieldProps {
  value: string;
  /** the error, or null; the text stays on an error */
  onSave(v: string): Promise<string | null>;
  label: string;
  mono?: boolean;
  /** a class on the input (a number field's width) and the phone's number keyboard */
  cls?: string;
  numeric?: boolean;
  placeholder?: string;
  type?: "text" | "password";
  /** after a save the field is empty again (Add a model id, a repo) */
  clears?: boolean;
  /** the button's word ("Save", "Add", "Save key") and whether it is the page's primary */
  button?: string;
  primary?: boolean;
  /** the button shows even while the text equals the value (disabled) */
  alwaysButton?: boolean;
  autoFocus?: boolean;
  /** the row shows this error */
  onError?(e: string | null): void;
  /** a check before the save; a refusal is shown on the row without a command */
  check?(v: string): string | null;
}

/** A text field that saves on Enter. Save shows beside it only while the text differs; Esc puts the text back. */
export function SaveField(p: SaveFieldProps) {
  const env = useSet();
  const [text, setText] = useState(p.value);
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => setText(p.value), [p.value]);
  useEffect(() => {
    if (p.autoFocus && !env.phone) ref.current?.focus();
  }, []);
  const differs = text !== p.value && !(p.clears && !text.trim());
  const save = async () => {
    if (!differs || busy) return;
    const bad = p.check?.(text) ?? null;
    if (bad) return p.onError?.(bad);
    setBusy(true);
    const e = await p.onSave(text);
    setBusy(false);
    p.onError?.(e);
    if (!e && p.clears) setText("");
  };
  return (
    <span class="inl">
      <input
        ref={ref}
        class={[p.mono && "mono", p.cls].filter(Boolean).join(" ") || undefined}
        type={(p.type ?? "text") as "text"}
        inputMode={p.numeric ? "numeric" : undefined}
        value={text}
        placeholder={p.placeholder}
        aria-label={p.label}
        autocomplete="off"
        spellcheck={false}
        disabled={env.readOnly}
        onInput={(e) => {
          setText((e.target as HTMLInputElement).value);
          p.onError?.(null);
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.isComposing) {
            e.preventDefault();
            void save();
          } else if (e.key === "Escape" && text !== p.value) {
            // Esc puts the text back first; only the next Esc leaves the page.
            e.preventDefault();
            e.stopPropagation();
            setText(p.value);
            p.onError?.(null);
          }
        }}
      />
      {(differs || p.alwaysButton) && (
        <button type="button" class={`btn${p.primary ? " p" : ""}`} disabled={!differs || busy || env.readOnly} onClick={() => void save()}>
          {p.button ?? "Save"}
        </button>
      )}
    </span>
  );
}

// ---- states (SPEC V0.119 (6)) ----

/** Loading: still rows, no shimmer (nothing moves on its own). */
export function Skeleton({ rows, label }: { rows: number; label?: string }) {
  return (
    <div class="sg" aria-busy="true" aria-label={label ?? "Loading"}>
      <div class="sg-c">
        {Array.from({ length: rows }, (_, i) => (
          <div class="sr">
            <span class="sr-b skelb">
              <span class="skel" style={{ width: `${[38, 52, 30, 44][i % 4]}%` }} />
              <span class="skel s" style={{ width: `${[62, 48, 70, 55][i % 4]}%` }} />
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

/** A read that failed: why, and Try again. The rest of the page is untouched. */
export function LoadError({ text, onRetry }: { text: string; onRetry(): void }) {
  return (
    <div class="note-err" role="alert">
      <span>{text}</span>
      <button type="button" class="btn" onClick={onRetry}>
        Try again
      </button>
    </div>
  );
}

export function Empty({ title, text }: { title?: string; text: ComponentChildren }) {
  return (
    <div class="empty">
      {title && <b>{title}</b>}
      {text}
    </div>
  );
}
