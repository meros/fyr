// SPEC 0.5 V0.34: every output is one tap from the clipboard. A code block in a
// message carries its own Copy button; the copied text is the code, unescaped.
import { test } from "node:test";
import assert from "node:assert/strict";
import { renderMarkdown } from "../src/markdown.ts";
import { copySource } from "../src/copy.ts";

test("a fenced code block renders with a Copy button", () => {
  const html = renderMarkdown("Run this:\n\n```sh\nyarn test --watch=false <x>\n```\n");
  assert.match(html, /<div class="codeb">/);
  assert.match(html, /<button type="button" class="cpy" data-cpy[^>]*>Copy<\/button>/);
  assert.match(html, /<pre><code[^>]*>yarn test --watch=false &lt;x&gt;\n?<\/code><\/pre>/);
});

test("raw HTML in a code block stays escaped", () => {
  const html = renderMarkdown("```\n<script>alert(1)</script>\n```");
  assert.doesNotMatch(html, /<script>/);
});

test("what a Copy button copies: a message is its markdown source, a block its text", () => {
  assert.equal(copySource({ kind: "message", text: "**Done.** See `x`." }), "**Done.** See `x`.");
  assert.equal(copySource({ kind: "block", text: "line 1\nline 2\n" }), "line 1\nline 2");
});
