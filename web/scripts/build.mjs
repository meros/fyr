// Build web/dist: index.html, app.js, app.css and the self-hosted Geist fonts.
import { build } from "esbuild";
import { copyFile, mkdir, readdir, rm, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

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

await writeFile(
  join(dist, "index.html"),
  `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Fyr</title>
<link rel="preload" href="fonts/Geist-Variable.woff2" as="font" type="font/woff2" crossorigin>
<link rel="stylesheet" href="app.css">
<script>try{var t=localStorage.getItem("fyr.theme");if(t==="light"||t==="dark")document.documentElement.setAttribute("data-theme",t)}catch(e){}</script>
</head>
<body>
<div id="app"></div>
<script type="module" src="app.js"></script>
</body>
</html>
`,
);
console.log("built web/dist");
