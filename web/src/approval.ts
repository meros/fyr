// Pure formatting for tool approval cards (desktop & phone, V0.tool-approval-judge).

import type { Decision } from "./types.ts";

/** Commands longer than this many lines start clamped with a "Show full command" toggle. */
export const MAX_CMD_LINES = 8;

export interface FormattedApproval {
  cmd: string | null;
  fullCmd: string | null;
  truncated: boolean;
  hiddenLines: number;
  outsideSandbox: boolean;
  rest: Record<string, unknown>;
  judgeNote: string | null;
}

/**
 * Cleans up a tool approval Decision for display:
 * - picks `command` (or `CommandLine` when `command` is absent);
 * - omits duplicate `CommandLine` when equal to `command`;
 * - lifts `BypassSandbox: true` into `outsideSandbox` instead of dumping raw JSON;
 * - clamps commands longer than MAX_CMD_LINES;
 * - surfaces the judge's escalation note if present.
 */
export function formatApproval(d: Pick<Decision, "input" | "judgeNote">, opts: { omitCwd?: boolean } = {}): FormattedApproval {
  const input = d.input ?? {};
  const rawCmd =
    typeof input.command === "string"
      ? input.command
      : typeof input.CommandLine === "string"
        ? input.CommandLine
        : null;
  const outsideSandbox = input.BypassSandbox === true;
  const rest: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(input)) {
    if (k === "command") continue;
    if (opts.omitCwd && k === "cwd") continue;
    if (k === "BypassSandbox" && typeof v === "boolean") continue;
    if (k === "CommandLine" && typeof v === "string" && (v === rawCmd || rawCmd === v)) continue;
    rest[k] = v;
  }

  let cmd = rawCmd;
  let truncated = false;
  let hiddenLines = 0;
  if (rawCmd !== null) {
    const lines = rawCmd.split("\n");
    if (lines.length > MAX_CMD_LINES) {
      cmd = lines.slice(0, MAX_CMD_LINES).join("\n");
      truncated = true;
      hiddenLines = lines.length - MAX_CMD_LINES;
    }
  }
  const judgeNote = d.judgeNote?.trim() || null;
  return { cmd, fullCmd: rawCmd, truncated, hiddenLines, outsideSandbox, rest, judgeNote };
}
