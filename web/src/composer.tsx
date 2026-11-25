// The thread's composer of both fronts (SPEC V0.18, V0.128; V0.composer-focus-reach (1)): the field, the commands list,
// attachments and Send. `useComposer` is all of it but the markup; `Composer` (desktop) and `PhoneComposer` are two
// thin views over it, and each difference left between them says why next to it.
import { useDraft } from "./drafts.ts";
import { AgentStatus } from "./agent-status.tsx";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import type { Thread } from "./types.ts";
import { isAvailable, unavailableReason } from "./store.ts";
import { ChipStrip, ClipPicker, PhonePickers, useAttachments } from "./attach-ui.tsx";
import { sendArgs } from "./attach.ts";
import { createSubmitGuard } from "./send-guard.ts";
import { SlashList, useSlash } from "./slash-ui.tsx";
import { haptic } from "./native.ts";
import { I } from "./phone-icons.tsx";
import { answerTakesComposer, composerKey, composerPlaceholder } from "./compose.ts";

export interface AnswerField {
  index: number;
  text: string;
  setText(text: string): void;
  /** Enter: take the text as this question's answer, then submit or move on */
  submit(): void;
  /** V0.no-interrupt (1): the owner chose to answer in their own words ("Other…", 0), so the field is the answer's
   *  even over a draft */
  chosen: boolean;
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

/**
 * The composer's state and commands, the same on both fronts: the draft (or, while a question is open on the
 * desktop, the answer's text), attachments, the `/` list, the field's height and one guarded send. A send that
 * the host took clears the field and the chips and gives a short haptic tap (a no-op without a touch screen).
 */
export function useComposer(t: Thread, onSend: (a: SendArgs) => Promise<boolean>, asked: AnswerField | null = null) {
  const [draft, setDraft] = useDraft(t.id);
  // V0.no-interrupt (1): a question that arrives while the owner writes a message waits on its card; the field stays theirs.
  const answer = answerTakesComposer(draft, asked) ? asked : null;
  const att = useAttachments(t.hostId);
  const ta = useRef<HTMLTextAreaElement>(null);
  const [sending, setSending] = useState(false);
  const guard = useRef(createSubmitGuard(setSending)).current;
  const sendReason = unavailableReason(t, "thread.send");
  const text = answer ? answer.text : draft;
  const setText = answer ? answer.setText : setDraft;
  // An open question is answered even when a message could not be sent.
  const disabled = !answer && !!sendReason;
  // V0.79: `/` at the start lists the agent's commands and skills (not in the answer field).
  const slash = useSlash(answer || disabled ? null : { host: t.hostId, threadId: t.id }, text, setText, ta);

  // The field grows with its text up to the CSS max-height of each front (thread.css, phone.css), then scrolls.
  useLayoutEffect(() => {
    const el = ta.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = el.scrollHeight + "px";
  }, [text]);

  const plan = sendArgs(text, att.chips);
  const send = () => {
    if (sendReason || !plan.ok || guard.busy()) return;
    const args = plan.args;
    void guard.run(async () => {
      const ok = await onSend(args);
      if (ok) {
        haptic();
        setText("");
        att.clear();
      }
      return ok;
    });
  };
  const onKeyDown = (e: KeyboardEvent, enterSends: boolean) => {
    if (slash.onKeyDown(e)) return;
    const k = composerKey(e, { enterSends, answering: !!answer });
    if (k === "none") return;
    e.preventDefault();
    e.stopPropagation();
    if (k === "answer") answer!.submit();
    else send();
  };
  const stopReason = unavailableReason(t, "thread.interrupt");
  return {
    ta,
    /** The open question's answer field when it holds the composer now (V0.no-interrupt (1)) */
    answer,
    text,
    setText,
    att,
    slash,
    sendReason,
    disabled,
    send,
    onKeyDown,
    placeholder: composerPlaceholder(t, sendReason, answer),
    /** Why Send is off now (sending, unavailable, nothing to send), or null when it can send */
    sendOff: sending ? "Sending…" : (sendReason ?? (plan.ok ? null : plan.reason)),
    /** Stop (thread.interrupt): available, or why not */
    canStop: isAvailable(t, "thread.interrupt"),
    stopReason,
    empty: !text.trim() && !att.chips.length,
  };
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
 * The desktop's composer. Enter sends (steers a running thread). While a question is open the composer is the answer
 * field for the active question's "Other" (U15): Enter answers, ⌘Enter queues the text as a message instead. A
 * question that arrives over a draft leaves the draft and its Enter alone until it is sent or cleared (V0.no-interrupt (1)).
 * Files: the paperclip, paste and drop, any type; each uploads at once and shows a chip.
 */
export function Composer({ t, now, answer: asked, onSend, onInterrupt, onRestart }: ComposerProps) {
  const c = useComposer(t, onSend, asked);
  const answer = c.answer;
  return (
    <div class="cmpw" onDrop={c.att.onDrop} onDragOver={c.att.onDragOver}>
      <AgentStatus t={t} now={now} onRestart={onRestart} />
      <ChipStrip chips={c.att.chips} onRemove={c.att.remove} />
      <SlashList s={c.slash} />
      <div class={`cmp${c.disabled ? " off" : ""}`}>
        <ClipPicker onFiles={c.att.add} disabled={!!c.sendReason} />
        <textarea
          ref={c.ta}
          id="composer"
          class="in"
          rows={1}
          value={c.text}
          disabled={c.disabled}
          placeholder={c.placeholder}
          // Desktop only: a hover shows why the field is off when the reason is longer than the placeholder's line.
          title={c.disabled ? (c.sendReason ?? undefined) : undefined}
          data-other={answer ? "1" : undefined}
          onInput={(e) => c.setText((e.target as HTMLTextAreaElement).value)}
          onPaste={c.att.onPaste}
          onKeyDown={(e) => c.onKeyDown(e, true)}
        />
        {answer && !c.sendReason && <span class="hint">⌘⏎ queue</span>}
        {/* Stop stands beside Send while a run goes on: the desktop has the width, and Esc is its key. */}
        {t.running && (
          <button class="btn" disabled={!c.canStop} title={c.stopReason ?? "Stop the run (Esc)"} onClick={onInterrupt}>
            Stop <kbd>Esc</kbd>
          </button>
        )}
        {answer ? (
          <button class="btn p" title="Answer (Enter)" onClick={answer.submit}>
            Answer <kbd>⏎</kbd>
          </button>
        ) : (
          <button class="btn p" disabled={c.sendOff !== null} title={c.sendOff ?? "Send (Enter)"} onClick={c.send}>
            Send <kbd>⏎</kbd>
          </button>
        )}
      </div>
    </div>
  );
}

/**
 * The phone's composer: the same state as the desktop's (useComposer); what differs, and why:
 * - Enter is a new line and Send is the button: the on-screen keyboard's return key is the only way to start a line.
 * - No answer mode: the phone's question sheet (MAsk) has its own field, and the composer is hidden while it shows,
 *   except under a sheet that came folded over a message the owner was writing (V0.no-interrupt (2)).
 * - Stop takes Send's place while a run goes on and the field is empty: one 44 px button at the thumb, no width for two.
 * - The camera and file pickers instead of the paperclip: a phone takes a photo where a desktop picks a file.
 */
export function PhoneComposer({ t, now, onSend, onStop, onRestart }: { t: Thread; now: number; onSend(args: SendArgs): Promise<boolean>; onStop(): void; onRestart(): void }) {
  const c = useComposer(t, onSend);
  const stop = t.running && c.empty;
  return (
    <div class="m-cmpw" onDrop={c.att.onDrop} onDragOver={c.att.onDragOver}>
      <AgentStatus phone t={t} now={now} onRestart={onRestart} />
      <ChipStrip chips={c.att.chips} onRemove={c.att.remove} />
      <SlashList s={c.slash} phone />
      <div class={`m-cmp${c.disabled ? " off" : ""}`}>
        <PhonePickers onFiles={c.att.add} disabled={!!c.sendReason} />
        <textarea
          ref={c.ta}
          class="in"
          rows={1}
          value={c.text}
          disabled={c.disabled}
          placeholder={c.placeholder}
          onInput={(e) => c.setText((e.target as HTMLTextAreaElement).value)}
          onKeyDown={(e) => c.onKeyDown(e, false)}
          onPaste={c.att.onPaste}
        />
        {stop ? (
          <button class="m-send" aria-label="Stop the run" title={c.stopReason ?? "Stop the run"} disabled={!c.canStop} onClick={onStop}>
            {I.stop}
          </button>
        ) : (
          <button class="m-send" aria-label="Send" title={c.sendOff ?? "Send"} disabled={c.sendOff !== null} onClick={c.send}>
            {I.send}
          </button>
        )}
      </div>
    </div>
  );
}
