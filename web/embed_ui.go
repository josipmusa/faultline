//go:build ui

package web

import (
	"embed"
	"io/fs"
)

// The all: prefix matters: Next names its asset directory _next, and embed
// skips paths beginning with an underscore without it.
//
//go:embed all:out
var export embed.FS

// Available says the UI was built into this binary.
const Available = true

// FS is the static export's root, holding index.html, 404.html and _next.
func FS() fs.FS {
	sub, err := fs.Sub(export, "out")
	if err != nil {
		// Unreachable: the embed above fails to compile without out/.
		panic("web: embedded UI has no out directory: " + err.Error())
	}
	return sub
}
