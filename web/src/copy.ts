// SPEC 0.5 V0.34: what a Copy button puts on the clipboard.
type CopyWhat = { kind: "message"; text: string } | { kind: "block"; text: string };

export function copySource(w: CopyWhat): string {
  return w.kind === "block" ? w.text.replace(/\n+$/, "") : w.text;
}

/** Write text to the clipboard; resolves false when the browser refuses. */
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    // Older WebViews without the async clipboard: a hidden textarea and execCommand.
    try {
      const ta = document.createElement("textarea");
      ta.value = text;
      ta.setAttribute("readonly", "");
      ta.style.cssText = "position:fixed;opacity:0;left:-9999px";
      document.body.appendChild(ta);
      ta.select();
      const ok = document.execCommand("copy");
      ta.remove();
      return ok;
    } catch {
      return false;
    }
  }
}
