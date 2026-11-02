package httpapi

import (
	"encoding/json"
	"github.com/beepgrid/beepgrid/api"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPublicContractMatchesRegisteredRoutes(t *testing.T) {
	var spec struct {
		OpenAPI string                                `json:"openapi"`
		Paths   map[string]map[string]json.RawMessage `json:"paths"`
	}
	if e := json.Unmarshal(api.OpenAPI, &spec); e != nil {
		t.Fatal(e)
	}
	if spec.OpenAPI != "3.1.0" {
		t.Fatal(spec.OpenAPI)
	}
	files, e := filepath.Glob("*.go")
	if e != nil {
		t.Fatal(e)
	}
	pattern := regexp.MustCompile(`HandleFunc\("(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS) ([^"+]+)"\s*,`)
	for _, name := range files {
		if filepath.Ext(name) != ".go" {
			continue
		}
		raw, _ := os.ReadFile(name)
		for _, m := range pattern.FindAllStringSubmatch(string(raw), -1) {
			method := strings.ToLower(m[1])
			if _, ok := spec.Paths[m[2]][method]; !ok {
				t.Errorf("route missing from OpenAPI: %s %s", m[1], m[2])
			}
		}
	}
	server := New(nil, "http://localhost", nil)
	placeholders := regexp.MustCompile(`\{[^}]+\}`)
	for path, operations := range spec.Paths {
		for method := range operations {
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options":
			default:
				continue
			}
			request := httptest.NewRequest(strings.ToUpper(method), "http://localhost"+placeholders.ReplaceAllString(path, "contract-item"), nil)
			_, pattern := server.mux.Handler(request)
			if pattern != strings.ToUpper(method)+" "+path {
				t.Errorf("documented operation does not match runtime registration: %s %s -> %q", method, path, pattern)
			}
		}
	}
	// These loops register paths assembled at runtime; keep them covered explicitly.
	for _, route := range []string{"GET /api/v1/people", "GET /api/v1/agents", "GET /api/v1/services", "POST /api/v1/requests/{id}/cancel", "POST /api/v1/requests/{id}/supersede"} {
		method, path, _ := strings.Cut(route, " ")
		if _, ok := spec.Paths[path][strings.ToLower(method)]; !ok {
			t.Errorf("dynamic route missing: %s", route)
		}
	}
}
