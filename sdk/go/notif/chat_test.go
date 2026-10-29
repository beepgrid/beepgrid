package notif

import (
	"context"
	"encoding/json"
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
		if len(in.Mentions) != 1 || in.Mentions[0].Start != 2 || in.Mentions[0].End != 13 || in.Body != "👋 @Reviewer 2 please review" || !in.Approval || len(in.Choices) != 2 {
			t.Error(in)
		}
		w.Write([]byte(`{"conversation_id":"shared_1","messages":[]}`))
	}))
	defer server.Close()
	input := StartChatInput{Targets: []Target{{Kind: "agent", ID: "a"}}, Body: "👋 @Reviewer 2 please review", Approval: true, Choices: []string{"Yes", "No"}, Mentions: []Mention{{PrincipalID: "a", Start: 2, End: 13}}}
	if out, err := New(server.URL, "fixture").StartChat(context.Background(), input, "shared-1"); err != nil || out.ConversationID != "shared_1" {
		t.Fatal(out, err)
	}
}
