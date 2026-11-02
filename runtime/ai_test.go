package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/beepgrid/beepgrid/sdk/go"
)

func TestEmbeddedRuntimeAIConfigurationAndTenantBoundaries(t *testing.T) {
	base, c := service(t)
	ctx := context.Background()
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer model-only-fixture" {
			t.Error("model configuration lost")
		}
		var wire struct {
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil || len(wire.Messages) != 2 {
			t.Error("invalid model request")
			http.Error(w, "invalid", 400)
			return
		}
		var payload struct{ Sources []struct{ Agent beepgrid.Agent } }
		if err := json.Unmarshal([]byte(wire.Messages[1].Content), &payload); err != nil || len(payload.Sources) != 1 {
			t.Error("cross-tenant model context", err)
			http.Error(w, "invalid", 400)
			return
		}
		text, _ := json.Marshal(map[string]any{"instructions": []any{}, "operation": "status", "agent_ids": []string{payload.Sources[0].Agent.ID}, "body": "", "summary": "One agent in the current tenant"})
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(text)}}}})
	}))
	defer model.Close()
	values := map[string]string{"BEEPGRID_AI_PROVIDER": "openai-compatible", "BEEPGRID_AI_MODEL": "explicit-model", "BEEPGRID_AI_API_KEY": "model-only-fixture", "BEEPGRID_AI_BASE_URL": model.URL + "/v1", "BEEPGRID_AI_ALLOW_PRIVATE": "true"}
	ai, err := AIConfigFromEnv(func(k string) string { return values[k] })
	if err != nil {
		t.Fatal(err)
	}
	c.AI = ai
	configured, err := Open(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer configured.Close()
	if base.store.OverseerPlanner != nil || configured.store.AllowLocal {
		t.Fatal("model configuration escaped its runtime or network boundary")
	}
	api := httptest.NewServer(configured.Handler())
	defer api.Close()
	idA, idB := "a_"+c.Schema, "b_"+c.Schema
	tokenA, tokenB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	if err = configured.ProvisionWorkspace(ctx, idA, "A", tokenA); err != nil {
		t.Fatal(err)
	}
	if err = configured.ProvisionWorkspace(ctx, idB, "B", tokenB); err != nil {
		t.Fatal(err)
	}
	a, b := beepgrid.New(api.URL, tokenA), beepgrid.New(api.URL, tokenB)
	_, sessionA, err := a.Enroll(ctx, beepgrid.EnrollInput{Agent: "same-name", Name: "A agent"})
	if err != nil {
		t.Fatal(err)
	}
	_, sessionB, err := b.Enroll(ctx, beepgrid.EnrollInput{Agent: "same-name", Name: "B agent"})
	if err != nil {
		t.Fatal(err)
	}
	settings, err := a.Assistance(ctx)
	if err != nil || settings.Overseer || !settings.OverseerConfigured || calls.Load() != 0 {
		t.Fatal("embedded configuration enabled a feature or chose a different model", err)
	}
	yes := true
	if _, err = a.UpdateAssistance(ctx, beepgrid.AssistanceUpdate{Overseer: &yes}); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PlanOverseer(ctx, beepgrid.OverseerInput{Prompt: "A status"})
	if err != nil || len(plan.Sources) != 1 || plan.Sources[0].Agent.ID != sessionA.Agent.ID || plan.RequiresConfirmation {
		t.Fatal("wrong tenant source", err)
	}
	if _, err = b.PlanOverseer(ctx, beepgrid.OverseerInput{Prompt: "B status"}); err == nil || calls.Load() != 1 {
		t.Fatal("workspace toggle crossed tenant boundary", err)
	}
	if _, err = b.UpdateAssistance(ctx, beepgrid.AssistanceUpdate{Overseer: &yes}); err != nil {
		t.Fatal(err)
	}
	other, err := b.PlanOverseer(ctx, beepgrid.OverseerInput{Prompt: "B status"})
	if err != nil || len(other.Sources) != 1 || other.Sources[0].Agent.ID != sessionB.Agent.ID || calls.Load() != 2 {
		t.Fatal("shared provider mixed tenant data", err)
	}
	_, err = b.OverseerPlan(ctx, plan.ID)
	var apiError *beepgrid.APIError
	if !errors.As(err, &apiError) || apiError.Status != 404 {
		t.Fatal("cross-tenant plan exposed", err)
	}
	bad := c
	bad.AI.Model = ""
	if _, err = Open(ctx, bad); err == nil || !strings.Contains(err.Error(), "BEEPGRID_AI_MODEL") {
		t.Fatal("embedded runtime chose a default model", err)
	}
}
