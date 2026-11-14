// The key-hint bar: which keys work right now. Pure.

export interface HintContext {
  dialog: "new" | "adopt" | "snooze" | null;
  zone: "list" | "thread";
  hasSelection: boolean;
  picked: number;
  view: "open" | "peek" | null;
  card: "ask" | "approval" | "effect" | null;
  cardMulti: boolean;
  running: boolean;
  failed: boolean;
  undo: boolean;
  inText: boolean;
}

export type Hint = [key: string, label: string];

export function keyHints(c: HintContext): Hint[] {
  if (c.dialog === "snooze") return [["1–3", "snooze"], ["Esc", "close"]];
  if (c.dialog) return [["⏎", c.dialog === "new" ? "create" : "adopt"], ["⇧⏎", "new line"], ["Esc", "close"]];
  if (c.inText) {
    const h: Hint[] = [["⏎", "send"], ["⇧⏎", "new line"], ["Esc", "leave field"]];
    if (c.running) h.push(["Esc Esc", "interrupt"]);
    return h;
  }
  const h: Hint[] = [];
  if (c.zone === "thread" && c.view === "open" && c.card === "ask") {
    h.push(["1–4", c.cardMulti ? "toggle" : "pick"], ["0", "other"], ["Tab", "next question"], ["⏎", "answer"]);
  } else if (c.zone === "thread" && c.view === "open" && (c.card === "approval" || c.card === "effect")) {
    h.push(["1", c.card === "approval" ? "allow" : "approve"], ["2", c.card === "approval" ? "deny" : "decline"]);
  }
  h.push(["j/k", "move"]);
  if (c.hasSelection) h.push(["⏎", c.zone === "thread" && c.view === "open" && !c.card ? "write" : "open"], ["Space", "peek"]);
  h.push(["n", "next needing you"]);
  if (c.hasSelection || c.view) h.push(["e", "settle"], ["s", "snooze"], ["#", "archive"], ["u", "unseen"], ["x", "select"]);
  if (c.picked > 0) h.push(["⇧E", `settle ${c.picked}`]);
  else if (c.hasSelection) h.push(["⇧E", "settle group"]);
  if (c.failed) h.push(["r", "retry"]);
  h.push(["c", "new"], ["g1–5", "section"]);
  if (c.undo) h.push(["⌘Z", "undo"]);
  if (c.view === "peek") h.push(["Esc", "close peek"]);
  else if (c.running && c.zone === "thread") h.push(["Esc", "interrupt"]);
  return h;
}
