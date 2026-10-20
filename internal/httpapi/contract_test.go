package httpapi

import (
	"encoding/json"
	"github.com/notif-dev/notif/api"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestPublicContractCoversRegisteredLiteralRoutes(t *testing.T) {
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
	pattern := regexp.MustCompile(`HandleFunc\("(GET|POST) ([^"+]+)"\s*,`)
	for _, name := range files {
		if filepath.Ext(name) != ".go" {
			continue
		}
		raw, _ := os.ReadFile(name)
		for _, m := range pattern.FindAllStringSubmatch(string(raw), -1) {
			method := "get"
			if m[1] == "POST" {
				method = "post"
			}
			if _, ok := spec.Paths[m[2]][method]; !ok {
				t.Errorf("route missing from OpenAPI: %s %s", m[1], m[2])
			}
		}
	}
}
