package webapp

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

func distHandler(dist fs.FS) http.Handler {
	files := http.FileServer(http.FS(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		requested := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if requested != "" && requested != "." {
			if info, statErr := fs.Stat(dist, requested); statErr == nil && info.Mode().IsRegular() {
				if strings.HasPrefix(requested, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				clone := r.Clone(r.Context())
				clone.URL.Path = "/" + requested
				files.ServeHTTP(w, clone)
				return
			}
			if strings.HasPrefix(requested, "assets/") {
				w.Header().Set("Cache-Control", "no-store")
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}
		}
		index, readErr := fs.ReadFile(dist, "index.html")
		if readErr != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", mime.TypeByExtension(".html"))
		http.ServeContent(w, r, "index.html", time.Time{}, strings.NewReader(string(index)))
	})
}
