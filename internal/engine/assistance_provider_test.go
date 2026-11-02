package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssistanceProviderProtocolsAndBounds(t *testing.T) {
	s, _ := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer provider-only-secret" {
			t.Error("missing separate provider authentication")
		}
		if r.URL.Path == "/oversized" {
			w.Write([]byte(strings.Repeat("x", 32769)))
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/stolen", 302)
			return
		}
		if r.URL.Path == "/slow" {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(time.Second):
				return
			}
		}
		var in map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		raw := string(JSON(in))
		if strings.Contains(raw, "PRIVATE ANSWER") || strings.Contains(raw, "engine-secret") {
			t.Error("unneeded private data reached provider")
		}
		var task string
		_ = json.Unmarshal(in["task"], &task)
		w.Header().Set("Content-Type", "application/json")
		switch task {
		case "overseer_plan":
			w.Write([]byte(`{"operation":"status","agent_ids":["agt_1"],"summary":"Only the authorized agent is visible"}`))
		case "overseer_summary":
			w.Write([]byte(`{"summary":"One authorized agent reports completion"}`))
		default:
			t.Error("unknown task", task)
			http.Error(w, "unsupported", 400)
		}
	}))
	defer server.Close()
	if err := s.ConfigureOverseer(ctx, server.URL, "provider-only-secret"); err != nil {
		t.Fatal(err)
	}
	largeContext := context.WithValue(ctx, assistanceActorKey{}, "large")
	if _, err := s.OverseerPlanner(largeContext, strings.Repeat("x", 32769), nil); err == nil {
		t.Fatal("unbounded payload")
	}
	plan, err := s.OverseerPlanner(context.WithValue(ctx, assistanceActorKey{}, "scope/person1"), "Status?", []FleetStatus{{Agent: Principal{ID: "agt_1"}}})
	if err != nil || plan.Operation != "status" {
		t.Fatal(plan, err)
	}
	summary, err := s.OverseerSummarizer(context.WithValue(ctx, assistanceActorKey{}, "scope/person2"), "Summarize", []OverseerReply{{AgentID: "agt_1", State: "resolved", Answer: "done"}})
	if err != nil || summary == "" {
		t.Fatal(summary, err)
	}
	before := calls.Load()
	if err = s.ConfigureOverseer(ctx, server.URL+"/redirect", "provider-only-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OverseerPlanner(ctx, "Status?", nil); err == nil || calls.Load() != before+1 {
		t.Fatal("provider redirect followed", err)
	}
	if err = s.ConfigureOverseer(ctx, server.URL+"/oversized", "provider-only-secret"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.OverseerPlanner(ctx, "Status?", nil); err == nil {
		t.Fatal("oversized response accepted")
	}
	if err = s.ConfigureOverseer(ctx, server.URL+"/slow", "provider-only-secret"); err != nil {
		t.Fatal(err)
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err = s.OverseerPlanner(bounded, "Status?", nil); err == nil {
		t.Fatal("ignored cancellation")
	}
	s.AllowLocal = false
	if err = s.ConfigureOverseer(ctx, server.URL, ""); err == nil {
		t.Fatal("private provider address accepted in production defaults")
	}
}
