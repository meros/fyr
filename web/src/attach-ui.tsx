// Attachment chips, pickers and a sent message's attachments (SPEC 0.5 V0.18, V0.165).

import type { HostId } from "./hostid.ts";
import { useEffect, useRef, useState } from "preact/hooks";
import { addChips, chipDone, chipError, chipProgress, fileLabel, formatSize, isImage, pasteMarkers, pastedName, removeChip, type Chip } from "./attach.ts";
import { useApi } from "./api-context.ts";
import { DownloadLink, openImages, type ViewerImage } from "./preview-ui.tsx";

let keySeq = 0;

/** Chips for one composer: add files (picked, pasted, dropped), each uploads at once, to `host` (V0.54: the
 *  thread's host, or the one a new thread starts on; the front routes by it). V0.dialogs-review: `o.initial` are chips
 *  put back from a form that was closed (their uploads are on the host already); `o.onClose` gets the chips as the
 *  composer goes and returns the ones it keeps, whose thumbnails then stay valid. */
export function useAttachments(host?: HostId, o: { initial?: Chip[]; onClose?(chips: Chip[]): Chip[] } = {}) {
  const api = useApi();
  const [chips, setChips] = useState<Chip[]>(() => o.initial ?? []);
  const chipsRef = useRef(chips);
  chipsRef.current = chips;
  const hostRef = useRef(host);
  hostRef.current = host;
  const closeRef = useRef(o.onClose);
  closeRef.current = o.onClose;
  const aborts = useRef(new Map<string, AbortController>());
  const previews = useRef(new Set<string>((o.initial ?? []).flatMap((c) => c.preview ?? [])));
  useEffect(
    () => () => {
      aborts.current.forEach((a) => a.abort());
      const kept = new Set((closeRef.current?.(chipsRef.current) ?? []).map((c) => c.preview));
      previews.current.forEach((u) => !kept.has(u) && URL.revokeObjectURL(u));
    },
    [],
  );

  const add = (list: Iterable<File> | ArrayLike<File> | null | undefined) => {
    const files = list ? Array.from(list as ArrayLike<File>) : [];
    if (!files.length) return;
    const fresh = files.map((f) => {
      const preview = isImage(f.type) ? URL.createObjectURL(f) : undefined;
      if (preview) previews.current.add(preview);
      return { file: f, nf: { key: `f${++keySeq}`, name: f.name || (isImage(f.type) ? "pasted.png" : "file"), size: f.size, type: f.type, preview } };
    });
    setChips((c) => addChips(c, fresh.map((x) => x.nf)));
    for (const { file, nf } of fresh) {
      if (nf.size > 25 * 1024 * 1024) continue;
      const ac = new AbortController();
      aborts.current.set(nf.key, ac);
      const named = file.name ? file : new File([file], nf.name, { type: file.type });
      const to = hostRef.current;
      // Slice 2b: an attachment goes to the host the thread is on; with none picked there is nowhere to send it.
      if (!to) {
        setChips((c) => chipError(c, nf.key, null, "Pick the host first"));
        continue;
      }
      void api.uploadAttachment(to, named, (l, t) => setChips((c) => chipProgress(c, nf.key, l, t)), ac.signal).then((r) => {
        aborts.current.delete(nf.key);
        if (ac.signal.aborted) return;
        setChips((c) => (r.ok ? chipDone(c, nf.key, r.reply) : chipError(c, nf.key, r.status, r.error)));
      });
    }
  };
  const remove = (key: string) => {
    aborts.current.get(key)?.abort();
    aborts.current.delete(key);
    setChips((c) => removeChip(c, key));
  };
  const clear = () => setChips([]);
  /** V0.dialogs-review: replace the chips (Discard and its Undo in New thread). Uploads still on their way stop. */
  const put = (next: Chip[]) => {
    aborts.current.forEach((a) => a.abort());
    aborts.current.clear();
    for (const c of next) if (c.preview) previews.current.add(c.preview);
    setChips(next);
  };

  /** V0.paste-inline: a file pasted into a text field uploads as a chip, and a [name] marker goes in the text at the
   *  caret, so the agent reads where in the sentence it was pasted (the marker names the "Attached:" line it gets).
   *  Only the owner's own paste does this; the paperclip and the pickers add a chip alone, as before. */
  const onPaste = (e: ClipboardEvent) => {
    const files = Array.from(e.clipboardData?.files ?? []);
    if (!files.length) return;
    e.preventDefault();
    const taken = new Set(chipsRef.current.map((c) => c.name));
    const named = files.map((f) => {
      const name = pastedName(f.name, f.type, taken);
      taken.add(name);
      return name === f.name ? f : new File([f], name, { type: f.type });
    });
    add(named);
    const el = e.target;
    if (el instanceof HTMLTextAreaElement || el instanceof HTMLInputElement) insertAtCaret(el, named.map((f) => f.name));
  };
  const onDrop = (e: DragEvent) => {
    const files = Array.from(e.dataTransfer?.files ?? []);
    if (files.length) {
      e.preventDefault();
      add(files);
    }
  };
  const onDragOver = (e: DragEvent) => {
    if (e.dataTransfer?.types.includes("Files")) e.preventDefault();
  };
  return { chips, add, remove, clear, put, onPaste, onDrop, onDragOver };
}

