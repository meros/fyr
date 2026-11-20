package front

import "testing"

// Decision 0037 & 0038: Settings → Devices names the desktop apps' windows,
// not the WebKit they run on.
func TestBrowserNameKnowsTheDesktopApp(t *testing.T) {
	ua := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15 FyrDesktop/linux"
	if got := BrowserName(ua); got != "The Fyr app on Linux" {
		t.Fatalf("BrowserName(linux) = %q", got)
	}
	mac := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) FyrDesktop/mac"
	if got := BrowserName(mac); got != "The Fyr app on macOS" {
		t.Fatalf("BrowserName(mac) = %q", got)
	}
	if got := BrowserName("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"); got != "Safari on Linux" {
		t.Fatalf("BrowserName(safari) = %q", got)
	}
}
