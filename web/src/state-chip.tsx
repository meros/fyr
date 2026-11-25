// V0.thread-claims: the state chip of a thread's header, one component on desktop and phone. It says the row's word
// ("Merge", "1 question", "Failed", "Waiting", "Finished"), not the state's code name ("Blocked", "Asking", "Done"), and
// its icon turns only while the thread's state is working: a run that waits for the owner's answer is not shown as moving.
import { StateIcon, stateClass } from "./icons.tsx";
import { stateChip } from "./rows.ts";
import type { Thread } from "./types.ts";

export function StateChip({ t, phone }: { t: Thread; phone?: boolean }) {
  const st = t.attention.state;
  const c = stateChip(t);
  // Phone: no title, there is no hover; the chips open Details, whose State row has the whole line.
  return (
    <span class={`${phone ? "m-chip" : "chip"} st ${stateClass(st)}`} title={phone ? undefined : c.why || c.word} data-state={st}>
      <span class={st === "working" ? "spin icw" : "icw"}>
        <StateIcon state={st} size={14} />
      </span>
      <span class="stw">{c.word}</span>
    </span>
  );
}
