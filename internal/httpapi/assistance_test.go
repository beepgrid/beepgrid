package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/notif-dev/notif/internal/engine"
	"github.com/notif-dev/notif/sdk/go/notif"
)

func TestAssistanceSDKHTTPJourney(t *testing.T) {
	store, owner, _ := setup(t)
	ctx := context.Background()
	store.AllowLocal = true
	var me struct {
		Person notif.Person `json:"person"`
	}
	if err := owner.Do(ctx, "GET", "/api/v1/me", nil, &me, ""); err != nil {
		t.Fatal(err)
	}
	worker, session, err := owner.Enroll(ctx, notif.EnrollInput{Agent: "assistance/scout", Name: "Scout"})
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
	if err != nil || settings.AutomaticAnswers || settings.Overseer {
		t.Fatal(settings, err)
	}
	yes := true
	if _, err = owner.UpdateAssistance(ctx, notif.AssistanceUpdate{Overseer: &yes}); err == nil {
		t.Fatal("enabled unconfigured provider")
	}
	var answerID string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Task string `json:"task"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		switch in.Task {
		case "automatic_answer_match":
			json.NewEncoder(w).Encode(engine.AutomaticMatch{AnswerID: answerID, Equivalent: true, Confidence: 1, Reason: "Same policy and informational meaning"})
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
	if err = store.ConfigureAutomaticAnswers(ctx, provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	if err = store.ConfigureOverseer(ctx, provider.URL, ""); err != nil {
		t.Fatal(err)
	}
	settings, err = owner.UpdateAssistance(ctx, notif.AssistanceUpdate{AutomaticAnswers: &yes, Overseer: &yes})
	if err != nil || !settings.Overseer || !settings.AutomaticAnswers {
		t.Fatal(settings, err)
	}
	if _, err = worker.UpdateAssistance(ctx, notif.AssistanceUpdate{Overseer: &yes}); err == nil {
		t.Fatal("worker changed workspace assistance")
	}
	source, err := worker.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: me.Person.ID}, Title: "Use GST inclusive prices?", SharedKey: "report/2026/v1"}, "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = owner.Respond(ctx, source.ID, notif.Response{Outcome: "answered", Answer: "Yes", Version: 1}); err != nil {
		t.Fatal(err)
	}
	var published struct {
		ID string `json:"id"`
	}
	if err = owner.Do(ctx, "POST", "/api/v1/answers", map[string]any{"request_id": source.ID, "audience": []string{session.Agent.ID}}, &published, ""); err != nil {
		t.Fatal(err)
	}
	answerID = published.ID
	q, err := worker.Ask(ctx, notif.AskInput{Target: notif.Target{Kind: "person", ID: me.Person.ID}, Title: "Should this report's prices include tax?", SharedKey: "report/2026/v1"}, "future")
	if err != nil {
		t.Fatal(err)
	}
	for {
		worked, err := store.ProcessAutomaticAnswer(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	q, err = worker.GetRequest(ctx, q.ID)
	if err != nil || q.State != "resolved" || q.Response.ReuseMethod != "automatic" {
		t.Fatal(q, err)
	}
	plan, err := owner.PlanOverseer(ctx, notif.OverseerInput{Prompt: "Ask Scout what is complete"})
	if err != nil || !plan.RequiresConfirmation {
		t.Fatal(plan, err)
	}
	plan, err = owner.ConfirmOverseer(ctx, plan.ID)
	if err != nil || len(plan.Dispatches) != 1 {
		t.Fatal(plan, err)
	}
	if _, err = worker.Respond(ctx, plan.Dispatches[0].RequestID, notif.Response{Outcome: "answered", Answer: "Three pages complete", Version: 1}); err != nil {
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
	if _, err = owner.UpdateAssistance(ctx, notif.AssistanceUpdate{Overseer: &no, AutomaticAnswers: &no}); err != nil {
		t.Fatal(err)
	}
	if _, err = owner.OverseerPlan(ctx, plan.ID); err == nil {
		t.Fatal("disabled overseer accessible")
	}
}
