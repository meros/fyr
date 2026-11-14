// Build web/dist: index.html, app.js, app.css, the self-hosted Geist fonts, and the PWA parts
// (manifest.webmanifest, icons/, sw.js). Node runs the .ts helpers with its built-in type stripping.
import { build } from "esbuild";
import { createHash } from "node:crypto";
import { copyFile, mkdir, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { markPng } from "./icons.ts";
import { THEME, manifest } from "./manifest.ts";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const dist = join(root, "dist");
const watch = process.argv.includes("--dev");

// Clean dist but keep .keep (the Go binary embeds web/dist).
await mkdir(dist, { recursive: true });
for (const f of await readdir(dist)) if (f !== ".keep") await rm(join(dist, f), { recursive: true, force: true });

await build({
  entryPoints: [join(root, "src/app.tsx")],
  outfile: join(dist, "app.js"),
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

// Icons: the Fyr mark, drawn and PNG-encoded here (scripts/icons.ts).
await mkdir(join(dist, "icons"), { recursive: true });
const icons = {
  "icon-192.png": markPng(192),
  "icon-512.png": markPng(512),
  "maskable-512.png": markPng(512, { maskable: true }),
  "apple-touch-icon.png": markPng(180, { maskable: true }), // iOS rounds the corners itself
  "badge-96.png": markPng(96, { badge: true }), // Android status bar: white on transparent
};
for (const [name, png] of Object.entries(icons)) await writeFile(join(dist, "icons", name), png);

await writeFile(join(dist, "manifest.webmanifest"), JSON.stringify(manifest(), null, 2) + "\n");

await writeFile(
  join(dist, "index.html"),
  `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<title>Fyr</title>
<link rel="manifest" href="/manifest.webmanifest">
<meta name="theme-color" content="${THEME.light}" media="(prefers-color-scheme: light)">
<meta name="theme-color" content="${THEME.dark}" media="(prefers-color-scheme: dark)">
<link rel="icon" type="image/png" sizes="192x192" href="/icons/icon-192.png">
<link rel="apple-touch-icon" sizes="180x180" href="/icons/apple-touch-icon.png">
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
const shell = [
  "/index.html",
  "/app.js",
  "/app.css",
  "/manifest.webmanifest",
  "/fonts/Geist-Variable.woff2",
  "/fonts/GeistMono-Variable.woff2",
  ...Object.keys(icons).map((n) => `/icons/${n}`),
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
