package notif

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLargeLegalConversationResponseAndExplicitBound(t *testing.T) {
	body := strings.Repeat("large history ", 400000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"messages": []Message{{ID: "large", Body: body}}})
	}))
	defer server.Close()
	c := New(server.URL, "")
	var result struct {
		Messages []Message `json:"messages"`
	}
	if e := c.Do(context.Background(), "GET", "/api/v1/conversations/history", nil, &result, ""); e != nil || len(result.Messages) != 1 || result.Messages[0].Body != body {
		t.Fatal("legal history truncated", e)
	}
	c.MaxResponseBytes = 1024
	if e := c.Do(context.Background(), "GET", "/api/v1/conversations/history", nil, &result, ""); e == nil || !strings.Contains(e.Error(), "exceeds 1024") {
		t.Fatal("response bound ignored", e)
	}
}
