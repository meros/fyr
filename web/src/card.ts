// V0.load-card, V0.hover-cards: a hover card's words, pure. A head and an optional verdict in its colour, an optional
// sentence, then groups of rows (label, value and a quieter note), and a quiet last line. The host's load card
// (load.ts), the quota card (quota.ts), the rail's undeclared and leftover cards (undeclared.ts, leftovers.ts) are
// each one of these; CardView (card-ui.tsx) draws them all.

export type CardLevel = "calm" | "warm" | "hot";

export interface CardRow {
  label: string;
  value: string;
  /** quieter words after the value */
  note?: string;
  hot?: boolean;
}
export interface CardSection {
  title?: string;
  /** a list: each row is its value and note on one line, no label column */
  list?: boolean;
  rows: CardRow[];
}
export interface Card {
  /** the card's name: a host, a provider, what the list is */
  head?: string;
  /** the head is a name to read letter by letter (a host) */
  headMono?: boolean;
  level?: CardLevel;
  /** one or two words in the level's colour: "Calm", "Running low" */
  verdict?: string;
  /** one sentence: what it means for the owner */
  summary?: string;
  sections: CardSection[];
  /** a quiet last line: where the figures come from, how old they are, what a click does */
  source?: string;
}

/** The card as plain lines (the last line, `source`, apart), "Label: value · note" (a row without a label continues the one above); for tests and logs. */
export function cardLines(c: Card): string[] {
  const top = [c.verdict, c.summary].filter(Boolean).join(": ");
  const rows = c.sections.flatMap((s) => s.rows.map((r) => `${s.list ? "" : `${r.label || "…"}: `}${r.value}${r.note ? ` · ${r.note}` : ""}`));
  return [...(top ? [top] : []), ...rows];
}
