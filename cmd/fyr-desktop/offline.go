//go:build linux && cgo

package main

import (
	"html"
	"net/url"
)

// offlinePage is what the window shows while fyrd does not answer; it loads
// Fyr again by itself when fyrd's feed comes back (app.feedUp).
func offlinePage(why string) string {
	return "data:text/html;charset=utf-8," + url.PathEscape(`<!doctype html><meta charset="utf-8"><title>Fyr</title>
<style>body{font:15px system-ui,sans-serif;margin:0;display:grid;place-items:center;height:100vh;color:#222;background:#fafafa}
@media (prefers-color-scheme:dark){body{color:#ddd;background:#161616}}div{max-width:30em;padding:16px}p{color:#888}</style>
<div><h2>Fyr is not answering</h2><p>`+html.EscapeString(why)+`</p>
<p>This window opens Fyr as soon as it is back. Resume in the tray menu starts it.</p></div>`)
}
