// SPEC 0.5 V0.79: the composer's `/` list. Typing `/` at the very start opens the agent's commands and skills
// that match the typed name; arrow keys move, Tab or Enter picks, Escape closes. A pick fills `/name `; a send
// sends the text unchanged and the CLI runs it. Pure logic; slash-ui.tsx renders it for both shells.

/** One command or skill as the host serves it (GET /api/threads/{id}/commands). */
export interface SlashCommand {
  name: string;
  description?: string;
  argumentHint?: string;
}

/** The most rows the list shows. */
export const SLASH_MAX = 8;

/** The name being typed when the text is `/` and a name with no whitespace yet; else null. */
export function slashQuery(text: string): string | null {
  const m = /^\/(\S*)$/.exec(text);
  return m ? m[1] : null;
}

/** Names that start with the query (by name), then names that contain it, at most SLASH_MAX; case does not matter. */
export function matchCommands(list: SlashCommand[], query: string): SlashCommand[] {
  const q = query.toLowerCase();
  const byName = (a: SlashCommand, b: SlashCommand) => a.name.localeCompare(b.name);
  const starts = list.filter((c) => c.name.toLowerCase().startsWith(q)).sort(byName);
  const contains = list.filter((c) => !c.name.toLowerCase().startsWith(q) && c.name.toLowerCase().includes(q)).sort(byName);
  return [...starts, ...contains].slice(0, SLASH_MAX);
}

/** Whether the list shows for this text: a query with matches, and not the text the owner closed it at. */
export function slashOpen(text: string, list: SlashCommand[], dismissedAt: string | null): boolean {
  const q = slashQuery(text);
  if (q === null || text === dismissedAt) return false;
  return matchCommands(list, q).length > 0;
}

type SlashKey = { kind: "move"; index: number } | { kind: "pick"; command: SlashCommand } | { kind: "close" };

/** What a key does while the list is open; null lets the field have it. */
export function slashKey(key: string, items: SlashCommand[], index: number): SlashKey | null {
  if (!items.length) return null;
  const n = items.length;
  const i = Math.min(Math.max(index, 0), n - 1);
  switch (key) {
    case "ArrowDown":
      return { kind: "move", index: (i + 1) % n };
    case "ArrowUp":
      return { kind: "move", index: (i - 1 + n) % n };
    case "Tab":
    case "Enter":
      return { kind: "pick", command: items[i] };
    case "Escape":
      return { kind: "close" };
  }
  return null;
}

/** The composer's text after a pick. */
export function pickText(c: SlashCommand): string {
  return `/${c.name} `;
}

/** The picked command and its argument hint while nothing follows its name, for the muted line above the field. */
export function argHint(text: string, list: SlashCommand[]): { name: string; hint: string } | null {
  const m = /^\/(\S+)[ \t]+$/.exec(text);
  if (!m) return null;
  const c = list.find((x) => x.name === m[1]);
  return c?.argumentHint ? { name: c.name, hint: c.argumentHint } : null;
}
