// A text field that grows with its text up to its CSS max-height, then scrolls: the thread composer's rule
// (composer.tsx), here for the New thread prompt of both fronts (V0.dialogs-review (18)).
import { useLayoutEffect } from "preact/hooks";

export function useGrow(field: { current: HTMLTextAreaElement | null }, text: string): void {
  useLayoutEffect(() => {
    const el = field.current;
    if (!el) return;
    el.style.height = "auto";
    // scrollHeight has the padding; the borders are the rest of a border-box height.
    el.style.height = `${el.scrollHeight + el.offsetHeight - el.clientHeight}px`;
  }, [field, text]);
}
