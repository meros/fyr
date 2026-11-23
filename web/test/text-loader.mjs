// Loaded with --import before the unit tests (package.json): an .html import is its text, as esbuild's text loader
// gives the bundle (scripts/build.mjs). The one such file is the live view's bridge, which the Go host embeds too.
import { registerHooks } from "node:module";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

registerHooks({
  load(url, context, next) {
    if (!url.startsWith("file:") || !url.endsWith(".html")) return next(url, context);
    return { format: "module", source: `export default ${JSON.stringify(readFileSync(fileURLToPath(url), "utf8"))};`, shortCircuit: true };
  },
});
