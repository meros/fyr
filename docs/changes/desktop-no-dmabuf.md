# V0.desktop-no-dmabuf: the Linux app turns off WebKitGTK's DMABUF renderer, so the page does not flicker while the owner types

→ amends V0.linux-desktop (the window).

Why: on a Wayland session with Intel graphics the window flickered like a broken double buffer whenever the owner typed. Turning off WebKit's GPU compositing removed it but made the page slow; turning off only the DMABUF renderer removed most of it and kept the GPU composition.

1. `fyr-desktop` sets `WEBKIT_DISABLE_DMABUF_RENDERER=1` before the window starts, unless the variable is already set (an owner's own value, including `0`, wins).
