package admin

import (
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/josipmusa/faultline/web"
)

// notBuilt is what / says when the binary carries no UI. A response-tier fault
// explains why it could not apply rather than silently doing nothing, and a
// missing UI is the same kind of absence: naming the command beats a blank 404.
const notBuilt = `faultline: the web UI is not built into this binary.

Build it with "make ui" and rebuild, or use the HTTP API at /api.
`

// ui serves a Next.js static export: the index at the root, its assets by
// path, and the export's own 404 document for anything else. A nil fsys is a
// binary built without the ui tag.
type ui struct {
	fsys fs.FS
}

func newUIHandler(fsys fs.FS) http.Handler {
	return &ui{fsys: fsys}
}

// uiHandler serves the UI embedded at build time, or explains its absence.
func uiHandler() http.Handler {
	if !web.Available {
		return newUIHandler(nil)
	}
	return newUIHandler(web.FS())
}

func (u *ui) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if u.fsys == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, notBuilt)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if !u.isFile(name) {
		u.notFound(w)
		return
	}
	http.ServeFileFS(w, r, u.fsys, name)
}

// isFile says the path names a regular file in the export. A directory is not
// one: serving a listing would expose the export's layout, so it reads as a
// miss like any other path that is not a page.
func (u *ui) isFile(name string) bool {
	info, err := fs.Stat(u.fsys, name)
	return err == nil && info.Mode().IsRegular()
}

// notFound answers with the export's own 404 document, so a mistyped URL reads
// as a page rather than as a Go error.
func (u *ui) notFound(w http.ResponseWriter) {
	body, err := fs.ReadFile(u.fsys, "404.html")
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write(body)
}
