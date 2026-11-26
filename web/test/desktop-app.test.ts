// V0.desktop-chromium: the Linux app's Chromium window is known by its user agent from the first request, so the
// client registers no service worker there even before the app's bridge exists.
import { test } from "node:test";
import assert from "node:assert/strict";
import { isDesktopApp } from "../src/pwa.ts";

test("the app's window is known by FyrDesktop/ in its user agent", () => {
  assert.equal(isDesktopApp("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36 FyrDesktop/linux"), true);
  assert.equal(isDesktopApp("Mozilla/5.0 (Macintosh) AppleWebKit/605.1.15 (KHTML, like Gecko) FyrDesktop/mac"), true);
  assert.equal(isDesktopApp("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/157.0.0.0 Safari/537.36"), false);
});
