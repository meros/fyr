// In-thread previews (SPEC 0.5 V0.19): the card model for a `preview` event, frame heights, and
// which markdown image sources render inline. Pure.

import { attachmentUrl } from "./attach.ts";

export interface PreviewData {
  kind: "image" | "html";
  attachmentId: string;
  title: string;
  caption: string;
  width: number;
  height: number;
  url?: string;
}

export interface PreviewCard extends PreviewData {
  /** the event's seq: the card's place in the log and its DOM id */
  seq: number;
  at: string;
  /** what the image or frame loads */
  src: string;
}

/** Desktop thumbnails are at most this tall; phones show the full width. */
export const THUMB_MAX_H = 360;
/** An HTML frame starts at most this tall... */
export const FRAME_START_MAX = 600;
/** ...and never grows past this. */
export const FRAME_MAX = 2000;
const FRAME_DEFAULT = 400;

let previewUrlFor: (id: string) => string = (id) => `/api/previews/${encodeURIComponent(id)}`;
/** Where an HTML page loads from: a preview or a live view (V0.37). */
export function previewUrl(id: string): string {
  return previewUrlFor(id);
}

/** The app sets this (mock mode serves data: pages). */
export function setPreviewUrl(fn: (id: string) => string): void {
  previewUrlFor = fn;
}

function num(v: unknown): number {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.round(n) : 0;
}

/** The card for a `preview` event, or null when the event is not usable. */
export function previewCard(seq: number, at: string, data: unknown): PreviewCard | null {
  const d = (data && typeof data === "object" ? data : {}) as Record<string, unknown>;
  const kind = d.kind === "html" ? "html" : d.kind === "image" ? "image" : null;
  const id = typeof d.attachmentId === "string" ? d.attachmentId : "";
  if (!kind || !id) return null;
  return {
    seq,
    at,
    kind,
    attachmentId: id,
    title: typeof d.title === "string" ? d.title : "",
    caption: typeof d.caption === "string" ? d.caption : "",
    width: num(d.width),
    height: num(d.height),
    url: typeof d.url === "string" && /^https?:\/\//i.test(d.url) ? d.url : undefined,
    src: kind === "image" ? attachmentUrl(id) : previewUrlFor(id),
  };
}

/** An HTML frame's height: min(height, 600) at first, at most 2,000 px when opened up. */
export function frameHeight(height: number, expanded = false): number {
  const h = height > 0 ? height : FRAME_DEFAULT;
  return Math.min(h, expanded ? FRAME_MAX : FRAME_START_MAX);
}

/** A thumbnail's box: keep the aspect, at most `maxW` wide and `maxH` tall (desktop). */
export function thumbBox(width: number, height: number, maxW: number, maxH = THUMB_MAX_H): { width: number; height: number } {
  if (!(width > 0 && height > 0)) return { width: maxW, height: Math.min(maxH, Math.round(maxW * 0.6)) };
  const s = Math.min(1, maxW / width, maxH / height);
  return { width: Math.round(width * s), height: Math.round(height * s) };
}

/**
 * Markdown images: a same-origin `/api/attachments/{id}` renders inline; anything else stays a link.
 * Returns the attachment id, or null.
 */
export function inlineAttachmentId(src: string, origin: string): string | null {
  let u: URL;
  try {
    u = new URL(src.trim(), origin + "/");
  } catch {
    return null;
  }
  if (u.origin !== origin) return null;
  const m = /^\/api\/attachments\/([A-Za-z0-9_.-]+)$/.exec(u.pathname);
  return m && !u.search && !u.hash ? decodeURIComponent(m[1]) : null;
}

/** The thread's previews for the filmstrip, newest first. */
export function filmstrip(cards: PreviewCard[]): PreviewCard[] {
  return [...cards].sort((a, b) => b.seq - a.seq);
}

/** Viewer navigation over the thread's image previews. */
export function stepImage(cards: PreviewCard[], currentSeq: number, dir: 1 | -1): PreviewCard | null {
  const imgs = cards.filter((c) => c.kind === "image").sort((a, b) => a.seq - b.seq);
  const i = imgs.findIndex((c) => c.seq === currentSeq);
  if (i < 0) return null;
  return imgs[i + dir] ?? null;
}
