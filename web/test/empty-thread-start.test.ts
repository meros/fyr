// V0.empty-thread-start: New thread starts with nothing in it; uploads that wait or failed still hold Start.
import { test } from "node:test";
import assert from "node:assert/strict";
import { startArgs } from "../src/new-thread.ts";
import type { Chip } from "../src/attach.ts";

const chip = (status: Chip["status"], id?: string) => ({ key: "k", name: "a.png", size: 1, mediaType: "image/png", status, progress: 1, id }) as Chip;

test("V0.empty-thread-start: an empty form starts an empty thread", () => {
  assert.deepEqual(startArgs("", []), { ok: true, empty: true, args: { text: "" } });
  assert.deepEqual(startArgs("  \n", []), { ok: true, empty: true, args: { text: "" } });
  assert.deepEqual(startArgs("hi", []), { ok: true, empty: false, args: { text: "hi" } });
  assert.deepEqual(startArgs("", [chip("done", "f1")]), { ok: true, empty: false, args: { text: "", attachments: ["f1"] } });
  assert.deepEqual(startArgs("", [chip("uploading")]), { ok: false, reason: "Waiting for uploads" });
  assert.deepEqual(startArgs("", [chip("error")]), { ok: false, reason: "Remove the files that failed" });
});
