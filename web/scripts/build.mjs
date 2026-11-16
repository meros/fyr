// Build web/dist: index.html, app.js, app.css, the self-hosted Geist fonts, and the PWA parts
// (manifest.webmanifest, icons/, sw.js). Node runs the .ts helpers with its built-in type stripping.
import { build } from "esbuild";
import { createHash } from "node:crypto";
import { copyFile, mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { ICON_DIR, ICON_FILES, iconUrl } from "../src/icon-paths.ts";
import { THEME, manifest } from "./manifest.ts";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
// --out DIR builds elsewhere (the bundle test); the default is web/dist, which the Go binary embeds.
const outArg = process.argv.indexOf("--out");
const dist = outArg > 0 ? resolve(process.argv[outArg + 1]) : join(root, "dist");
const watch = process.argv.includes("--dev");

// Clean dist but keep .keep (the Go binary embeds web/dist).
await mkdir(dist, { recursive: true });
for (const f of await readdir(dist)) if (f !== ".keep") await rm(join(dist, f), { recursive: true, force: true });

// Code splitting (#42): the mock Api (src/mock-api.ts, loaded with import() only in mock mode) and the code it
// shares with the app become chunks next to app.js; the service worker precaches every one.
const app = await build({
  entryPoints: [join(root, "src/app.tsx")],
  outdir: dist,
  entryNames: "[name]",
  chunkNames: "[name]-[hash]",
  splitting: true,
  metafile: true,
  bundle: true,
  format: "esm",
  target: "es2022",
  jsx: "automatic",
  jsxImportSource: "preact",
  minify: !watch,
  sourcemap: watch ? "inline" : false,
  legalComments: "none",
  logLevel: "warning",
});

await build({
  entryPoints: [join(root, "src/app.css")],
  outfile: join(dist, "app.css"),
  bundle: true,
  minify: !watch,
  external: ["fonts/*"],
  logLevel: "warning",
});

await mkdir(join(dist, "fonts"), { recursive: true });
const geist = join(root, "node_modules/geist/dist/fonts");
await copyFile(join(geist, "geist-sans/Geist-Variable.woff2"), join(dist, "fonts/Geist-Variable.woff2"));
await copyFile(join(geist, "geist-mono/GeistMono-Variable.woff2"), join(dist, "fonts/GeistMono-Variable.woff2"));
await copyFile(join(root, "node_modules/geist/LICENSE.txt"), join(dist, "fonts/LICENSE.txt")).catch(() => {});

// V0.93: the sidebar logo, an isometric lighthouse rendered with Blender (design/logo/lighthouse.py).
await copyFile(join(root, "assets/logo.png"), join(dist, "logo.png"));

// Icons (V0.110): cut from the rendered lighthouse by design/logo/icons.sh and committed in assets/icons.
const iconDir = join(dist, ICON_DIR.slice(1));
await mkdir(iconDir, { recursive: true });
for (const n of Object.values(ICON_FILES)) await copyFile(join(root, "assets/icons", n), join(iconDir, n));

await writeFile(join(dist, "manifest.webmanifest"), JSON.stringify(manifest(), null, 2) + "\n");

await writeFile(
  join(dist, "index.html"),
  `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content">
<title>Fyr</title>
<link rel="manifest" href="/manifest.webmanifest">
<meta name="theme-color" content="${THEME.light}" media="(prefers-color-scheme: light)">
<meta name="theme-color" content="${THEME.dark}" media="(prefers-color-scheme: dark)">
<link rel="icon" type="image/png" sizes="32x32" href="${iconUrl("favicon32")}">
<link rel="icon" type="image/png" sizes="192x192" href="${iconUrl("any192")}">
<link rel="apple-touch-icon" sizes="180x180" href="${iconUrl("apple180")}">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-title" content="Fyr">
<meta name="apple-mobile-web-app-status-bar-style" content="default">
<link rel="preload" href="/fonts/Geist-Variable.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="/app.css">
<script>try{var t=localStorage.getItem("fyr.theme");if(t==="light"||t==="dark")document.documentElement.setAttribute("data-theme",t)}catch(e){}</script>
</head>
<body>
<div id="app"></div>
<script type="module" src="/app.js"></script>
</body>
</html>
`,
);

// Service worker last: its cache name is a hash of the shell it precaches.
const scripts = Object.keys(app.metafile.outputs).map((p) => "/" + relative(dist, resolve(p)).split("\\").join("/"));
const shell = [
  "/index.html",
  ...scripts,
  "/app.css",
  "/manifest.webmanifest",
  "/fonts/Geist-Variable.woff2",
  "/fonts/GeistMono-Variable.woff2",
  ...Object.values(ICON_FILES).map((n) => `${ICON_DIR}/${n}`),
  "/logo.png",
];
const hash = createHash("sha256");
for (const p of shell) hash.update(await readFile(join(dist, p.slice(1))));
const buildId = hash.digest("hex").slice(0, 12);
await build({
  entryPoints: [join(root, "src/sw.ts")],
  outfile: join(dist, "sw.js"),
  bundle: true,
  format: "iife",
  target: "es2022",
  minify: !watch,
  legalComments: "none",
  logLevel: "warning",
  define: { __BUILD_ID__: JSON.stringify(buildId), __PRECACHE__: JSON.stringify(shell) },
});
console.log(`built web/dist (shell ${buildId})`);
