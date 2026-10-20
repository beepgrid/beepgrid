package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
)

//go:embed dist/*
var assets embed.FS

func Handler() http.Handler {
	root, _ := fs.Sub(assets, "dist")
	files := http.FileServer(http.FS(root))
	type asset struct {
		gzip []byte
		etag string
	}
	compressed := map[string]asset{}
	// The UI build supplies compressed assets. Serving needs no compressor
	// buffers or runtime compression CPU, including at cold start.
	_ = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() || strings.HasSuffix(p, ".gz") {
			return e
		}
		raw, e := fs.ReadFile(root, p)
		if e != nil {
			return e
		}
		a := asset{etag: fmt.Sprintf(`"%x"`, sha256.Sum256(raw))}
		if encoded, e := fs.ReadFile(root, p+".gz"); e == nil {
			a.gzip = encoded
		}
		compressed[p] = a
		return nil
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".gz") {
			http.NotFound(w, r)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/index.html") {
			files.ServeHTTP(w, r)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "." || p == "" {
			p = "index.html"
		}
		if _, e := fs.Stat(root, p); e != nil {
			if strings.HasPrefix(r.URL.Path, "/api/") || strings.Contains(path.Base(p), ".") {
				http.NotFound(w, r)
				return
			}
			p = "index.html"
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		if a, ok := compressed[p]; ok {
			w.Header().Set("ETag", a.etag)
			w.Header().Add("Vary", "Accept-Encoding")
			if len(a.gzip) > 0 && r.Header.Get("Range") == "" && acceptsGzip(r.Header.Get("Accept-Encoding")) {
				w.Header().Set("ETag", strings.TrimSuffix(a.etag, `"`)+`-gzip"`)
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(p)))
				http.ServeContent(w, r, path.Base(p), time.Time{}, bytes.NewReader(a.gzip))
				return
			}
		}
		files.ServeHTTP(w, r)
	})
}

func acceptsGzip(header string) bool {
	for _, item := range strings.Split(header, ",") {
		parts := strings.Split(strings.TrimSpace(item), ";")
		if !strings.EqualFold(parts[0], "gzip") {
			continue
		}
		for _, p := range parts[1:] {
			k, v, _ := strings.Cut(strings.TrimSpace(p), "=")
			if k == "q" {
				q, e := strconv.ParseFloat(v, 64)
				if e != nil || q <= 0 || q > 1 {
					return false
				}
			}
		}
		return true
	}
	return false
}
