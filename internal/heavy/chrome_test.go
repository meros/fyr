package heavy

import "testing"

// The watchdog thread's 14:04 findings: the two jobs behind that thrash matched no pattern, so the gate let them run
// without a slot: the layout harness (node over many headless Chrome tabs) and a repo's pre-push check. Anything
// that starts headless Chrome counts too.
func TestHeavyChromeHarnessAndPrePush(t *testing.T) {
	m := Default()
	for cmd, want := range map[string]string{
		"cd web && node test/layout.e2e.mjs":                                    "node layout.e2e.mjs",
		"FYR_SHOTS=/tmp/s node test/layout.e2e.mjs 2>&1 | tail -3":              "node layout.e2e.mjs",
		"cd web && node test/timing.e2e.mjs":                                    "node timing.e2e.mjs",
		"cd ~/src/shop && yarn pre-push:check":                                  "yarn pre-push:check",
		"yarn run pre-push":                                                     "yarn pre-push",
		"google-chrome --headless=new --remote-debugging-port=9222 about:blank": "headless chrome",
		"chromium --headless --disable-gpu --screenshot https://x":              "headless chrome",
		"node test/selection.test.ts":                                           "",
		"node --test test/load.test.ts":                                         "",
		"grep -n layout.e2e.mjs Makefile":                                       "",
	} {
		if got := m.Class(cmd); got != want {
			t.Errorf("%q: got %q, want %q", cmd, got, want)
		}
	}
	for class, kind := range map[string]string{"node layout.e2e.mjs": "e2e", "headless chrome": "e2e", "yarn pre-push:check": "test"} {
		if got := KindOf(class); got != kind {
			t.Errorf("kind of %q: got %q, want %q", class, got, kind)
		}
	}
}
