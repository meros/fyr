// SPEC 0.5 V0.170: the host mark, one component for every place a host is shown: the thread rows, the host switch,
// the thread header, the load meters, the Host field and Settings → Hosts.
import { createContext } from "preact";
import { useContext, useMemo } from "preact/hooks";
import { useHosts } from "./host-pick-ui.tsx";
import { hostColors, hostMarkOf, type OwnerMarks } from "./host-mark.ts";
import { HOST_GLYPHS } from "./lucide.ts";

/** V0.171: the owner's host marks by install id, from the state (and its "hostMarks" event). */
export const HostMarksContext = createContext<OwnerMarks>({});
export const useHostMarks = () => useContext(HostMarksContext);

/** The colours of the front's hosts, computed once per hosts list and owner's marks. */
export function useHostColors(): Map<string, string> {
  const { hosts } = useHosts();
  const marks = useHostMarks();
  return useMemo(() => hostColors(hosts, marks), [hosts, marks]);
}

/** A circle with the host's glyph in its colour; an offline host's has a small ring at its corner. `label`: say the
 *  host's name to a screen reader (where no text beside the mark names it). */
export function HostMark({ name, size = 16, label = false }: { name: string; size?: number; label?: boolean }) {
  const { hosts } = useHosts();
  const marks = useHostMarks();
  const colors = useHostColors();
  const m = hostMarkOf(name, hosts, colors, marks);
  return (
    <span class={`hmark${m.offline ? " off" : ""}${m.emoji ? " emo" : ""}`} style={{ "--hc": `var(--p-${m.color})`, "--hs": `${size}px` }} title={m.title} data-host={name} data-glyph={m.glyph ?? "emoji"} {...(label ? { role: "img", "aria-label": `Host ${m.title}` } : { "aria-hidden": "true" })}>
      {m.glyph ? (
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" dangerouslySetInnerHTML={{ __html: HOST_GLYPHS[m.glyph] }} />
      ) : (
        // V0.171: the owner's emoji, on the host's colour as a glyph is.
        <span class="hme">{m.emoji}</span>
      )}
    </span>
  );
}
