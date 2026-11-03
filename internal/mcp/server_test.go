package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/beepgrid/beepgrid/sdk/go"
	"io"
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
	if e := Serve(context.Background(), beepgrid.New(backend.URL, "scoped-worker-token"), strings.NewReader(input), &out); e != nil {
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
	if !strings.Contains(lines[0], `"name":"beepgrid"`) {
		t.Fatal("MCP initialization returned the wrong product identity", lines[0])
	}
}

func TestStableToolRetryKeyAndInvalidRequest(t *testing.T) {
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Idempotency-Key") != "saved-checkpoint-7" {
			t.Error("lost caller retry key")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, exists := body["idempotency_key"]; exists {
			t.Error("MCP-only field leaked into strict REST body")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"conversation_id":"conversation-1","messages":[{"id":"message-1","body":"Hello"}]}`))
	}))
	defer backend.Close()
	input := `{ "jsonrpc":"1.0", "id":1,"method":"ping" }
{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"send","arguments":{"body":"Hello","target":{"kind":"agent","id":"a"},"idempotency_key":"saved-checkpoint-7"}}}`
	var out bytes.Buffer
	if e := Serve(context.Background(), beepgrid.New(backend.URL, "test"), strings.NewReader(input), &out); e != nil {
		t.Fatal(e)
	}
	if calls != 1 || !strings.Contains(out.String(), "-32600") || !strings.Contains(out.String(), "message-1") {
		t.Fatal(calls, out.String())
	}
}

func TestExecutionToolsPreserveScopeAndExactAction(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer worker" {
			t.Error("wrong identity")
		}
		var body map[string]any
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Error(e)
		}
		switch r.URL.Path {
		case "/api/v1/requests/request-1/redeem":
			if body["resource_version"] != "v1" || body["operation_key"] != "once" {
				t.Error("changed exact action", body)
			}
		case "/api/v1/executions/execution-1":
			if body["state"] != "succeeded" {
				t.Error(body)
			}
		default:
			t.Error(r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"execution-1","state":"authorized"}`))
	}))
	defer backend.Close()
	c := beepgrid.New(backend.URL, "worker")
	if _, e := call(context.Background(), c, "redeem_approval", json.RawMessage(`{"id":"request-1","action":{"name":"fixture","executor_id":"worker","resource":"fixture","resource_version":"v1","operation_key":"once","arguments":{}}}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := call(context.Background(), c, "report_execution", json.RawMessage(`{"id":"execution-1","state":"succeeded","result":{"reported":true}}`)); e != nil {
		t.Fatal(e)
	}
}

func TestFileToolsPreserveBytesAndRejectOversizedInputs(t *testing.T) {
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer scoped" {
			t.Error("missing credential")
		}
		if r.Method == "POST" {
			if err := r.ParseMultipartForm(65536); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			raw, _ := io.ReadAll(file)
			if string(raw) != "file bytes" {
				t.Error(string(raw))
			}
			fmt.Fprint(w, `{"id":"att_1"}`)
		} else {
			fmt.Fprint(w, "file bytes")
		}
	}))
	defer backend.Close()
	c := beepgrid.New(backend.URL, "scoped")
	if _, err := call(context.Background(), c, "upload_attachment", json.RawMessage(`{"conversation_id":"conv","file_name":"brief.pdf","media_type":"application/pdf","base64":"ZmlsZSBieXRlcw=="}`)); err != nil {
		t.Fatal(err)
	}
	result, err := call(context.Background(), c, "download_attachment", json.RawMessage(`{"id":"att_1"}`))
	if err != nil || result.(map[string]any)["base64"] != "ZmlsZSBieXRlcw==" {
		t.Fatal(result, err)
	}
	if _, err = call(context.Background(), c, "upload_attachment", json.RawMessage(`{"base64":"`+strings.Repeat("A", 87385)+`"}`)); err == nil || calls != 2 {
		t.Fatal("oversized upload reached API", calls, err)
	}
	if _, err = call(context.Background(), c, "list_directory", json.RawMessage(`{"kind":"principal"}`)); err == nil {
		t.Fatal("mixed identity accepted")
	}
}

func TestLiveInputOwnsInbox(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Live MCP must not poll or ACK the receiver's inbox")
	}))
	defer backend.Close()
	input := strings.Join([]string{`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"receive","arguments":{}}}`, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"acknowledge","arguments":{"id":"del_1","generation":1}}}`}, "\n")
	var out bytes.Buffer
	if err := ServeWithInput(context.Background(), beepgrid.New(backend.URL, "worker"), strings.NewReader(input), &out, true); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if strings.Contains(lines[0], `"name":"receive"`) || strings.Contains(lines[0], `"name":"acknowledge"`) || !strings.Contains(lines[0], `"name":"send"`) {
		t.Fatal(lines[0])
	}
	for _, line := range lines[1:] {
		if !strings.Contains(line, `"isError":true`) {
			t.Fatal(line)
		}
	}
}

func TestAnycastToolUsesStableKeyWithoutStatusModelLoop(t *testing.T) {
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/v1/anycast" || r.Header.Get("Idempotency-Key") != "same-task" {
			t.Error(r.URL.Path, r.Header)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["role_id"] != "grp_builder" || body["idempotency_key"] != nil {
			t.Error(body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"acast_one","state":"queued"}`))
	}))
	defer backend.Close()
	_, e := call(context.Background(), beepgrid.New(backend.URL, "worker"), "anycast", json.RawMessage(`{"role_id":"grp_builder","body":"Inspect","idempotency_key":"same-task"}`))
	if e != nil || calls != 1 {
		t.Fatal(calls, e)
	}
}
