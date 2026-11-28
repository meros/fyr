// V0.folder-chip (#26): the folder chip of the thread header, one component on both fronts. A click opens a card under
// it with the full path, the branch, the host and Copy path; a click outside or Escape closes it. V0.popover-place: on
// the phone a tap opens the same card as a bottom sheet (a card under a chip in a row that scrolls sideways had no
// place to stay, and the sheet gives the path the whole width and Copy path a finger's size); Back, a tap on the dim
// page or a drag down closes it.

import { useEffect, useRef, useState } from "preact/hooks";
import { createPortal } from "preact/compat";
import { CardView, HoverCard, usePopoverDismiss } from "./card-ui.tsx";
import { copyToClipboard } from "./copy.ts";
import { folderCard, folderChipText, folderLabel, threadFolder } from "./folder.ts";
import { BottomSheet } from "./motion-ui.tsx";
import type { Thread } from "./types.ts";

export function FolderChip({ t, phone }: { t: Pick<Thread, "cwd" | "branch" | "changes" | "host">; phone?: boolean }) {
  const v = threadFolder(t);
  const txt = folderChipText(v);
  const [open, setOpen] = useState(false);
  const [copied, setCopied] = useState<"" | "ok" | "fail">("");
  const chipRef = useRef<HTMLButtonElement>(null);
  const popRef = useRef<HTMLDivElement>(null);
  usePopoverDismiss(open && !phone, [chipRef, popRef], () => setOpen(false));
  useEffect(() => {
    if (!copied) return;
    const id = setTimeout(() => setCopied(""), 2000);
    return () => clearTimeout(id);
  }, [copied]);
  const toggle = () => {
    setCopied("");
    setOpen((o) => !o);
  };
  const copy = async () => setCopied((await copyToClipboard(v.path)) ? "ok" : "fail");
  const parts = (
    <>
      <span class="fldn">{txt.name}</span>
      {txt.branch && <span class="fldb">{txt.branch}</span>}
    </>
  );
  const body = (
    <div class={`fldpop${phone ? " phone" : ""}`}>
      <CardView c={folderCard(v, t.host)} />
      <div class="fldact">
        <button type="button" class="btn fldcopy" onClick={() => void copy()}>
          {copied === "ok" ? "Copied" : "Copy path"}
        </button>
        {copied === "fail" && <span class="fldfail">The browser did not copy it: select the path above.</span>}
      </div>
    </div>
  );
  return (
    <>
      {/* The phone's chip sits in a 44 px button (a finger's size) like the project chip; the desktop's is the 24 px chip. */}
      <button ref={chipRef} type="button" class={phone ? "m-pjb m-fldbtn" : "chip m fldc"} aria-expanded={open} aria-label={`${folderLabel(v)}. Path and Copy path`} title={phone ? undefined : v.path} onClick={toggle}>
        {phone ? <span class="m-chip m fldc">{parts}</span> : parts}
      </button>
      {open &&
        (phone ? (
          createPortal(
            <BottomSheet onClose={() => setOpen(false)} label={folderLabel(v)} cls="fldsheet" layer>
              {body}
            </BottomSheet>,
            chipRef.current?.closest(".m-app") ?? document.body,
          )
        ) : (
          <HoverCard at={() => chipRef.current?.getBoundingClientRect() ?? null} place="below" interactive popRef={popRef} onGone={() => setOpen(false)}>
            {body}
          </HoverCard>
        ))}
    </>
  );
}
