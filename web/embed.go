// Package web embeds the built client (web/dist) into the fyrd binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist is the built client. It holds only .keep until `npm run build` ran.
func Dist() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}
