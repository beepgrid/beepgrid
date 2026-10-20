package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed dist/*
var assets embed.FS

func Handler() http.Handler {
	root, _ := fs.Sub(assets, "dist")
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "." || p == "" {
			p = "index.html"
		}
		if _, e := fs.Stat(root, p); e != nil {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(path.Base(p), ".") {
				http.NotFound(w, r)
				return
			}
			copy := r.Clone(r.Context())
			copy.URL.Path = "/"
			files.ServeHTTP(w, copy)
			return
		}
		files.ServeHTTP(w, r)
	})
}
