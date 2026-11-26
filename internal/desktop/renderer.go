package desktop

// V0.desktop-gpu-shm: how WebKitGTK draws the Linux app's page.
//
// V0.desktop-no-dmabuf turned the DMABUF renderer off to stop a flicker while
// the owner typed (Wayland, Intel graphics). In WebKitGTK 2.54 that renderer is
// the only accelerated one, so with it off the web process never opens the GPU
// and paints every frame on the CPU: about 90 ms of CPU a keystroke with many
// threads, and slower still when agents keep the cores busy. A browser tab of
// the same page stays on the GPU, which is why only the app was slow.
//
// FORCE_SHM keeps the DMABUF renderer, so the page is still drawn on the GPU,
// and hands each finished frame to the window through shared memory instead
// of a GPU buffer. The window then never shows a buffer the GPU is still
// drawing, which is what flickered.

// rendererVars are the variables this file decides on. If the owner sets any
// of them, the app sets none: an owner's own choice, including `0`, wins.
var rendererVars = []string{
	"WEBKIT_DISABLE_DMABUF_RENDERER",
	"WEBKIT_DMABUF_RENDERER_FORCE_SHM",
	"WEBKIT_DMABUF_RENDERER_DISABLE_GBM",
	"WEBKIT_DISABLE_COMPOSITING_MODE",
}

// RendererEnv is what the app adds to its environment before WebKit starts.
// lookup is os.LookupEnv.
func RendererEnv(lookup func(string) (string, bool)) map[string]string {
	for _, k := range rendererVars {
		if _, set := lookup(k); set {
			return nil
		}
	}
	return map[string]string{"WEBKIT_DMABUF_RENDERER_FORCE_SHM": "1"}
}
