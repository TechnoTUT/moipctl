// Package ui serves the web UI built from frontend/ (Vue and Vite, static output), which is embedded in the binary.
//
// `make frontend` builds the UI and copies it to internal/ui/dist. Until that has been done the binary has no UI:
// the API works and "/" says how to build it, so `go build` and `go test` need no Node.js.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:dist
var embedded embed.FS

// Content-Security-Policy of the UI. The built page has no inline script or style (the theme is applied by an
// external script), so nothing but this origin may run or style anything: markup injected through a name that
// came from the network could not execute even if the UI ever let it through. The UI does not do that either: it
// never writes network-supplied text as HTML.
const csp = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// Handler serves the UI embedded in the binary.
func Handler() http.Handler {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err) // dist exists: it is embedded
	}
	return New(sub)
}

// New serves the UI found in fsys.
func New(fsys fs.FS) http.Handler {
	_, err := fs.Stat(fsys, "index.html")
	built := err == nil
	files := http.FileServerFS(fsys)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")

		if !built {
			if r.URL.Path == "/" {
				h.Set("Content-Type", "text/plain; charset=utf-8")
				w.Write([]byte("moipctl: this binary has no web UI (build it with `make frontend`, then rebuild). The API is under /api/.\n"))
				return
			}
			http.NotFound(w, r)
			return
		}

		switch {
		case r.URL.Path == "/":
			// the page itself must never be cached: it names the hashed assets of this build
			h.Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			// file names carry a content hash
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
		default:
			h.Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
		}
	})
}
