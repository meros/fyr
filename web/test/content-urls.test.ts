// #41: one ApiContext replaces the module-level setters; what used to be set once (the content URLs) is
// passed in, so a test hands in a fake Api's URLs and the app hands in its own.
import { test } from "node:test";
import assert from "node:assert/strict";
import type { ContentUrls } from "../src/attach.ts";
import { buildConversation, previewsOf } from "../src/conversation.ts";
import { renderMarkdown, setMarkdownOrigin } from "../src/markdown.ts";
import { hostUrls, previewCard } from "../src/previews.ts";
import type { FyrEvent } from "../src/types.ts";
import { hid } from "./helpers.ts";

const fake: ContentUrls = {
  attachmentUrl: (host, id) => `fake:att/${id}@${host}`,
  previewUrl: (host, id) => `fake:pv/${id}@${host}`,
};

const preview = (seq: number, kind: string, id: string): FyrEvent => ({ seq, at: "2026-11-15T10:00:00Z", type: "preview", data: { kind, attachmentId: id } }) as FyrEvent;

test("#41: previews load from the Api's URLs that are passed in", () => {
  assert.equal(previewCard(1, "", { kind: "image", attachmentId: "a1" }, hid("h"), fake)?.src, "fake:att/a1@h");
  assert.equal(previewCard(2, "", { kind: "html", attachmentId: "p1" }, hid("h"), fake)?.src, "fake:pv/p1@h");
  assert.deepEqual(previewsOf([preview(3, "html", "p2")], hid("h"), fake).map((c) => c.src), ["fake:pv/p2@h"]);
  const items = buildConversation([preview(4, "image", "a2")], { host: hid("h"), urls: fake });
  assert.deepEqual(items.flatMap((i) => (i.kind === "preview" ? [i.card.src] : [])), ["fake:att/a2@h"]);
  assert.equal(previewCard(5, "", { kind: "image", attachmentId: "a3" }, hid("F7QK-2M9X"))?.src, hostUrls.attachmentUrl(hid("F7QK-2M9X"), "a3"), "the host's own paths, under its prefix");
});

test("#41: markdown images use the URL function passed in, and the cache does not mix two Apis", () => {
  setMarkdownOrigin("http://localhost");
  try {
    const md = "![shot](/api/attachments/a_1)";
    assert.match(renderMarkdown(md, hid("h"), fake.attachmentUrl), /src="fake:att\/a_1@h"/);
    assert.match(renderMarkdown(md, hid("H1")), /src="\/api\/h\/H1\/attachments\/a_1"/);
  } finally {
    setMarkdownOrigin(null);
  }
});
