# Fyr logo

An isometric lighthouse rendered with Blender Cycles on a transparent background (SPEC 0.5 V0.93). The desktop rail shows it at 32 px. Since V0.110 the app icons (favicon, PWA, Apple touch icon, push badge) are cut from the same render.

| File | What |
|---|---|
| `lighthouse.py` | Builds the scene from nothing, frames it and renders it. The only source of truth. |
| `lighthouse.blend` | The scene the script built, for editing in Blender by hand. Port any change back into the script. |
| `lighthouse-512.png` | The master render, 512 px. |
| `../../web/assets/logo.png` | The shipped asset, 128 px (4× the 32 px display). |
| `icons.sh` | Cuts the app icons from the master render with ImageMagick into `web/assets/icons/`: the render as it is (192, 512, favicon 32), the render at 80 % on a night tile (maskable 512, Apple touch 180), and a white silhouette from its alpha (badge 96). |
| `../../web/assets/icons/` | The shipped app icons. The build copies them to `/icons/lighthouse/` (`web/src/icon-paths.ts`). |
| `iterations/` | The four versions of 16 Nov 2026, with the review notes below. |

Render again (Blender 5.2; samples 256 for the shipped 128 px, 160 for the master):

```sh
nix shell nixpkgs#blender --command blender -b -P design/logo/lighthouse.py -- OUT 128 256
cp OUT/lighthouse-128.png web/assets/logo.png
design/logo/icons.sh   # after a new master render: the app icons
```

A new master render needs new icon URLs: the service worker serves `/icons/` cache-first as immutable, so rename `ICON_DIR` in `web/src/icon-paths.ts`.

The scene: a white tower with two slate bands on a plinth, a rock mound with faceted boulders, a dark gallery with a railing, a glass lantern with an amber lamp, a dark dome with a finial, and two amber beams that start outside the glass and fade outward. True isometric camera: orthographic, 35.264° down, 45° around; the frame fits every vertex with a 4 % margin. Light: a sun from the upper left, a soft fill, a cool rim from behind (it outlines the dark cap on a dark page), a dim world, and the lamp's own point light. Colours come from the design tokens; the lamp is deeper than `amber`, because AgX moves a bright emitter toward yellow.

## Iterations

| Version | Review |
|---|---|
| v1 | The base was cut off at the bottom; the lamp read yellow-green and weak; an octagonal slab read as a plinth; the tower white read grey. |
| v2 | Framing, amber lamp, rock mound and a brighter key fixed. At 32 px the lighthouse filled half the tile (the framing used box corners); the boulders read as soft blobs. |
| v3 | Framing from the vertices, two beams, faceted rocks, a rim light. The beams started inside the lantern and tinted the glass into an orange drum. |
| v4 | Beams start outside the glass, stronger near the lamp; a smaller base gives the tower more of the tile. Chosen ("This is good! Use it"). |
