//go:build !(linux && cgo)

// fyr-desktop is Fyr's Linux app (decision 0038). It needs GTK, WebKitGTK
// and cgo: `nix build .#fyr-desktop`, or `CGO_ENABLED=1 go build` with the
// libraries' development files.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "fyr-desktop: this build has no GTK (it needs Linux and cgo); `fyr open -app` opens Fyr in a browser window")
	os.Exit(1)
}
