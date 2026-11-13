# Design

| Path | What |
|---|---|
| `system/README.md` | The brand book: voice, colour, type, space, motion, layout, icons |
| `system/tokens.json` | Tokens (colour in light and dark, type, spacing, radius, shadow). Source of truth |
| `system/tokens.css` | CSS custom properties generated from `tokens.json` |
| `system/components/bundle.js`, `bundle.css`, `index.d.ts` | Reference components (plain React `createElement`, global `window.Fyr`) and their props |
| `system/components/<Name>/` | Each component's usage rules (`README.md`) and a live preview (`preview.html`) |
| `system/components/Cover/preview.html` | The visual identity sheet: palette blocks, dot grid, name |
| `screens.html` | Static mock of the key screens, built from the system |
| `generators/make_ds.py` | Writes everything under `system/` |
| `generators/make_mocks.py` | Builds `screens.html` from `system/` |

Edit the generators, not the generated files, then run:

```sh
python3 design/generators/make_ds.py
python3 design/generators/make_mocks.py
```

Open `screens.html` or any `preview.html` in a browser. The previews expect `tokens.css`, `bundle.css`, React 18 and `bundle.js` to be loaded first; `screens.html` is self-contained.

Changes that are binding for v1 but not yet in the mock are listed in `docs/FYR-V1.md` section 6.
