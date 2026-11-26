package desktop

import (
	"reflect"
	"testing"
)

// V0.desktop-gpu-shm: the app keeps WebKit's GPU renderer and hands frames over
// shared memory; it never turns the DMABUF renderer off itself (that paints on
// the CPU), and an owner's own renderer variable wins.
func TestRendererKeepsTheGPU(t *testing.T) {
	none := func(string) (string, bool) { return "", false }
	got := RendererEnv(none)
	if want := map[string]string{"WEBKIT_DMABUF_RENDERER_FORCE_SHM": "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("RendererEnv = %v, want %v", got, want)
	}
	if _, off := got["WEBKIT_DISABLE_DMABUF_RENDERER"]; off {
		t.Fatal("the app turns the DMABUF renderer off: WebKit then paints on the CPU")
	}
	for _, k := range rendererVars {
		own := func(n string) (string, bool) { return "0", n == k }
		if got := RendererEnv(own); got != nil {
			t.Errorf("with the owner's %s set, RendererEnv = %v, want nothing", k, got)
		}
	}
}
