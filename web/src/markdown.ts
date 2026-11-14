// Assistant markdown. Raw HTML in the source is escaped, links are limited to safe schemes,
// and images render as links (no remote fetches from the log).

import { Marked, type Tokens } from "marked";
import { inlineAttachmentId } from "./previews.ts";
import { attachmentUrl } from "./attach.ts";

/** The page's origin; tests set one. */
let originOverride: string | null = null;
export function setMarkdownOrigin(o: string | null): void {
  originOverride = o;
}
function mdOrigin(): string {
  return originOverride ?? (typeof location !== "undefined" ? location.origin : "http://localhost");
}

export function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]!);
}

function safeHref(href: string): string | null {
  const h = href.trim();
  if (/^(https?:|mailto:)/i.test(h)) return h;
  if (/^[a-z][a-z0-9+.-]*:/i.test(h)) return null; // javascript:, data:, file: ...
  return null; // relative links mean nothing in the client
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
      if (!h) return inner;
      const t = title ? ` title="${escapeHtml(title)}"` : "";
      return `<a href="${escapeHtml(h)}"${t} target="_blank" rel="noopener noreferrer">${inner}</a>`;
    },
    code({ text, lang }: Tokens.Code) {
      // V0.34: each code block has its own Copy button (the click is handled in the conversation).
      const cls = lang ? ` class="language-${escapeHtml(lang.split(/\s/)[0])}"` : "";
      return `<div class="codeb"><button type="button" class="cpy" data-cpy aria-label="Copy code">Copy</button><pre><code${cls}>${escapeHtml(text)}\n</code></pre></div>`;
    },
    image({ href, text }: Tokens.Image) {
      // A same-origin attachment renders inline (V0.19); every other image stays a link.
      const id = inlineAttachmentId(href, mdOrigin());
      if (id) return `<img class="md-img" src="${escapeHtml(attachmentUrl(id))}" alt="${escapeHtml(text || "")}" loading="lazy">`;
      const h = safeHref(href);
      const label = escapeHtml(text || "image");
      return h ? `<a href="${escapeHtml(h)}" target="_blank" rel="noopener noreferrer">[${label}]</a>` : `[${label}]`;
    },
  },
});

export function renderMarkdown(src: string): string {
  return md.parse(src, { async: false }) as string;
}
