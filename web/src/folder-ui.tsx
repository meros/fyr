// V0.folder-chip (#26): the folder chip of the thread header, one component on both fronts. A click (a tap) opens a
// small card under it with the full path, the branch, the host and Copy path; a click outside or Escape closes it.

import { useEffect, useRef, useState } from "preact/hooks";
import { CardView, HoverCard } from "./card-ui.tsx";
import { copyToClipboard } from "./copy.ts";
import { folderCard, folderChipText, folderLabel, threadFolder } from "./folder.ts";
import type { Thread } from "./types.ts";

export function FolderChip({ t, phone }: { t: Pick<Thread, "cwd" | "branch" | "changes" | "host">; phone?: boolean }) {
  const v = threadFolder(t);
  const txt = folderChipText(v);
  const [openAt, setOpenAt] = useState<DOMRect | null>(null);
  const [copied, setCopied] = useState<"" | "ok" | "fail">("");
  const chipRef = useRef<HTMLButtonElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!openAt) return;
    const onDown = (e: Event) => {
      const target = e.target as Node;
      if (chipRef.current?.contains(target) || popRef.current?.contains(target)) return;
      setOpenAt(null);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpenAt(null);
    };
    document.addEventListener("pointerdown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("pointerdown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [openAt]);
  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(""), 2000);
    return () => clearTimeout(id);
  }, [copied]);
  const toggle = () => {
    setCopied("");
    setOpenAt((prev) => (prev ? null : (chipRef.current?.getBoundingClientRect() ?? null)));
  };
  const copy = async () => setCopied((await copyToClipboard(v.path)) ? "ok" : "fail");
  const parts = (
    <>
      <span class="fldn">{txt.name}</span>
      {txt.branch && <span class="fldb">{txt.branch}</span>}
    </>
  );
  return (
    <>
      {/* The phone's chip sits in a 44 px button (a finger's size) like the project chip; the desktop's is the 24 px chip. */}
      <button ref={chipRef} type="button" class={phone ? "m-pjb m-fldbtn" : "chip m fldc"} aria-expanded={!!openAt} aria-label={`${folderLabel(v)}. Path and Copy path`} title={phone ? undefined : v.path} onClick={toggle}>
        {phone ? <span class="m-chip m fldc">{parts}</span> : parts}
      </button>
      {openAt && (
        <HoverCard at={openAt} place="below" interactive popRef={popRef}>
          <div class={`fldpop${phone ? " phone" : ""}`}>
            <CardView c={folderCard(v, t.host)} />
            <div class="fldact">
              <button type="button" class="btn fldcopy" onClick={() => void copy()}>
                {copied === "ok" ? "Copied" : "Copy path"}
              </button>
              {copied === "fail" && <span class="fldfail">The browser did not copy it: select the path above.</span>}
            </div>
          </div>
        </HoverCard>
      )}
    </>
  );
}
