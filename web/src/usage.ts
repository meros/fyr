/** V0.usage-ledger (decision 0041): a thread's tokens, as both fronts show them. Tokens only: no money, since a
 *  subscription is not billed per token. The total is a plain sum of the kinds, a count and not a weight; the title
 *  says each kind. */

/** A sum of tokens by kind, as the host sends it (api.Usage). Thinking is part of output. */
export interface Usage {
  input?: number;
  output?: number;
  cacheRead?: number;
  cacheWrite5m?: number;
  cacheWrite1h?: number;
  thinking?: number;
  total?: number;
}

/** 950 → "950", 12_345 → "12k", 1_234_567 → "1.2M", 25_000_000 → "25M". */
export function tokens(n: number | undefined): string {
  const v = Math.max(0, Math.round(n ?? 0));
  if (v < 1000) return String(v);
  if (v < 1_000_000) return `${v < 10_000 ? (v / 1000).toFixed(1).replace(/\.0$/, "") : Math.round(v / 1000)}k`;
  if (v < 1_000_000_000) return `${v < 10_000_000 ? (v / 1e6).toFixed(1).replace(/\.0$/, "") : Math.round(v / 1e6)}M`;
  return `${(v / 1e9).toFixed(1).replace(/\.0$/, "")}B`;
}

/** The chip's words, or "" when the thread has counted nothing. */
export function usageLabel(u: Usage | undefined): string {
  if (!u || !u.total) return "";
  return `${tokens(u.total)} tok`;
}

/** The chip's title: each kind on its own line. */
export function usageTitle(u: Usage | undefined): string {
  if (!u || !u.total) return "";
  const lines = [
    `Tokens this thread used: ${tokens(u.total)}`,
    `Input ${tokens(u.input)} · output ${tokens(u.output)}${u.thinking ? ` (thinking ${tokens(u.thinking)})` : ""}`,
    `Cache read ${tokens(u.cacheRead)} · cache write ${tokens((u.cacheWrite5m ?? 0) + (u.cacheWrite1h ?? 0))}`,
    "A plain sum of the kinds, with subagents and the CLI's own calls",
  ];
  return lines.join("\n");
}
