// V0.diff-line-comments (#641): the comment box under picked diff lines, a pending comment, the bar that sends them, and
// the folded comments of a sent message in the conversation. One set of components for desktop and phone; the phone's
// box opens in a bottom sheet (no room under a line at 390 px), said where it does.

import { useEffect, useRef, useState } from "preact/hooks";
import { diffComments, location, parseComments, type DiffComment } from "./diff-comments.ts";
import { BottomSheet } from "./motion-ui.tsx";

/** The pending comments of one thread, kept in the drafts store; every view of the thread sees the same list. */
export function useDiffComments(threadId: string): [DiffComment[], (list: DiffComment[]) => void] {
  const [list, setList] = useState(() => diffComments.get(threadId));
  useEffect(() => {
    setList(diffComments.get(threadId));
    return diffComments.subscribe((id) => id === threadId && setList(diffComments.get(threadId)));
  }, [threadId]);
  return [list, (l) => diffComments.set(threadId, l)];
}

/** The file name and lines of a location, for a line that has no room for the folders (the title keeps the whole). */
function Where({ where }: { where: string }) {
  const m = /^(.*?)(:\d.*)$/.exec(where.replace(/^[^ ]*\//, ""));
  // The name ellipsizes, the lines never do: they are what the owner checks.
  return (
    <span class="dcm-where" title={where}>
      <span class="ell">{m ? m[1] : where}</span>
      {m && <span class="dcm-lines">{m[2]}</span>}
    </span>
  );
}

/** Ctrl+Enter (⌘⏎) sends everything; Enter alone writes a new line. */
const sendKey = (e: KeyboardEvent) => e.key === "Enter" && (e.ctrlKey || e.metaKey);

export function CommentBox({ where, phone, initial, onSave, onCancel, onSaveAndSend }: { where: string; phone?: boolean; initial?: string; onSave(text: string): void; onCancel(): void; onSaveAndSend(text: string): void }) {
  const [text, setText] = useState(initial ?? "");
  const ta = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    // The phone's sheet does not focus its field on open (V0.composer-focus-reach (2)): the keyboard would cover it.
    if (!phone) ta.current?.focus();
  }, [phone]);
  const ok = text.trim() !== "";
  const box = (
    <div class={`dcm-box${phone ? " phone" : ""}`} data-keys="self">
      <Where where={where} />
      <textarea
        ref={ta}
        class="dcm-ta"
        rows={3}
        value={text}
        placeholder="Comment for the agent"
        aria-label={`Comment on ${where}`}
        onInput={(e) => setText((e.currentTarget as HTMLTextAreaElement).value)}
        onKeyDown={(e) => {
          if (sendKey(e) && ok) {
            e.preventDefault();
            onSaveAndSend(text.trim());
          } else if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            onCancel();
          }
        }}
      />
      <div class="dcm-acts">
        {!phone && <span class="hint">⌘⏎ add and send</span>}
        <button class="btn" onClick={onCancel}>
          Cancel
        </button>
        <button class="btn p" disabled={!ok} onClick={() => onSave(text.trim())}>
          {initial ? "Save" : "Add comment"}
        </button>
      </div>
    </div>
  );
  // Phone: a sheet over the diff, as there is no room for a box under a line at phone width.
  if (phone)
    return (
      <BottomSheet onClose={onCancel} label={`Comment on ${where}`} cls="dcm-sheet">
        {box}
      </BottomSheet>
    );
  return box;
}

export function PendingComment({ c, onEdit, onDelete }: { c: DiffComment; onEdit(): void; onDelete(): void }) {
  return (
    <div class="dcm-pend">
      <div class="dcm-ph">
        <Where where={location(c)} />
        <button class="btn dcm-sm" onClick={onEdit}>
          Edit
        </button>
        <button class="btn dcm-sm" onClick={onDelete} aria-label={`Delete the comment on ${location(c)}`}>
          Delete
        </button>
      </div>
      <div class="dcm-text">{c.text}</div>
    </div>
  );
}

/** The bar over the files while comments wait: how many, Send and Discard. */
export function CommentBar({ count, sendOff, sending, onSend, onDiscard }: { count: number; sendOff: string | null; sending: boolean; onSend(): void; onDiscard(): void }) {
  if (!count) return null;
  const off = sending ? "Sending…" : sendOff;
  return (
    <div class="dcm-bar" role="region" aria-label="Pending comments">
      <span class="dcm-n tnum">
        {count} {count === 1 ? "comment" : "comments"} on the diff
      </span>
      <button class="btn" onClick={onDiscard}>
        Discard
      </button>
      <button class="btn p" disabled={off !== null} title={off ?? "Send them to the agent as one message"} onClick={onSend}>
        {sending ? "Sending…" : "Send"}
      </button>
    </div>
  );
}

/** The phone's bar while a line is picked: a second tap on a line makes it a range; Comment opens the sheet. */
export function PickBar({ where, onComment, onCancel }: { where: string; onComment(): void; onCancel(): void }) {
  return (
    <div class="dcm-pick" role="region" aria-label="Picked lines">
      <span class="dcm-pw">
        <Where where={where} />
        <span class="dcm-hint">Tap another line for a range</span>
      </span>
      <button class="btn" onClick={onCancel}>
        Cancel
      </button>
      <button class="btn p" onClick={onComment}>
        Comment
      </button>
    </div>
  );
}

/** The owner's message of diff comments in the conversation: each comment's place and text, its quoted lines folded. */
export function SentComments({ text }: { text: string }) {
  const cs = parseComments(text);
  if (!cs) return <>{text}</>;
  return (
    <div class="dcm-sent">
      <div class="dcm-sh">
        {cs.length} {cs.length === 1 ? "comment" : "comments"} on the diff
      </div>
      {cs.map((c, i) => (
        <div key={i} class="dcm-sc">
          <details class="dcm-q">
            <summary>
              <Where where={c.where} />
            </summary>
            <pre>{c.quote.join("\n")}</pre>
          </details>
          <div class="dcm-text">{c.text}</div>
        </div>
      ))}
    </div>
  );
}
