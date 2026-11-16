// V0.108: Regenerate title. The thread says when Fyr could not make a title; the title stays.
import { test } from "node:test";
import assert from "node:assert/strict";
import { buildConversation } from "../src/conversation.ts";
import { threadMenu } from "../src/menu.ts";
import { thread } from "./helpers.ts";

test("a failed title call becomes a failed line in the thread", () => {
  const items = buildConversation([
    { seq: 1, at: "2026-11-16T14:00:00Z", subject: "t", type: "effect.error", data: { kind: "title", error: "Fyr could not regenerate the title: no answer in time. The title stays." } },
  ]);
  assert.equal(items.length, 1);
  assert.equal(items[0].kind, "effect");
  assert.equal((items[0] as { ok: boolean }).ok, false);
  assert.equal((items[0] as { line: string }).line, "Fyr could not regenerate the title: no answer in time. The title stays.");
});

test("the row menu offers Regenerate title, disabled with the host's reason", () => {
  const ok = threadMenu(thread("t1", "seen", "seen")).find((x) => !("sep" in x) && x.id === "regenTitle");
  assert.ok(ok && !("sep" in ok) && ok.label === "Regenerate title" && !ok.disabled);
  const off = threadMenu(thread("t2", "seen", "seen", { unavailable: { "thread.regenerateTitle": "Offline" } })).find((x) => !("sep" in x) && x.id === "regenTitle");
  assert.equal(off && !("sep" in off) ? off.disabled : "", "Offline");
});
