// Attachments (SPEC 0.5 V0.18): the chip state of files being uploaded, and the send arguments. Pure.

import type { HostId } from "./hostid.ts";
import { hostUrl } from "./urls.ts";

export const MAX_BYTES = 25 * 1024 * 1024;

type ChipStatus = "uploading" | "done" | "error";

export interface Chip {
  /** client key, stable for the chip's life */
  key: string;
  name: string;
  size: number;
  mediaType: string;
  status: ChipStatus;
  /** 0..1 while uploading */
  progress: number;
  /** the host's attachment id once uploaded */
  id?: string;
  error?: string;
  /** an object URL for an image thumbnail */
  preview?: string;
}

interface NewFile {
  key: string;
  name: string;
  size: number;
  type: string;
  preview?: string;
}

export interface UploadReply {
  id: string;
  name: string;
  size: number;
  mediaType: string;
  path?: string;
}

/** New chips start uploading; a file over 25 MB fails at once without an upload. */
export function addChips(chips: Chip[], files: NewFile[]): Chip[] {
  return [
    ...chips,
    ...files.map<Chip>((f) => {
      const base = { key: f.key, name: f.name || "file", size: f.size, mediaType: f.type || "application/octet-stream", progress: 0, preview: f.preview };
      return f.size > MAX_BYTES ? { ...base, status: "error", error: `Over 25 MB (${formatSize(f.size)})` } : { ...base, status: "uploading" };
    }),
  ];
}

function update(chips: Chip[], key: string, fn: (c: Chip) => Chip): Chip[] {
  return chips.map((c) => (c.key === key ? fn(c) : c));
}

export function chipProgress(chips: Chip[], key: string, loaded: number, total: number): Chip[] {
  return update(chips, key, (c) => (c.status === "uploading" ? { ...c, progress: total > 0 ? Math.min(1, loaded / total) : 0 } : c));
}

export function chipDone(chips: Chip[], key: string, r: UploadReply): Chip[] {
  return update(chips, key, (c) => ({ ...c, status: "done", progress: 1, id: r.id, name: r.name || c.name, size: r.size ?? c.size, mediaType: r.mediaType || c.mediaType, error: undefined }));
}

/** `status` is the HTTP status, or null when the host could not be reached. */
export function uploadError(status: number | null, message?: string): string {
  if (status === 413) return "Over 25 MB";
  if (status === null) return message ? `Upload failed: ${message}` : "Upload failed: Fyr is not reachable";
  return message ? `Upload failed: ${message}` : `Upload failed (HTTP ${status})`;
}

export function chipError(chips: Chip[], key: string, status: number | null, message?: string): Chip[] {
  return update(chips, key, (c) => ({ ...c, status: "error", error: uploadError(status, message) }));
}

export function removeChip(chips: Chip[], key: string): Chip[] {
  return chips.filter((c) => c.key !== key);
}

type SendPlan = { ok: true; args: { text: string; attachments?: string[] } } | { ok: false; reason: string };

/** What a send carries: the text and the uploaded ids. A message may be attachments only. */
export function sendArgs(text: string, chips: Chip[]): SendPlan {
  if (chips.some((c) => c.status === "uploading")) return { ok: false, reason: "Waiting for uploads" };
  if (chips.some((c) => c.status === "error")) return { ok: false, reason: "Remove the files that failed" };
  const ids = chips.filter((c) => c.status === "done" && c.id).map((c) => c.id!);
  if (!text.trim() && !ids.length) return { ok: false, reason: "Nothing to send" };
  return { ok: true, args: ids.length ? { text, attachments: ids } : { text } };
}

export function isImage(mediaType: string): boolean {
  return /^image\//.test(mediaType);
}

/** A short type label for a chip: "PDF", "PNG", "Video", "LOG". */
export function fileLabel(mediaType: string, name: string): string {
  const ext = /\.([A-Za-z0-9]{1,5})$/.exec(name)?.[1]?.toUpperCase();
  if (mediaType === "application/pdf") return "PDF";
  if (/^image\//.test(mediaType)) return ext ?? mediaType.slice(6).toUpperCase();
  if (/^video\//.test(mediaType)) return "Video";
  if (/^audio\//.test(mediaType)) return "Audio";
  if (/zip|x-tar|gzip|x-7z/.test(mediaType)) return ext ?? "Archive";
  if (ext) return ext;
  if (/^text\//.test(mediaType)) return "Text";
  return "File";
}

export function formatSize(bytes: number): string {
  if (!(bytes >= 0)) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1).replace(/\.0$/, "")} MB`;
}

/** Where attachments and HTML previews load from (V0.18, V0.19). The Api provides them (#41: through ApiContext);
 *  the mock serves object and data URLs. `host` is the install id of the thread's host (decision 0031). */
export interface ContentUrls {
  attachmentUrl(host: HostId | undefined, id: string): string;
  previewUrl(host: HostId | undefined, id: string): string;
}

/** V0.download-assets: the URL that saves an attachment instead of showing it. The host answers `?download=1` with
 *  `Content-Disposition: attachment` and the file's name; a data or object URL (the mock, a local thumbnail) has no
 *  server behind it and stays as it is, saved by the link's `download` attribute. */
export function downloadUrl(src: string): string {
  if (!src.startsWith("/")) return src;
  return src + (src.includes("?") ? "&" : "?") + "download=1";
}

/** The URL of an attachment on its host. */
export function attachmentUrl(host: HostId | undefined, id: string): string {
  return hostUrl(host, `/attachments/${encodeURIComponent(id)}`);
}
