// V0.sensitive-pr: a PR whose merge needs the owner's deliberate press, such as a promotion to production. The agent
// marks it on its merge ask (args.sensitive: the reason), or the PR has the GitHub label "sensitive"; the host sends
// the reason as the change's `sensitive`. Fyr never auto-merges it, Merge all green leaves it out, its card shows the
// reason in red, and every Merge of it asks first, on the desktop and the phone alike.

import type { Ask } from "./confirm.ts";
import { shortHead } from "./pr.ts";
import type { Change, Decision, Thread } from "./types.ts";

/** Why the merge decision d of thread t is sensitive, or "": the ask's own reason, else its change's. */
export function decisionSensitive(t: Pick<Thread, "changes"> | null | undefined, d: Decision): string {
  if (d.kind !== "effect" || d.effect?.action !== "pr.merge") return "";
  const args = (d.effect.args ?? {}) as Record<string, unknown>;
  if (typeof args.sensitive === "string" && args.sensitive.trim()) return args.sensitive.trim();
  const id = typeof args.change === "string" ? args.change : "";
  return t?.changes.find((c) => c.id === id)?.sensitive ?? "";
}

/** The red line on a sensitive change's card. */
export const sensitiveLine = (why: string) => `Sensitive: ${why}. Fyr never merges it by itself.`;

/** The confirmation before a sensitive merge. */
export function sensitiveMergeAsk(why: string, head?: string): Ask {
  return {
    title: "Merge a sensitive PR?",
    text: `Sensitive: ${why}. Fyr merges ${head ? `commit ${shortHead(head)}` : "it"} on GitHub when you confirm.`,
    confirm: "Merge",
    danger: true,
  };
}

/** Confirms a sensitive merge first; true when it may go on (always for a merge that is not sensitive). */
export async function confirmSensitive(why: string, ask: (a: Ask) => Promise<boolean>, head?: string): Promise<boolean> {
  return !why || ask(sensitiveMergeAsk(why, head));
}

/** The change's reason, for the callers that hold a change. */
export const changeSensitive = (c: Pick<Change, "sensitive"> | null | undefined) => c?.sensitive ?? "";
