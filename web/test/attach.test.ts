import { test } from "node:test";
import assert from "node:assert/strict";
import { MAX_BYTES, addChips, chipDone, chipError, chipProgress, downloadUrl, fileLabel, pasteMarkers, pastedName, formatSize, removeChip, sendArgs, uploadError } from "../src/attach.ts";

const png = { key: "a", name: "shot.png", size: 2048, type: "image/png", preview: "blob:x" };
const log = { key: "b", name: "build.log", size: 30_000, type: "text/plain" };

test("a chip goes uploading → progress → done with the host's id", () => {
  let c = addChips([], [png]);
  assert.equal(c[0].status, "uploading");
  c = chipProgress(c, "a", 1024, 2048);
  assert.equal(c[0].progress, 0.5);
  c = chipDone(c, "a", { id: "att_1", name: "shot.png", size: 2048, mediaType: "image/png" });
  assert.deepEqual([c[0].status, c[0].id, c[0].progress], ["done", "att_1", 1]);
  assert.equal(chipProgress(c, "a", 1, 2)[0].progress, 1, "progress after done is ignored");
});

test("errors: 413, unreachable, other; a file over 25 MB fails without uploading", () => {
  let c = addChips([], [log]);
  c = chipError(c, "b", 413);
  assert.deepEqual([c[0].status, c[0].error], ["error", "Over 25 MB"]);
  assert.equal(uploadError(null), "Upload failed: Fyr is not reachable");
  assert.equal(uploadError(500), "Upload failed (HTTP 500)");
  assert.equal(uploadError(400, "bad name"), "Upload failed: bad name");
  const big = addChips([], [{ key: "c", name: "video.mov", size: MAX_BYTES + 1, type: "video/quicktime" }]);
  assert.equal(big[0].status, "error");
  assert.match(big[0].error!, /^Over 25 MB/);
});

test("remove drops the chip", () => {
  const c = addChips([], [png, log]);
  assert.deepEqual(removeChip(c, "a").map((x) => x.key), ["b"]);
});

test("send args: text and ids; attachments only is fine; uploads and errors block", () => {
  let c = addChips([], [png, log]);
  assert.deepEqual(sendArgs("hi", c), { ok: false, reason: "Waiting for uploads" });
  c = chipDone(c, "a", { id: "att_1", name: "shot.png", size: 2048, mediaType: "image/png" });
  c = chipError(c, "b", 413);
  assert.deepEqual(sendArgs("hi", c), { ok: false, reason: "Remove the files that failed" });
  c = removeChip(c, "b");
  assert.deepEqual(sendArgs("", c), { ok: true, args: { text: "", attachments: ["att_1"] } });
  assert.deepEqual(sendArgs("hi", []), { ok: true, args: { text: "hi" } });
  assert.deepEqual(sendArgs("  ", []), { ok: false, reason: "Nothing to send" });
});

test("type labels and sizes", () => {
  assert.equal(fileLabel("application/pdf", "spec.pdf"), "PDF");
  assert.equal(fileLabel("image/png", "shot.png"), "PNG");
  assert.equal(fileLabel("image/heic", "IMG_1"), "HEIC");
  assert.equal(fileLabel("video/mp4", "clip.mp4"), "Video");
  assert.equal(fileLabel("text/plain", "build.log"), "LOG");
  assert.equal(fileLabel("text/plain", "README"), "Text");
  assert.equal(fileLabel("application/zip", "a.zip"), "ZIP");
  assert.equal(fileLabel("", "noext"), "File");
  assert.equal(formatSize(812), "812 B");
  assert.equal(formatSize(30_000), "29 KB");
  assert.equal(formatSize(23_100_000), "22 MB");
  assert.equal(formatSize(1_258_291), "1.2 MB");
});

test("V0.download-assets: a host's attachment is saved with ?download=1; a data or object URL stays as it is", () => {
  assert.equal(downloadUrl("/api/h/H1/attachments/a_1"), "/api/h/H1/attachments/a_1?download=1");
  assert.equal(downloadUrl("/api/h/H1/attachments/a_1?v=2"), "/api/h/H1/attachments/a_1?v=2&download=1");
  assert.equal(downloadUrl("data:image/png;base64,AAAA"), "data:image/png;base64,AAAA");
  assert.equal(downloadUrl("blob:http://x/1"), "blob:http://x/1");
});

test("V0.paste-inline: a pasted clipboard image is named image-N, a named file keeps its name", () => {
  assert.equal(pastedName("image.png", "image/png", new Set()), "image-1.png");
  assert.equal(pastedName("", "image/jpeg", new Set(["image-1.jpeg"])), "image-2.jpeg");
  assert.equal(pastedName("image.png", "image/png", new Set(["image-1.png", "image-2.png"])), "image-3.png");
  assert.equal(pastedName("diagram.png", "image/png", new Set()), "diagram.png");
  assert.equal(pastedName("diagram.png", "image/png", new Set(["diagram.png"])), "diagram-1.png");
  assert.equal(pastedName("report.pdf", "application/pdf", new Set()), "report.pdf");
  assert.equal(pastedName("", "image/svg+xml", new Set()), "image-1.svg");
});

test("V0.paste-inline: the markers go at the caret with a space where they would touch a word", () => {
  assert.equal(pasteMarkers(["image-1.png"], "", ""), "[image-1.png]");
  assert.equal(pasteMarkers(["image-1.png"], "look at", "here"), " [image-1.png] ");
  assert.equal(pasteMarkers(["image-1.png"], "look at ", " here"), "[image-1.png]");
  assert.equal(pasteMarkers(["a.png", "b.png"], "x\n", ""), "[a.png] [b.png]");
});
