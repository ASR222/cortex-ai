package gateway

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	"cortexai/internal/platform/httpx"
)

// spaHandler serves a built single-page app from dir.
//
// Files that exist are served directly; Vite's content-hashed files under
// /assets are cached for a year. Any other path returns index.html so the
// client-side app can handle it. Directory listings are never shown.
func spaHandler(dir string) http.Handler {
	fsys := os.DirFS(dir)
	files := http.FileServerFS(fsys)

	serveIndex := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, fsys, "index.html")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		info, err := fs.Stat(fsys, name)
		if name == "" || name == "index.html" || err != nil || info.IsDir() {
			serveIndex(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		files.ServeHTTP(w, r)
	})
}
