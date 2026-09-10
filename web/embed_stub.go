//go:build !ui

package web

import "io/fs"

// Available says the UI was built into this binary.
const Available = false

// FS is nil in a binary built without the UI.
func FS() fs.FS { return nil }
