package front

import "testing"

// Decision 0038: Settings → Devices names the Linux app's window, not the
// WebKit it runs on.
func TestBrowserNameKnowsTheDesktopApp(t *testing.T) {
	ua := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15 FyrDesktop/linux"
	if got := BrowserName(ua); got != "The Fyr app on Linux" {
		t.Fatalf("BrowserName = %q", got)
	}
	if got := BrowserName("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"); got != "Safari on Linux" {
		t.Fatalf("BrowserName = %q", got)
	}
}
