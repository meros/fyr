// In-thread previews (SPEC 0.5 V0.19): the card model for a `preview` event, frame heights, and
// which markdown image sources render inline. Pure.

import { attachmentUrl, hostQuery, type ContentUrls } from "./attach.ts";

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

/** Where an HTML page loads from on the host: a preview or a live view (V0.37), on the thread's host (V0.54). */
export function previewUrl(id: string, host?: string): string {
  return `/api/previews/${encodeURIComponent(id)}${hostQuery(host)}`;
}

/** The host's content URLs; the real Api uses them, the mock has its own. */
export const hostUrls: ContentUrls = { attachmentUrl, previewUrl };

function num(v: unknown): number {
  const n = Number(v);
  return Number.isFinite(n) && n > 0 ? Math.round(n) : 0;
}

/** The card for a `preview` event, or null when the event is not usable. */
export function previewCard(seq: number, at: string, data: unknown, host?: string, urls: ContentUrls = hostUrls): PreviewCard | null {
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
    src: kind === "image" ? urls.attachmentUrl(id, host) : urls.previewUrl(id, host),
  };
}

/** What an HTML frame may use (V0.47): its own full screen, WebXR and motion sensors for 3D. */
export const FRAME_ALLOW = "fullscreen; xr-spatial-tracking; accelerometer; gyroscope";
const FRAME_MIN = 40;

/**
 * An HTML frame's height (V0.19, V0.47): the page's own report, else the agent's `size` height, else 16:10 of the
 * card's width; min(that, 600) at first, at most 2,000 px when opened up.
 */
export function frameHeight(height: number, expanded = false, reported = 0, width = 0): number {
  const h = reported > 0 ? Math.ceil(reported) : height > 0 ? height : width > 0 ? Math.round((width * 10) / 16) : FRAME_DEFAULT;
  return Math.max(FRAME_MIN, Math.min(h, expanded ? FRAME_MAX : FRAME_START_MAX));
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
