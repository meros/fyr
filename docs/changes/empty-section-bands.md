# V0.empty-section-bands: empty section headers keep the band in a softer shade

→ V0.section-bands. *Amends V0.section-bands (3): leaving empty headers unshaded broke the list's rhythm and shifted their icons slightly on the phone.*

(1) **Soft band.** An empty section header is drawn as a band of the same size and air as a populated one, filled with `--surface` instead of `--sunken` so it reads at lower contrast against the `--raised` list while still reading as a section header.

(2) **Alignment.** Every section header shares the same horizontal padding on both fronts, so icons line up across populated and empty sections alike.

(3) **Both fronts.** The same rule on the desktop (`.sec`) and the phone (`.m-sec`), in both light and dark themes.
