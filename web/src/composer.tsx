// The thread's composer (SPEC V0.18, V0.128): the field, the commands list, attachments and Send.
import { useDraft } from "./drafts.ts";
import { AgentStatus } from "./agent-status.tsx";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import type { Thread } from "./types.ts";
import { isAvailable, unavailableReason } from "./store.ts";
import { ChipStrip, ClipPicker, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { createSubmitGuard } from "./send-guard.ts";
import { SlashList, useSlash } from "./slash-ui.tsx";

export interface AnswerField {
  index: number;
  text: string;
  setText(text: string): void;
  /** Enter: take the text as this question's answer, then submit or move on */
  submit(): void;
}

/** What a send carries (SPEC V0.18): text and uploaded attachment ids; text may be empty. */
export interface SendArgs {
  text: string;
  attachments?: string[];
  /** V0.37: a press in the live view */
  from?: "view";
}

/** SPEC V0.128: the composer sends an ordinary message on every tab; sent from the View tab, the thread then shows
 *  the Conversation tab, which opens at its end, where the new message is. Desktop and phone. */
export async function sendFrom(tab: string, args: SendArgs, send: (a: SendArgs) => Promise<boolean>, setTab: (tab: string) => void): Promise<boolean> {
  const ok = await send(args);
  if (ok && tab === "view") setTab("conv");
  return ok;
}

interface ComposerProps {
  t: Thread;
  now: number;
  answer: AnswerField | null;
  onSend(args: SendArgs): Promise<boolean>;
  onInterrupt(): void;
  /** V0.143: Restart an agent that does not respond */
  onRestart(): void;
}

/**
 * Enter sends (steers a running thread). While a question is open the composer is the answer field
 * for the active question's "Other" (U15): Enter answers, ⌘Enter queues the text as a message instead.
 * Files: the paperclip, paste and drop, any type; each uploads at once and shows a chip.
 */
export function Composer({ t, now, answer, onSend, onInterrupt, onRestart }: ComposerProps) {
  const [draft, setDraft] = useDraft(t.id);
  const att = useAttachments(t.hostId);
  const ta = useRef<HTMLTextAreaElement>(null);
  const [sending, setSending] = useState(false);
  const guard = useRef(createSubmitGuard(setSending)).current;
  const sendReason = unavailableReason(t, "thread.send");
  const text = answer ? answer.text : draft;
  const setText = answer ? answer.setText : setDraft;
  const disabled = !answer && !!sendReason;
  // V0.79: `/` at the start lists the agent's commands and skills (not in the answer field).
  const slash = useSlash(answer || disabled ? null : { host: t.hostId, threadId: t.id }, text, setText, ta);

  useLayoutEffect(() => {
    const el = ta.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 200) + "px";
  }, [text]);

  const plan = sendArgs(text, att.chips);
  const send = () => {
    if (sendReason || !plan.ok || guard.busy()) return;
    const args = plan.args;
    void guard.run(async () => {
      const ok = await onSend(args);
      if (ok) {
        setText("");
        att.clear();
      }
      return ok;
    });
  };

  const placeholder = answer
    ? `Answer question ${answer.index + 1} in your own words…`
    : sendReason
      ? sendReason
      : t.running
        ? "Steer the running thread…"
        : "Message…";

  return (
    <div class="cmpw" onDrop={att.onDrop} onDragOver={att.onDragOver}>
      <AgentStatus t={t} now={now} onRestart={onRestart} />
      <ChipStrip chips={att.chips} onRemove={att.remove} />
      <SlashList s={slash} />
      <div class={`cmp${disabled ? " off" : ""}`}>
        <ClipPicker onFiles={att.add} disabled={!!sendReason} />
        <textarea
          ref={ta}
          id="composer"
          class="in"
          rows={1}
          value={text}
          disabled={disabled}
          placeholder={placeholder}
          title={disabled ? sendReason : undefined}
          data-other={answer ? "1" : undefined}
          onInput={(e) => setText((e.target as HTMLTextAreaElement).value)}
          onPaste={att.onPaste}
          onKeyDown={(e) => {
            if (slash.onKeyDown(e)) return;
            if (e.key !== "Enter" || e.shiftKey || e.isComposing || e.repeat) return;
            e.preventDefault();
            e.stopPropagation();
            if (answer && !(e.metaKey || e.ctrlKey)) answer.submit();
            else send();
          }}
        />
        {answer && !sendReason && <span class="hint">⌘⏎ queue</span>}
        {t.running && (
          <button class="btn" disabled={!isAvailable(t, "thread.interrupt")} title={unavailableReason(t, "thread.interrupt") ?? "Interrupt (Esc)"} onClick={onInterrupt}>
            Interrupt <kbd>Esc</kbd>
          </button>
        )}
        {answer ? (
          <button class="btn p" title="Answer (Enter)" onClick={answer.submit}>
            Answer <kbd>⏎</kbd>
          </button>
        ) : (
          <button class="btn p" disabled={sending || !!sendReason || !plan.ok} title={sending ? "Sending…" : (sendReason ?? (plan.ok ? "Send (Enter)" : plan.reason))} onClick={send}>
            Send <kbd>⏎</kbd>
          </button>
        )}
      </div>
    </div>
  );
}