/** Puts the markers at the caret the way typing would: through insertText, so the field's undo takes them back and its
 *  input handler stores the text; where the browser has no insertText, setRangeText and an input event. */
function insertAtCaret(el: HTMLTextAreaElement | HTMLInputElement, names: string[]) {
  const start = el.selectionStart ?? el.value.length;
  const end = el.selectionEnd ?? start;
  const s = pasteMarkers(names, el.value.slice(0, start), el.value.slice(end));
  let done = false;
  try {
    done = document.execCommand("insertText", false, s);
  } catch {
    done = false;
  }
  if (done) return;
  el.setRangeText(s, start, end, "end");
  el.dispatchEvent(new Event("input", { bubbles: true }));
}

export function ChipStrip({ chips, onRemove }: { chips: Chip[]; onRemove(key: string): void }) {
  if (!chips.length) return null;
  return (
    <div class="att-strip" role="list" aria-label="Attachments">
      {chips.map((c) => (
        <span class={`att-chip ${c.status}`} role="listitem" title={c.error ?? `${c.name} · ${formatSize(c.size)}`}>
          {c.preview ? <img class="att-th" src={c.preview} alt="" /> : <span class="att-th lab">{fileLabel(c.mediaType, c.name)}</span>}
          <span class="att-tx">
            <span class="att-n">{c.name}</span>
            <span class="att-m">{c.status === "error" ? c.error : c.status === "uploading" ? `${Math.round(c.progress * 100)} % · ${formatSize(c.size)}` : formatSize(c.size)}</span>
          </span>
          {c.status === "uploading" && <i class="att-bar" style={{ width: `${Math.round(c.progress * 100)}%` }} />}
          <button class="att-x" aria-label={`Remove ${c.name}`} onClick={() => onRemove(c.key)}>
            ×
          </button>
        </span>
      ))}
    </div>
  );
}

const ICON = {
  camera: (
    <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M4 8h3l1.6-2.4h6.8L17 8h3v11H4Z" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
      <circle cx="12" cy="13" r="3.4" fill="none" stroke="currentColor" stroke-width="1.7" />
    </svg>
  ),
  photos: (
    <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true">
      <rect x="3.5" y="5" width="17" height="14" rx="2.5" fill="none" stroke="currentColor" stroke-width="1.7" />
      <path d="M3.5 16l5-4.5 4 3.5 3-2.5 4.5 3.5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
      <circle cx="15.5" cy="9.5" r="1.5" fill="currentColor" />
    </svg>
  ),
  files: (
    <svg width="20" height="20" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M6 3.5h8l4 4V20.5H6Z M14 3.5v4h4" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linejoin="round" />
    </svg>
  ),
  clip: (
    <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
      <path d="M20 11.5l-7.8 7.8a5 5 0 0 1-7.1-7.1l8.5-8.5a3.3 3.3 0 0 1 4.7 4.7l-8.5 8.5a1.7 1.7 0 0 1-2.4-2.4l7.8-7.8" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  ),
};

