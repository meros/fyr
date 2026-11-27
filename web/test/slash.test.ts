// SPEC 0.5 V0.79: the composer's `/` list: which commands it shows, how its keys move and pick, and what a pick fills.
import { test } from "node:test";
import assert from "node:assert/strict";
import { argHint, browseCommands, matchCommands, pickInto, pickText, SLASH_MAX, slashKey, slashOpen, slashQuery, type SlashCommand } from "../src/slash.ts";

const list: SlashCommand[] = [
  { name: "compact", description: "Clear the conversation but keep a summary", argumentHint: "<instructions>" },
  { name: "review", description: "Review a pull request", argumentHint: "<pr-number>" },
  { name: "pr-comments", description: "Get the comments of a PR" },
  { name: "security-review", description: "Review the branch for security" },
  { name: "init", description: "Write a CLAUDE.md" },
];

test("a query is open only while a name is typed at the very start", () => {
  assert.equal(slashQuery("/"), "");
  assert.equal(slashQuery("/rev"), "rev");
  assert.equal(slashQuery("/review "), null, "a space ends the name");
  assert.equal(slashQuery("/review\n"), null);
  assert.equal(slashQuery(" /review"), null);
  assert.equal(slashQuery("see /review"), null);
  assert.equal(slashQuery(""), null);
});

test("names that start with the query come first, then names that contain it", () => {
  assert.deepEqual(
    matchCommands(list, "rev").map((c) => c.name),
    ["review", "security-review"],
  );
  assert.deepEqual(
    matchCommands(list, "REV").map((c) => c.name),
    ["review", "security-review"],
    "case does not matter",
  );
  assert.deepEqual(
    matchCommands(list, "").map((c) => c.name),
    ["compact", "init", "pr-comments", "review", "security-review"],
    "everything, by name",
  );
  assert.deepEqual(matchCommands(list, "zzz"), []);
  const many = Array.from({ length: 20 }, (_, i) => ({ name: `skill-${String(i).padStart(2, "0")}` }));
  assert.equal(matchCommands(many, "skill").length, SLASH_MAX);
});

test("the list opens for a matching query and stays closed after Escape until the text changes", () => {
  assert.equal(slashOpen("/rev", list, null), true);
  assert.equal(slashOpen("/zzz", list, null), false, "nothing matches");
  assert.equal(slashOpen("/rev", [], null), false, "no commands known");
  assert.equal(slashOpen("/rev", list, "/rev"), false, "dismissed at this text");
  assert.equal(slashOpen("/revi", list, "/rev"), true, "typing again opens it");
  assert.equal(slashOpen("hello", list, null), false);
});

test("arrow keys move and wrap, Tab and Enter pick, Escape closes, other keys pass", () => {
  const items = matchCommands(list, "");
  assert.deepEqual(slashKey("ArrowDown", items, 0), { kind: "move", index: 1 });
  assert.deepEqual(slashKey("ArrowDown", items, items.length - 1), { kind: "move", index: 0 });
  assert.deepEqual(slashKey("ArrowUp", items, 0), { kind: "move", index: items.length - 1 });
  assert.deepEqual(slashKey("Tab", items, 2), { kind: "pick", command: items[2] });
  assert.deepEqual(slashKey("Enter", items, 3), { kind: "pick", command: items[3] });
  assert.deepEqual(slashKey("Enter", items, 99), { kind: "pick", command: items[items.length - 1] }, "an index past the end picks the last");
  assert.deepEqual(slashKey("Escape", items, 0), { kind: "close" });
  assert.equal(slashKey("a", items, 0), null);
  assert.equal(slashKey("Enter", [], 0), null, "an empty list lets Enter send");
});

test("a pick fills the name and a space; the hint shows until something follows", () => {
  assert.equal(pickText(list[1]), "/review ");
  assert.deepEqual(argHint("/review ", list), { name: "review", hint: "<pr-number>" });
  assert.deepEqual(argHint("/review", list), null, "still typing the name");
  assert.deepEqual(argHint("/review 12", list), null, "an argument follows");
  assert.deepEqual(argHint("/init ", list), null, "no hint to show");
  assert.deepEqual(argHint("/unknown ", list), null);
});

test("V0.new-thread-skills: the Skills list has every command by name, and a pick goes before what was typed", () => {
  const all = browseCommands([...list, ...Array.from({ length: 12 }, (_, i) => ({ name: `z${i}` }))]);
  assert.equal(all.length, list.length + 12, "no cap: the list scrolls");
  assert.equal(all[0].name, "compact");
  const review = list[1];
  assert.equal(pickInto("", review), "/review ");
  assert.equal(pickInto("fix the login bug", review), "/review fix the login bug");
  assert.equal(pickInto("/init the repo", review), "/review the repo", "the first word runs as the command: one at most");
  assert.equal(pickInto("/ini", review), "/review ");
});
