package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestEmbeddedAssetsCompressionConditionalAndRange(t *testing.T) {
	h := Handler()
	root, _ := fs.Sub(assets, "dist")
	var identityBytes, gzipBytes int
	err := fs.WalkDir(root, ".", func(path string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() || strings.HasSuffix(path, ".gz") {
			return e
		}
		raw, e := fs.ReadFile(root, path)
		if e != nil {
			return e
		}
		urlPath := "/" + path
		if path == "index.html" {
			urlPath = "/"
		}
		urlPath = (&url.URL{Path: urlPath}).EscapedPath()
		request := httptest.NewRequest("GET", urlPath, nil)
		request.Header.Set("Accept-Encoding", "gzip")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, request)
		if rec.Code != 200 {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		body := rec.Body.Bytes()
		identityBytes += len(raw)
		gzipBytes += len(body)
		if rec.Header().Get("Content-Encoding") == "gzip" {
			z, e := gzip.NewReader(bytes.NewReader(body))
			if e != nil {
				t.Fatal(e)
			}
			body, e = io.ReadAll(z)
			if e != nil {
				t.Fatal(e)
			}
			z.Close()
		}
		if !bytes.Equal(body, raw) {
			t.Fatal("asset changed", path)
		}
		etag := rec.Header().Get("ETag")
		if etag == "" || rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Fatal("missing representation metadata")
		}
		request.Header.Set("If-None-Match", etag)
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, request)
		if rec.Code != 304 || rec.Body.Len() != 0 {
			t.Fatal("conditional request failed", rec.Code)
		}
		request = httptest.NewRequest("HEAD", urlPath, nil)
		request.Header.Set("Accept-Encoding", "gzip")
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, request)
		if rec.Code != 200 || rec.Body.Len() != 0 {
			t.Fatal("HEAD body", path)
		}
		if len(raw) >= 10 {
			request = httptest.NewRequest("GET", urlPath, nil)
			request.Header.Set("Accept-Encoding", "gzip")
			request.Header.Set("Range", "bytes=0-9")
			rec = httptest.NewRecorder()
			h.ServeHTTP(rec, request)
			if rec.Code != 206 || rec.Header().Get("Content-Encoding") != "" || !bytes.Equal(rec.Body.Bytes(), raw[:10]) {
				t.Fatal("range changed", path, rec.Code)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("embedded assets: identity=%d gzip=%d", identityBytes, gzipBytes)
	for _, header := range []string{"gzip;q=0", "gzip;q=bad", "gzip;q=2", "br"} {
		r := httptest.NewRequest("GET", "/agents", nil)
		r.Header.Set("Accept-Encoding", header)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 || rec.Header().Get("Content-Encoding") != "" {
			t.Fatal("negotiation/deep link", header)
		}
	}
}
