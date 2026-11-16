// SPEC 0.5 V0.27: what an effect decision says to a person. The action name and
// its arguments stay available as details; the card leads with words.
import type { Decision, PullRequest } from "./types.ts";
import { headLabel, shortHead } from "./pr.ts";

export interface EffectText {
  /** short name for the card header and the inbox row */
  label: string;
  /** one sentence: what the owner is asked to allow */
  title: string;
  /** the agent's reason, as it wrote it */
  why: string;
  /** what Fyr will do, in one or two sentences */
  happens: string;
  yes: string;
  no: string;
  details: { action: string; args: Record<string, unknown> };
}

const METHOD: Record<string, string> = {
  squash: "GitHub merges the branch as one commit.",
  merge: "GitHub merges the branch with a merge commit.",
  rebase: "GitHub puts the branch's commits on top of the base branch.",
};

function words(action: string): string {
  const w = action.replace(/[._-]+/g, " ").trim();
  return w ? w[0].toUpperCase() + w.slice(1) : "An action";
}

export function describeEffect(d: Decision, pr?: Pick<PullRequest, "url" | "number" | "title"> | null): EffectText {
  const action = d.effect?.action ?? "";
  const args = (d.effect?.args ?? {}) as Record<string, unknown>;
  const why = (d.effect?.reason ?? "").trim();
  const details = { action, args };
  if (action === "fyr.deploy") {
    return {
      label: "Update Fyr",
      title: "Update Fyr to a new version",
      why,
      happens: "Fyr builds and tests the new version, waits until no thread is working, then restarts. It is offline for a few seconds, and it goes back to the current version by itself if the new one fails.",
      yes: "Update Fyr",
      no: "Not now",
      details,
    };
  }
  if (action === "pr.merge") {
    const url = String(args.pr ?? "");
    const num = /\/pull\/(\d+)/.exec(url)?.[1] ?? (pr?.number ? String(pr.number) : "");
    const title = pr && (pr.url === url || String(pr.number) === num) ? pr.title : undefined;
    const ref = num ? `#${num}` : "the pull request";
    // V0.97: the head fixed when the agent asked; Fyr merges only that commit.
    const head = shortHead(typeof args.head === "string" ? args.head : "");
    return {
      label: `Merge ${ref}`,
      title: `Merge pull request${num ? " #" + num : ""}${title ? ": " + title : ""}`,
      why,
      happens: `${METHOD[String(args.method ?? "")] ?? "GitHub merges the branch."}${head ? ` Only commit ${head}: if the branch moved since the agent asked, Fyr refuses.` : ""} The agent gets the result.`,
      yes: headLabel("Merge", head),
      no: "Not now",
      details,
    };
  }
  return { label: words(action), title: words(action), why, happens: "Fyr runs it after you approve. The agent does not wait for it.", yes: "Approve", no: "Not now", details };
}
