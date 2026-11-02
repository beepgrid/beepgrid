package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beepgrid/beepgrid/internal/engine"
	"github.com/beepgrid/beepgrid/sdk/go"
)

func TestAssistanceSDKHTTPJourney(t *testing.T) {
	store, owner, _ := setup(t)
	ctx := context.Background()
	store.AllowLocal = true
	var me struct {
		Person beepgrid.Person `json:"person"`
	}
	if err := owner.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil {
		t.Fatal(err)
	}
	worker, session, err := owner.Enroll(ctx, beepgrid.EnrollInput{Agent: "assistance/scout", Name: "Scout"})
	if err != nil {
		t.Fatal(err)
	}
	var role struct {
		ID string `json:"id"`
	}
	if err = owner.Do(ctx, "POST", "/api/v1/roles", map[string]any{"name": "Worker replies", "rules": []map[string]string{{"action": "view", "target_kind": "person", "target_id": me.Person.ID}, {"action": "message", "target_kind": "person", "target_id": me.Person.ID}, {"action": "ask", "target_kind": "person", "target_id": me.Person.ID}}}, &role, ""); err != nil {
		t.Fatal(err)
	}
	if err = owner.Do(ctx, "POST", "/api/v1/role-bindings", map[string]string{"role_id": role.ID, "subject_kind": "principal", "subject_id": session.Agent.ID}, nil, ""); err != nil {
		t.Fatal(err)
	}
	settings, err := owner.Assistance(ctx)
	if err != nil || settings.Overseer {
		t.Fatal(settings, err)
	}
	yes := true
	if _, err = owner.UpdateAssistance(ctx, beepgrid.AssistanceUpdate{Overseer: &yes}); err == nil {
		t.Fatal("enabled unconfigured provider")
	}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Task string `json:"task"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch in.Task {
		case "overseer_plan":
			json.NewEncoder(w).Encode(engine.OverseerDecision{Operation: "question", AgentIDs: []string{session.Agent.ID}, Body: "How much is complete?", Summary: "Ask Scout for progress."})
		case "overseer_summary":
			json.NewEncoder(w).Encode(map[string]string{"summary": "Scout reports three pages complete."})
		default:
			t.Error(in.Task)
			http.Error(w, "unknown", 400)
		}
	}))
	defer provider.Close()
	if err = store.ConfigureOverseer(ctx, provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	settings, err = owner.UpdateAssistance(ctx, beepgrid.AssistanceUpdate{Overseer: &yes})
	if err != nil || !settings.Overseer {
		t.Fatal(settings, err)
	}
	if _, err = worker.UpdateAssistance(ctx, beepgrid.AssistanceUpdate{Overseer: &yes}); err == nil {
		t.Fatal("worker changed workspace assistance")
	}
	source, err := worker.Ask(ctx, beepgrid.AskInput{Target: beepgrid.Target{Kind: "person", ID: me.Person.ID}, Title: "Use GST inclusive prices?", SharedKey: "report/2026/v1"}, "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Respond(ctx, source.ID, beepgrid.Response{Outcome: "answered", Answer: "Yes", Version: 1}); err != nil {
		t.Fatal(err)
	}
	var published struct {
		ID string `json:"id"`
	}
	if err = owner.Do(ctx, "POST", "/api/v1/answers", map[string]any{"request_id": source.ID, "audience": []string{session.Agent.ID}}, &published, ""); err != nil {
		t.Fatal(err)
	}
	q, err := worker.Ask(ctx, beepgrid.AskInput{Target: beepgrid.Target{Kind: "person", ID: me.Person.ID}, Title: "Use GST inclusive prices?", SharedKey: "report/2026/v1"}, "future")
	if err != nil {
		t.Fatal(err)
	}
	q, err = worker.GetRequest(ctx, q.ID)
	if err != nil || q.State != "resolved" || q.Response.SourceAnswerID != published.ID {
		t.Fatal(q, err)
	}
	plan, err := owner.PlanOverseer(ctx, beepgrid.OverseerInput{Prompt: "Ask Scout what is complete"})
	if err != nil || !plan.RequiresConfirmation {
		t.Fatal(plan, err)
	}
	plan, err = owner.ConfirmOverseer(ctx, plan.ID)
	if err != nil || len(plan.Dispatches) != 1 {
		t.Fatal(plan, err)
	}
	if _, err = worker.Respond(ctx, plan.Dispatches[0].RequestID, beepgrid.Response{Outcome: "answered", Answer: "Three pages complete", Version: 1}); err != nil {
		t.Fatal(err)
	}
	// The same configured provider connection deliberately rate limits one person to one call/sec.
	// Reset only the test adapter's budget before independently exercising the summary protocol.
	if err = store.ConfigureOverseer(ctx, provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	plan, err = owner.SummarizeOverseer(ctx, plan.ID)
	if err != nil || plan.ReplySummary == "" || len(plan.Replies) != 1 {
		t.Fatal(plan, err)
	}
	plans, err := owner.OverseerPlans(ctx)
	if err != nil || len(plans) != 1 || plans[0].ID != plan.ID {
		t.Fatal(plans, err)
	}
	if err = owner.Do(ctx, "POST", "/api/v1/fleet/command", map[string]any{"plan_id": plan.ID, "body": "substitute command", "target": map[string]string{"kind": "agent", "id": session.Agent.ID}}, nil, ""); err == nil {
		t.Fatal("client substituted saved plan contents")
	}
	if err = owner.Do(ctx, "POST", "/api/v1/requests/"+q.ID+"/responses", map[string]any{"outcome": "answered", "answer": "fake", "version": 1, "reuse_method": "automatic"}, nil, ""); err == nil {
		t.Fatal("forged automatic provenance")
	}
	no := false
	if _, err = owner.UpdateAssistance(ctx, beepgrid.AssistanceUpdate{Overseer: &no}); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.OverseerPlan(ctx, plan.ID); err == nil {
		t.Fatal("disabled overseer accessible")
	}
}

func TestRetiredMatchingAPIIsUnavailable(t *testing.T) {
	_, owner, _ := setup(t)
	ctx := context.Background()
	var settings map[string]any
	if err := owner.Do(ctx, "GET", "/api/v1/assistance/settings", nil, &settings, ""); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"automatic_answers", "matcher_configured"} {
		if _, exists := settings[field]; exists {
			t.Fatal("retired field exposed", field)
		}
	}
	for _, path := range []string{"/api/v1/requests/missing/suggestions", "/api/v1/answer-proposals/missing"} {
		err := owner.Do(ctx, "POST", path, map[string]any{}, nil, "")
		var apiError *beepgrid.APIError
		if !errors.As(err, &apiError) || (apiError.Status != 404 && apiError.Status != 405) {
			t.Fatal("retired route still available", path, err)
		}
	}
	err := owner.Do(ctx, "POST", "/api/v1/assistance/settings", map[string]any{"automatic_answers": true}, nil, "")
	var apiError *beepgrid.APIError
	if !errors.As(err, &apiError) || apiError.Status != 400 {
		t.Fatal("retired toggle accepted", err)
	}
}
