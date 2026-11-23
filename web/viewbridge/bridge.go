// Package viewbridge holds the live view's bridge (SPEC 0.5 V0.37): the script and style the host puts at the top of
// every view page. bridge.html is its one source: the host embeds it here, and the client's mock (web/src/mock.ts)
// imports the same file, so the layout test drives the bridge the host serves (V0.single-sources).
package viewbridge

import (
	_ "embed"
	"strings"
)

//go:embed bridge.html
var bridge string

// HTML is the bridge: a <script> and a <style>, put first in a view page's <head>.
var HTML = strings.TrimSpace(bridge)
