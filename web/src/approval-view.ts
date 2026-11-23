// The words of an approval or effect card, built once for both fronts (desktop DecisionCard,
// phone MAllow). Markup stays per front; what the card says does not.

import { describeApproval, type ApprovalText } from "./approvals.ts";
import { describeEffect, type EffectText } from "./effects.ts";
import type { Decision, PullRequest } from "./types.ts";

interface ApprovalView {
  /** card header word */
  label: string;
  /** the question the card asks */
  title: string;
  /** set for a tool approval: the body is the readable approval */
  approval: ApprovalText | null;
  /** set for an effect decision (merge, deploy, run): the body is the effect text */
  effect: EffectText | null;
  /** the host the thread runs on; both fronts show it in the body ("in cwd · on host") */
  host: string | undefined;
  yes: string;
  no: string;
  /** the line under each choice, "" for none */
  yesDesc: string;
  noDesc: string;
}

/** Both fronts show the working folder and the host (the more informative of the two older behaviours). */
export function approvalView(d: Decision, thread: { cwd?: string; host?: string | null }, pr?: Pick<PullRequest, "url" | "number" | "title"> | null): ApprovalView {
  const isApproval = d.kind === "approval";
  const approval = isApproval ? describeApproval(d, thread.cwd ?? "") : null;
  const effect = isApproval ? null : describeEffect(d, pr);
  return {
    label: effect ? effect.label : "Approval",
    title: effect ? effect.title : approval!.title,
    approval,
    effect,
    host: thread.host || undefined,
    yes: effect ? effect.yes : "Allow",
    no: effect ? effect.no : "Deny",
    yesDesc: isApproval ? "Run it once." : "",
    noDesc: isApproval ? "The agent is told no and continues." : "Nothing happens; the agent is told.",
  };
}
