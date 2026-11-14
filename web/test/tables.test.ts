// SPEC 0.5 V0.35: tables in messages stay readable at any width. Each cell carries
// its column name, so a narrow screen can show each row as a card.
import { test } from "node:test";
import assert from "node:assert/strict";
import { renderMarkdown } from "../src/markdown.ts";

const src = "| Commit | Change |\n|---|---|\n| `3d0a470` | Finished in **14 min** |\n| a0e8242 | Drafts <b>survive</b> |\n";

test("a table is wrapped and every cell names its column", () => {
  const html = renderMarkdown(src);
  assert.match(html, /<div class="tblw"><table>/);
  assert.match(html, /<td data-label="Commit"><code>3d0a470<\/code><\/td>/);
  assert.match(html, /<td data-label="Change">Finished in <strong>14 min<\/strong><\/td>/);
});

test("cell content stays escaped, and labels are plain text", () => {
  const html = renderMarkdown(src);
  assert.doesNotMatch(html, /<b>survive<\/b>/);
  const h = renderMarkdown('| A "q" <x> |\n|---|\n| v |\n');
  assert.match(h, /data-label="A &quot;q&quot; &lt;x&gt;"/);
});
