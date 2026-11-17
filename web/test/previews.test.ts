import { test } from "node:test";
import assert from "node:assert/strict";
import { FRAME_ALLOW, FRAME_MAX, filmstrip, frameHeight, inlineAttachmentId, previewCard, stepImage, thumbBox } from "../src/previews.ts";
import { buildConversation, previewsOf } from "../src/conversation.ts";
import { renderMarkdown, setMarkdownOrigin } from "../src/markdown.ts";

const at = "2026-11-14T09:00:00Z";

test("card model per kind", () => {
  const img = previewCard(3, at, { kind: "image", attachmentId: "a1", title: "Sheet", caption: "c", width: 390, height: 844, url: "http://localhost:5173/x" }, "H1");
  assert.equal(img?.src, "/api/h/H1/attachments/a1");
  assert.equal(img?.url, "http://localhost:5173/x");
  const html = previewCard(4, at, { kind: "html", attachmentId: "p1", title: "Chart", width: 640, height: 300 }, "H1");
  assert.equal(html?.src, "/api/h/H1/previews/p1");
  assert.equal(html?.caption, "");
  assert.equal(previewCard(5, at, { kind: "video", attachmentId: "x" }, "H1"), null, "unknown kind");
  assert.equal(previewCard(6, at, { kind: "image" }, "H1"), null, "no id");
  assert.equal(previewCard(7, at, { kind: "image", attachmentId: "a", url: "javascript:alert(1)" }, "H1")?.url, undefined, "only http(s) links");
});

test("frame height: min(height, 600) at first, never over 2,000", () => {
  assert.equal(frameHeight(300), 300);
  assert.equal(frameHeight(1800), 600);
  assert.equal(frameHeight(1800, true), 1800);
  assert.equal(frameHeight(50_000, true), FRAME_MAX);
  assert.equal(frameHeight(0), 400, "unknown height");
});

test("V0.47 frame height: 16:10 of the card without a size; the page's own report wins", () => {
  assert.equal(frameHeight(0, false, 0, 640), 400);
  assert.equal(frameHeight(0, false, 0, 358), 224, "phone width");
  assert.equal(frameHeight(300, false, 0, 640), 300, "an explicit size");
  assert.equal(frameHeight(0, false, 900, 640), 600, "a report starts at most 600 px");
  assert.equal(frameHeight(0, true, 900, 640), 900, "Show all");
  assert.equal(frameHeight(0, false, 20, 640), 40, "at least 40 px");
  assert.match(FRAME_ALLOW, /fullscreen/);
  assert.match(FRAME_ALLOW, /xr-spatial-tracking/);
});

test("thumbnail box keeps the aspect inside 360 px tall", () => {
  assert.deepEqual(thumbBox(390, 844, 680), { width: 166, height: 360 });
  assert.deepEqual(thumbBox(1280, 800, 680), { width: 576, height: 360 });
  assert.deepEqual(thumbBox(200, 100, 680), { width: 200, height: 100 });
});

test("only same-origin /api/attachments/{id} images are inline", () => {
  const o = "https://desk.tail.ts.net:7443";
  assert.equal(inlineAttachmentId("/api/attachments/a_1", o), "a_1");
  assert.equal(inlineAttachmentId("https://desk.tail.ts.net:7443/api/attachments/a_1", o), "a_1");
  assert.equal(inlineAttachmentId("https://evil.example/api/attachments/a_1", o), null);
  assert.equal(inlineAttachmentId("/api/attachments/a_1/../../state", o), null);
  assert.equal(inlineAttachmentId("/api/previews/p1", o), null);
  assert.equal(inlineAttachmentId("/api/attachments/a_1?x=1", o), null);
  assert.equal(inlineAttachmentId("data:image/png;base64,AAAA", o), null);
  setMarkdownOrigin(o);
  const html = renderMarkdown("![shot](/api/attachments/a_1) ![x](https://example.com/x.png)", "H1");
  assert.ok(html.includes('<img class="md-img" src="/api/h/H1/attachments/a_1"'), "the agent's path is read from its thread's host");
  assert.ok(!html.includes('src="https://example.com'), "remote images stay links");
  assert.ok(html.includes('href="https://example.com/x.png"'));
  setMarkdownOrigin(null);
});

test("preview events: cards in the log in time order, filmstrip newest first, viewer steps over images", () => {
  const evs = [
    { seq: 1, at, subject: "t", type: "user", data: { text: "show me" } },
    { seq: 2, at, subject: "t", type: "preview", data: { kind: "image", attachmentId: "a", title: "one", width: 1, height: 1 } },
    { seq: 3, at, subject: "t", type: "preview", data: { kind: "html", attachmentId: "h", title: "two" } },
    { seq: 4, at, subject: "t", type: "preview", data: { kind: "image", attachmentId: "b", title: "three" } },
  ];
  assert.deepEqual(buildConversation(evs).map((i) => i.kind), ["user", "preview", "preview", "preview"]);
  const cards = previewsOf(evs);
  assert.deepEqual(filmstrip(cards).map((c) => c.seq), [4, 3, 2]);
  assert.equal(stepImage(cards, 2, 1)?.seq, 4, "skips the HTML preview");
  assert.equal(stepImage(cards, 4, 1), null);
  assert.equal(stepImage(cards, 4, -1)?.seq, 2);
});