/** A hidden file input behind a button. */
function Picker({ cls, label, accept, capture, multiple, icon, text, disabled, onFiles }: { cls: string; label: string; accept?: string; capture?: string; multiple?: boolean; icon: preact.ComponentChildren; text?: string; disabled?: boolean; onFiles(f: FileList): void }) {
  const ref = useRef<HTMLInputElement>(null);
  return (
    <>
      <button class={cls} aria-label={label} title={label} disabled={disabled} onClick={() => ref.current?.click()}>
        {icon}
        {text}
      </button>
      <input
        ref={ref}
        type="file"
        accept={accept}
        capture={capture as never}
        multiple={multiple}
        hidden
        data-picker={label.toLowerCase()}
        onChange={(e) => {
          const el = e.target as HTMLInputElement;
          if (el.files?.length) onFiles(el.files);
          el.value = "";
        }}
      />
    </>
  );
}

/** Phone: Camera, Photos and Files (44 px each). `big` renders the New screen's labelled tiles. */
export function PhonePickers({ onFiles, disabled, big }: { onFiles(f: FileList): void; disabled?: boolean; big?: boolean }) {
  const cls = big ? "m-capb" : "b44 att-b";
  return (
    <>
      <Picker cls={`${cls} cam`} label="Camera" accept="image/*" capture="environment" icon={ICON.camera} text={big ? "Camera" : undefined} disabled={disabled} onFiles={onFiles} />
      <Picker cls={`${cls} pho`} label="Photos" accept="image/*,video/*" multiple icon={ICON.photos} text={big ? "Photos" : undefined} disabled={disabled} onFiles={onFiles} />
      <Picker cls={`${cls} fil`} label="Files" multiple icon={ICON.files} text={big ? "Files" : undefined} disabled={disabled} onFiles={onFiles} />
    </>
  );
}

/** Desktop: a paperclip for any file type. */
export function ClipPicker({ onFiles, disabled }: { onFiles(f: FileList): void; disabled?: boolean }) {
  return <Picker cls="btn clip" label="Attach files" multiple icon={ICON.clip} disabled={disabled} onFiles={onFiles} />;
}

interface SentAttachment {
  id: string;
  name: string;
  mediaType: string;
  size: number;
}

/** A user message's attachments: image thumbnails (tap: the zooming viewer, V0.165, swiping between this message's
 *  images) and file chips (open in a new tab: the phone hands a PDF to its own viewer), each chip with Download
 *  beside it (V0.download-assets; an image has it in the viewer). */
/** How the viewer frames a sent file, or undefined when it cannot (V0.attachment-doc-preview). */
function docFrame(a: SentAttachment): "pdf" | "html" | undefined {
  if (a.mediaType === "application/pdf") return "pdf";
  if (a.mediaType === "text/html") return "html";
  return undefined;
}

export function SentAttachments({ items, host }: { items: SentAttachment[]; host?: HostId }) {
  const { attachmentUrl } = useApi();
  if (!items.length) return null;
  // V0.attachment-doc-preview: images, PDFs and HTML pages open in the viewer, in the order sent
  const images: ViewerImage[] = items.filter((a) => isImage(a.mediaType) || docFrame(a)).map((a) => ({ key: a.id, src: attachmentUrl(host, a.id), title: a.name, name: a.name, frame: docFrame(a) }));
  return (
    <div class="att-sent">
      {items.map((a) =>
        isImage(a.mediaType) ? (
          <button class="att-img" onClick={() => openImages(images, images.findIndex((x) => x.key === a.id))} aria-label={`Open ${a.name}`}>
            <img src={attachmentUrl(host, a.id)} alt={a.name} loading="lazy" decoding="async" />
          </button>
        ) : (
          <span class="att-fw">
            <a
              class="att-file"
              href={attachmentUrl(host, a.id)}
              target="_blank"
              rel="noopener noreferrer"
              title={a.name}
              // a plain press previews it; a modified click or middle click keeps the link's own tab
              onClick={docFrame(a) ? (e) => { if (e.button !== 0 || e.ctrlKey || e.metaKey || e.shiftKey) return; e.preventDefault(); openImages(images, images.findIndex((x) => x.key === a.id)); } : undefined}
            >
              <span class="att-th lab">{fileLabel(a.mediaType, a.name)}</span>
              <span class="att-tx">
                <span class="att-n">{a.name}</span>
                <span class="att-m">{formatSize(a.size)}</span>
              </span>
            </a>
            <DownloadLink cls="att-dl" src={attachmentUrl(host, a.id)} name={a.name} />
          </span>
        ),
      )}
    </div>
  );
}
