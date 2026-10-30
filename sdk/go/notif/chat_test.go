package notif

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStartChatPreservesCodePointMentionsAndDecisionFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/conversations" || r.Header.Get("Idempotency-Key") != "shared-1" {
			t.Error("wrong StartChat contract")
		}
		var in StartChatInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if len(in.Mentions) != 1 || in.Mentions[0].Start != 2 || in.Mentions[0].End != 13 || in.Body != "👋 @Reviewer 2 please review" || !in.Approval || len(in.Choices) != 2 || len(in.AttachmentIDs) != 1 || in.AttachmentIDs[0] != "draft" {
			t.Error(in)
		}
		w.Write([]byte(`{"conversation_id":"shared_1","messages":[]}`))
	}))
	defer server.Close()
	input := StartChatInput{Targets: []Target{{Kind: "agent", ID: "a"}}, Body: "👋 @Reviewer 2 please review", Approval: true, Choices: []string{"Yes", "No"}, Mentions: []Mention{{PrincipalID: "a", Start: 2, End: 13}}, AttachmentIDs: []string{"draft"}}
	if out, err := New(server.URL, "fixture").StartChat(context.Background(), input, "shared-1"); err != nil || out.ConversationID != "shared_1" {
		t.Fatal(out, err)
	}
}

func TestChatMembershipKeyHistoryDefaultAndDraftUpload(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing authentication")
		}
		if r.URL.Path == "/api/v1/attachments" {
			if err := r.ParseMultipartForm(65536); err != nil {
				t.Error(err)
				return
			}
			defer r.MultipartForm.RemoveAll()
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			defer file.Close()
			raw, _ := io.ReadAll(file)
			if string(raw) != "private draft" || header.Filename != "brief.txt" {
				t.Error("incorrect staged upload")
			}
			io.WriteString(w, `{"id":"att_draft","conversation_id":""}`)
			return
		}
		if r.URL.EscapedPath() != "/api/v1/conversations/a%2Fb/membership" || r.Header.Get("Idempotency-Key") != "membership-1" {
			t.Error("wrong membership route/key")
		}
		var in map[string]any
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["operation"] != "add" || in["version"] != float64(3) || in["share_history"] != nil {
			t.Error("history opt-in changed", in)
		}
		io.WriteString(w, `{"conversation_id":"a/b","version":4}`)
	}))
	defer server.Close()
	client := New(server.URL, "fixture")
	file, err := client.StageAttachment(context.Background(), "brief.txt", "text/plain", []byte("private draft"))
	if err != nil || file.ID != "att_draft" || file.ConversationID != "" {
		t.Fatal(file, err)
	}
	changed, err := client.ChangeChatMembership(context.Background(), "a/b", ChatMembershipInput{Operation: "add", Version: 3, Members: []string{"agent_2"}}, "membership-1")
	if err != nil || changed.Version != 4 || calls != 2 {
		t.Fatal(changed, calls, err)
	}
}
