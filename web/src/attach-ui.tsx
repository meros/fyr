// Attachment chips, pickers and the full-screen image viewer (SPEC 0.5 V0.18).

import { useEffect, useRef, useState } from "preact/hooks";
import { addChips, chipDone, chipError, chipProgress, fileLabel, formatSize, isImage, removeChip, type Chip } from "./attach.ts";
import { useApi } from "./api-context.ts";

let keySeq = 0;

/** Chips for one composer: add files (picked, pasted, dropped), each uploads at once, to `host` (V0.54: the
 *  thread's host, or the one a new thread starts on; the front routes by it). */
export function useAttachments(host?: string) {
  const api = useApi();
  const [chips, setChips] = useState<Chip[]>([]);
  const hostRef = useRef(host);
  hostRef.current = host;
  const aborts = useRef(new Map<string, AbortController>());
  const previews = useRef(new Set<string>());
  useEffect(
    () => () => {
      aborts.current.forEach((a) => a.abort());
      previews.current.forEach((u) => URL.revokeObjectURL(u));
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
      void api.uploadAttachment(named, (l, t) => setChips((c) => chipProgress(c, nf.key, l, t)), ac.signal, hostRef.current).then((r) => {
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

  const onPaste = (e: ClipboardEvent) => {
    const files = Array.from(e.clipboardData?.files ?? []);
    if (files.length) {
      e.preventDefault();
      add(files);
    }
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
  return { chips, add, remove, clear, onPaste, onDrop, onDragOver };
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

export interface SentAttachment {
  id: string;
  name: string;
  mediaType: string;
  size: number;
}

/** A user message's attachments: image thumbnails (tap: full screen) and file chips (open in a new tab). */
export function SentAttachments({ items, host }: { items: SentAttachment[]; host?: string }) {
  const { attachmentUrl } = useApi();
  const [full, setFull] = useState<SentAttachment | null>(null);
  if (!items.length) return null;
  return (
    <div class="att-sent">
      {items.map((a) =>
        isImage(a.mediaType) ? (
          <button class="att-img" onClick={() => setFull(a)} aria-label={`Open ${a.name}`}>
            <img src={attachmentUrl(a.id, host)} alt={a.name} loading="lazy" />
          </button>
        ) : (
          <a class="att-file" href={attachmentUrl(a.id, host)} target="_blank" rel="noopener noreferrer" title={a.name}>
            <span class="att-th lab">{fileLabel(a.mediaType, a.name)}</span>
            <span class="att-tx">
              <span class="att-n">{a.name}</span>
              <span class="att-m">{formatSize(a.size)}</span>
            </span>
          </a>
        ),
      )}
      {full && (
        <div class="att-full" role="dialog" aria-label={full.name} onClick={() => setFull(null)}>
          <img src={attachmentUrl(full.id, host)} alt={full.name} />
          <button class="att-close" aria-label="Close" onClick={() => setFull(null)}>
            ×
          </button>
        </div>
      )}
    </div>
  );
}
