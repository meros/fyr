// Assistant markdown. Raw HTML in the source is escaped, links are limited to safe schemes,
// and images render as links (no remote fetches from the log).

import { Marked, type Tokens } from "marked";

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
    image({ href, text }: Tokens.Image) {
      const h = safeHref(href);
      const label = escapeHtml(text || "image");
      return h ? `<a href="${escapeHtml(h)}" target="_blank" rel="noopener noreferrer">[${label}]</a>` : `[${label}]`;
    },
  },
});

export function renderMarkdown(src: string): string {
  return md.parse(src, { async: false }) as string;
}
