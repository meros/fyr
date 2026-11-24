// Assistant markdown. Raw HTML in the source is escaped, links are limited to safe schemes,
// and images render as links (no remote fetches from the log).

import type { HostId } from "./hostid.ts";
import { Marked, type Tokens } from "marked";
import { inlineAttachmentId } from "./previews.ts";
import { attachmentUrl } from "./attach.ts";
import { pathRef } from "./filelinks.ts";

// V0.38: a message renders once; a new event does not parse the old ones again.
const memo = new Map<string, string>();
// V0.54: the host of the thread being rendered; its attachments load from there (#41: through the Api's URLs).
let curHost: HostId | undefined;
let curUrl: (host: HostId | undefined, id: string) => string = attachmentUrl;

/** The page's origin; tests set one. */
let originOverride: string | null = null;
export function setMarkdownOrigin(o: string | null): void {
  originOverride = o;
  memo.clear();
}
function mdOrigin(): string {
  return originOverride ?? (typeof location !== "undefined" ? location.origin : "http://localhost");
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}

function safeHref(href: string): string | null {
  const h = href.trim();
  if (/^(https?:|mailto:)/i.test(h)) return h;
  if (/^[a-z][a-z0-9+.-]*:/i.test(h)) return null; // javascript:, data:, file: ...
  return null; // a relative link is a file path or nothing (pathAttrs)
}

/** V0.message-links-files: the attributes of a file path in a thread's message. The path is only a candidate here:
 *  the conversation asks the thread's host which exist and turns those into links (file-view-ui.tsx). Without a host
 *  (a PR's text) no path is marked. */
function pathAttrs(s: string): string | null {
  if (!curHost) return null;
  let raw = s;
  try {
    raw = decodeURIComponent(s);
  } catch {
    /* not percent-encoded: as written */
  }
  const r = pathRef(raw);
  if (!r) return null;
  return ` class="md-path" data-path="${escapeHtml(r.path)}"${r.line ? ` data-line="${r.line}"` : ""}`;
}

const md = new Marked({
  gfm: true,
  breaks: false,
  renderer: {
    html({ text }: Tokens.HTML | Tokens.Tag) {
      return escapeHtml(text);
    },
    link({ href, title, tokens }: Tokens.Link) {
      const inner = this.parser.parseInline(tokens);
      const h = safeHref(href);
      if (!h) {
        const file = pathAttrs(href);
        return file ? `<span${file}>${inner}</span>` : inner;
      }
      const t = title ? ` title="${escapeHtml(title)}"` : "";
      return `<a href="${escapeHtml(h)}"${t} target="_blank" rel="noopener noreferrer">${inner}</a>`;
    },
    codespan({ text }: Tokens.Codespan) {
      // V0.message-links-files: a URL written as code is still a link, and a file path is marked as one.
      const code = `<code>${escapeHtml(text)}</code>`;
      const url = /^https?:\/\/[^\s<>"'`]+$/i.test(text.trim()) ? text.trim() : null;
      if (url) return `<a href="${escapeHtml(url)}" target="_blank" rel="noopener noreferrer">${code}</a>`;
      const file = pathAttrs(text);
      return file ? `<code${file}>${escapeHtml(text)}</code>` : code;
    },
    table(token: Tokens.Table) {
      // V0.35: every cell names its column, so a narrow screen shows each row as a card.
      const label = (c: Tokens.TableCell) => escapeHtml(c.text.replace(/[`*_~]/g, "").trim());
      const align = (c: Tokens.TableCell) => (c.align ? ` style="text-align:${c.align}"` : "");
      const head = token.header.map((c) => `<th${align(c)}>${this.parser.parseInline(c.tokens)}</th>`).join("");
      const body = token.rows
        .map((r) => `<tr>${r.map((c, i) => `<td data-label="${label(token.header[i] ?? c)}"${align(c)}>${this.parser.parseInline(c.tokens)}</td>`).join("")}</tr>`)
        .join("");
      return `<div class="tblw"><table><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table></div>`;
    },
    code({ text, lang }: Tokens.Code) {
      // V0.34: each code block has its own Copy button (the click is handled in the conversation).
      const cls = lang ? ` class="language-${escapeHtml(lang.split(/\s/)[0])}"` : "";
      return `<div class="codeb"><button type="button" class="cpy" data-cpy aria-label="Copy code">Copy</button><pre><code${cls}>${escapeHtml(text)}\n</code></pre></div>`;
    },
    image({ href, text }: Tokens.Image) {
      // A same-origin attachment renders inline (V0.19); every other image stays a link.
      const id = inlineAttachmentId(href, mdOrigin());
      if (id && curHost) return `<img class="md-img" src="${escapeHtml(curUrl(curHost, id))}" alt="${escapeHtml(text || "")}" loading="lazy">`;
      const h = safeHref(href);
      const label = escapeHtml(text || "image");
      return h ? `<a href="${escapeHtml(h)}" target="_blank" rel="noopener noreferrer">[${label}]</a>` : `[${label}]`;
    },
  },
});

/** V0.longtask-work: counts of the main thread's work units (a markdown parse, a store flush), read by the layout test
 *  in place of a wall-time budget: the count does not depend on how busy the machine is. */
export function workCount(k: "mdParses" | "flushes"): void {
  const w = ((globalThis as { __fyrWork?: Record<string, number> }).__fyrWork ??= {});
  w[k] = (w[k] ?? 0) + 1;
}

export function renderMarkdown(src: string, host?: HostId, urlFor: (host: HostId | undefined, id: string) => string = attachmentUrl): string {
  if (urlFor !== curUrl) {
    // Another Api's URLs: what is cached points elsewhere.
    curUrl = urlFor;
    memo.clear();
  }
  const key = host ? `${host}\u0000${src}` : src;
  const hit = memo.get(key);
  if (hit !== undefined) {
    memo.delete(key); // keep recently used entries last
    memo.set(key, hit);
    return hit;
  }
  curHost = host;
  workCount("mdParses");
  let html: string;
  try {
    html = md.parse(src, { async: false }) as string;
  } finally {
    curHost = undefined;
  }
  memo.set(key, html);
  if (memo.size > 600) memo.delete(memo.keys().next().value as string);
  return html;
}
