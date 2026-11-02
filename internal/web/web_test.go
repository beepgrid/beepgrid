//go:build !headless

package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFaviconDeclarationsAndImageResponses(t *testing.T) {
	h := Handler()
	for _, route := range []string{"/", "/conversations/example"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", route, nil))
		for _, name := range []string{"icon.svg", "favicon.ico", "favicon-96x96.png", "apple-touch-icon.png"} {
			if w.Code != 200 || !strings.Contains(w.Body.String(), "/"+name+"?v=2") {
				t.Fatalf("missing favicon declaration on %s: %s", route, name)
			}
		}
	}
	for name, signature := range map[string][]byte{
		"icon.svg":             []byte("<svg"),
		"favicon.ico":          {0, 0, 1, 0},
		"favicon-96x96.png":    {137, 80, 78, 71, 13, 10, 26, 10},
		"apple-touch-icon.png": {137, 80, 78, 71, 13, 10, 26, 10},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/"+name+"?v=2", nil))
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/") || !bytes.HasPrefix(w.Body.Bytes(), signature) {
			t.Fatalf("favicon is not a usable image: %s %d %s", name, w.Code, w.Header().Get("Content-Type"))
		}
	}
}

func TestEmbeddedTypographyFontsAndLicenses(t *testing.T) {
	root, _ := fs.Sub(assets, "dist")
	raw, err := fs.ReadFile(root, "fonts/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var fonts []struct {
		Name, File, SHA256, License string
		Bytes                       int
	}
	if err := json.Unmarshal(raw, &fonts); err != nil || len(fonts) != 7 {
		t.Fatalf("font manifest: %v (%d fonts)", err, len(fonts))
	}
	for _, font := range fonts {
		t.Run(font.Name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/fonts/"+font.File, nil))
			body := rec.Body.Bytes()
			hash := sha256.Sum256(body)
			if rec.Code != 200 || rec.Header().Get("Content-Type") != "font/woff2" || len(body) != font.Bytes || !bytes.HasPrefix(body, []byte("wOF2")) || hex.EncodeToString(hash[:]) != font.SHA256 {
				t.Fatalf("font integrity or delivery: %d %s %d bytes", rec.Code, rec.Header().Get("Content-Type"), len(body))
			}
			license, err := fs.ReadFile(root, "fonts/"+font.Name+"-OFL.txt")
			if err != nil || font.License != "OFL-1.1" || !bytes.Contains(license, []byte("SIL OPEN FONT LICENSE")) {
				t.Fatalf("missing font license: %v", err)
			}
		})
	}
}

func TestEmbeddedAgentIconSpriteAndLicenses(t *testing.T) {
	root, _ := fs.Sub(assets, "dist")
	raw, err := fs.ReadFile(root, "agent-icons/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		SpriteSHA256 string `json:"sprite_sha256"`
		Icons        []struct {
			Pack  string
			Index int
		}
	}
	if err := json.Unmarshal(raw, &manifest); err != nil || len(manifest.Icons) != 24 {
		t.Fatalf("icon manifest: %v, %d icons", err, len(manifest.Icons))
	}
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/agent-icons/marks.svg", nil))
	body := rec.Body.Bytes()
	hash := sha256.Sum256(body)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "image/svg+xml") || hex.EncodeToString(hash[:]) != manifest.SpriteSHA256 {
		t.Fatal("missing or changed SVG sprite", rec.Code)
	}
	for _, pack := range []string{"lucide", "phosphor"} {
		license, err := fs.ReadFile(root, "agent-icons/"+pack+"-LICENSE.txt")
		if err != nil || !bytes.Contains(license, []byte("Permission")) {
			t.Fatalf("missing icon license for %s: %v", pack, err)
		}
	}
}

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

func TestIndependentWorkspaceEntrypointAndPrivateBuildFiles(t *testing.T) {
	h := Handler()
	for _, route := range []string{"/agents", "/inbox", "/conversations/con_test", "/settings/managed"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", route, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `src="/theme.js"`) || !strings.Contains(rec.Body.String(), `href="/icon.svg?v=2"`) {
			t.Fatalf("workspace deep link %s: %d", route, rec.Code)
		}
	}
	for _, route := range []string{"/.vite/manifest.json", "/assets/index.js.map"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", route, nil))
		if rec.Code != 404 {
			t.Fatalf("private build file %s: %d", route, rec.Code)
		}
	}
	root, _ := fs.Sub(assets, "dist")
	if e := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if !d.IsDir() && strings.HasSuffix(p, ".map") {
			t.Fatal("source map embedded", p)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
}
