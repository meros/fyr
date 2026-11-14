package agent

import "testing"

// Decision 0007: agents in Fyr see the user's profile, like a login shell.
func TestUserPath(t *testing.T) {
	exists := func(p string) bool { return p != "/home/u/.nix-profile/bin" }
	got := UserPath("/nix/store/x/bin:/run/current-system/sw/bin", "/home/u", "u", exists)
	want := "/nix/store/x/bin:/run/current-system/sw/bin:/run/wrappers/bin:/etc/profiles/per-user/u/bin"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
