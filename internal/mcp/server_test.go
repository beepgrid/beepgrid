package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/notif-dev/notif/sdk/go/notif"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStdioFramingAndScopedTools(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer scoped-worker-token" {
			t.Error("missing scoped credential")
		}
		if r.URL.Path != "/api/v1/agents" {
			t.Error("wrong endpoint")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":"agent-1","name":"Scout"}]`))
	}))
	defer backend.Close()
	input := strings.Join([]string{`{"jsonrpc":"2.0","id":1,"method":"initialize"}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_agents","arguments":{}}}`, `{"jsonrpc":"2.0","id":4,"method":"unknown"}`}, "\n")
	var out bytes.Buffer
	if e := Serve(context.Background(), notif.New(backend.URL, "scoped-worker-token"), strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatal(out.String())
	}
	for _, line := range lines {
		var v map[string]any
		if e := json.Unmarshal([]byte(line), &v); e != nil {
			t.Fatal("non-JSON stdout", line)
		}
	}
	if !strings.Contains(lines[2], "agent-1") || !strings.Contains(lines[3], "-32601") {
		t.Fatal(out.String())
	}
}
