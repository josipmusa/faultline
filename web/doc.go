// Package web carries the built UI. The static export is embedded only when
// the ui build tag is set, so a checkout with no Node toolchain still builds a
// working binary that says the UI is missing rather than failing to compile.
package web
