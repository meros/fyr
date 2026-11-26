# V0.desktop-gpu-shm: the Linux app draws its page on the GPU again, and hands each frame to the window through shared memory

→ amends V0.desktop-no-dmabuf (point 1).

Why: V0.desktop-no-dmabuf turned off WebKitGTK's DMABUF renderer to stop a flicker while typing, assuming the page would still compose on the GPU. In WebKitGTK 2.54 that renderer is the only accelerated one: with it off, the web process never opens the GPU (no Mesa driver loaded, no render node open) and paints every frame on the CPU. The app became slow to type in, and much slower when agents keep the cores busy, while a browser tab of the same page stayed fast.

Measured on a host with many threads: WebKitGTK 2.54 MiniBrowser in a headless sway, 124 keystrokes into a thread's composer.

| renderer | web process CPU | key to frame, median | frame time, 95th percentile |
|---|---|---|---|
| DMABUF off (V0.desktop-no-dmabuf) | 11.4 s | 35 ms | 53 ms |
| DMABUF, frames over shared memory (this entry) | 1.0 s | 24 ms | 19 ms |
| DMABUF, frames as GPU buffers | 0.8 s | 24 ms | 19 ms |

With every core busy in the agents' slice and the browser in the user's slice, the median key to frame went from 100 ms with DMABUF off to 56 to 80 ms on the GPU, and the web process used about a third of the CPU.

1. `fyr-desktop` sets `WEBKIT_DMABUF_RENDERER_FORCE_SHM=1` before the window starts, and no longer sets `WEBKIT_DISABLE_DMABUF_RENDERER`. The page is drawn on the GPU; the finished frame is copied to shared memory for the window, so the window never shows a GPU buffer that is still being drawn, which is what flickered.
2. If the owner sets any of `WEBKIT_DISABLE_DMABUF_RENDERER`, `WEBKIT_DMABUF_RENDERER_FORCE_SHM`, `WEBKIT_DMABUF_RENDERER_DISABLE_GBM` or `WEBKIT_DISABLE_COMPOSITING_MODE`, the app sets none of them (`internal/desktop/renderer.go`).
