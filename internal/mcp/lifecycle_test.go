package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/beepgrid/beepgrid/sdk/go/notif"
)

func TestStagedFilesAndMembershipPreserveAuthorityAndExplicitHistory(t *testing.T) {
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer bounded-worker" {
			t.Error("credential changed")
		}
		if r.URL.Path == "/api/v1/attachments" {
			if err := r.ParseMultipartForm(65536); err != nil {
				t.Error(err)
			}
			defer r.MultipartForm.RemoveAll()
			fmt.Fprint(w, `{"id":"draft"}`)
			return
		}
		var input map[string]any
		_ = json.NewDecoder(r.Body).Decode(&input)
		if r.URL.Path == "/api/v1/conversations" {
			if r.Header.Get("Idempotency-Key") != "start-1" || input["attachment_ids"].([]any)[0] != "draft" {
				t.Error(input)
			}
			fmt.Fprint(w, `{"conversation_id":"shared"}`)
			return
		}
		if r.URL.Path != "/api/v1/conversations/shared/membership" || r.Header.Get("Idempotency-Key") != "members-1" || input["share_history"] != nil || input["operation"] != "add" {
			t.Error(input)
		}
		fmt.Fprint(w, `{"conversation_id":"shared","version":2}`)
	}))
	defer backend.Close()
	client := notif.New(backend.URL, "bounded-worker")
	for _, step := range []struct{ name, input string }{
		{"stage_attachment", `{"file_name":"brief.txt","media_type":"text/plain","base64":"eA=="}`},
		{"start_chat", `{"targets":[{"kind":"agent","id":"peer"}],"body":"Review","attachment_ids":["draft"],"idempotency_key":"start-1"}`},
		{"change_chat_membership", `{"conversation_id":"shared","operation":"add","version":1,"members":["new-member"],"idempotency_key":"members-1"}`},
	} {
		if _, err := call(context.Background(), client, step.name, json.RawMessage(step.input)); err != nil {
			t.Fatal(step.name, err)
		}
	}
	if _, err := call(context.Background(), client, "stage_attachment", json.RawMessage(`{"base64":"`+strings.Repeat("A", 87385)+`"}`)); err == nil || calls != 3 {
		t.Fatal("oversized stage reached backend", calls, err)
	}
}

func TestReplaceRequestPreservesObservedVersionAndStableKey(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]any
		_ = json.NewDecoder(r.Body).Decode(&input)
		if r.URL.Path != "/api/v1/requests/req_one/replace" || r.Header.Get("Idempotency-Key") != "replace-one" || input["version"] != float64(7) || input["body"] != "Restart after the demo?" {
			t.Error(r.URL.Path, input)
		}
		fmt.Fprint(w, `{"previous":{"id":"req_one","state":"superseded"},"replacement":{"id":"req_new","state":"open"}}`)
	}))
	defer backend.Close()
	client := notif.New(backend.URL, "bounded-worker")
	out, err := call(context.Background(), client, "replace_request", json.RawMessage(`{"id":"req_one","version":7,"body":"Restart after the demo?","idempotency_key":"replace-one"}`))
	if err != nil || out.(notif.ReplacedRequest).Replacement.ID != "req_new" {
		t.Fatal(out, err)
	}
}
