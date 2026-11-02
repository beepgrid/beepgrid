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

	"github.com/beepgrid/beepgrid/internal/aimodel"
)

func nativeAssistanceFixture(t *testing.T, s *Store, provider string, respond func(string, map[string]any) any) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer model-fixture" && r.Header.Get("x-api-key") != "model-fixture" && r.Header.Get("x-goog-api-key") != "model-fixture" {
			t.Error("model credential was lost")
		}
		var in map[string]any
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		var data string
		switch provider {
		case "openai":
			data = in["input"].([]any)[1].(map[string]any)["content"].(string)
		case "anthropic":
			data = in["messages"].([]any)[0].(map[string]any)["content"].(string)
		case "google":
			data = in["contents"].([]any)[0].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"].(string)
		default:
			data = in["messages"].([]any)[1].(map[string]any)["content"].(string)
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		task, _ := payload["task"].(string)
		result := respond(task, payload)
		raw, _ := json.Marshal(result)
		var envelope any
		switch provider {
		case "openai":
			envelope = map[string]any{"status": "completed", "output": []any{map[string]any{"type": "message", "status": "completed", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": string(raw)}}}}}
		case "anthropic":
			envelope = map[string]any{"type": "message", "role": "assistant", "stop_reason": "end_turn", "content": []any{map[string]any{"type": "text", "text": string(raw)}}}
		case "google":
			envelope = map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP", "content": map[string]any{"role": "model", "parts": []any{map[string]any{"text": string(raw)}}}}}}
		default:
			envelope = map[string]any{"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(raw)}}}}
		}
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	t.Cleanup(server.Close)
	if err := s.ConfigureAI(context.Background(), aimodel.Config{Provider: provider, Model: "explicitly-selected", APIKey: "model-fixture", BaseURL: server.URL + "/v1", AllowPrivate: true}); err != nil {
		t.Fatal(err)
	}
	return &calls
}

func TestNativeAIEndToEnd(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "google", "openai-compatible"} {
		t.Run(provider, func(t *testing.T) {
			s, owner := testStore(t)
			ctx := context.Background()
			worker := testWorker(t, s, owner, "scout")
			hidden := testWorker(t, s, owner, "hidden")
			person := testPerson(t, s, owner, "operator")
			grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: worker.ID}, {Action: "ask", TargetKind: "agent", TargetID: worker.ID}, {Action: "message", TargetKind: "agent", TargetID: worker.ID}})
			calls := nativeAssistanceFixture(t, s, provider, func(task string, payload map[string]any) any {
				if strings.Contains(string(JSON(payload)), hidden.ID) {
					t.Error("hidden agent reached provider")
				}
				switch task {
				case "overseer_plan":
					return map[string]any{"instructions": []any{}, "operation": "question", "agent_ids": []string{worker.ID}, "body": "Report progress", "summary": "Ask the permitted agent for current progress"}
				case "overseer_summary":
					return map[string]any{"summary": "The permitted agent reports seven pages complete"}
				}
				t.Error("unexpected assistance task")
				return nil
			})
			settings, err := s.AssistanceSettings(ctx, owner)
			if err != nil || settings.Overseer || !settings.OverseerConfigured {
				t.Fatal(settings, err)
			}
			if _, err = s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Status?"}); ErrorStatus(err) != 409 || calls.Load() != 0 {
				t.Fatal("configuration enabled a feature", err)
			}
			enableAssistance(t, s, owner, true)
			plan, err := s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Ask my agent for progress"})
			if err != nil || !plan.RequiresConfirmation || len(plan.Dispatches) != 0 {
				t.Fatal(plan, err)
			}
			plan, err = s.ConfirmOverseer(ctx, person, plan.ID)
			if err != nil || len(plan.Dispatches) != 1 {
				t.Fatal(plan, err)
			}
			again, err := s.ConfirmOverseer(ctx, person, plan.ID)
			if err != nil || len(again.Dispatches) != 1 || again.Dispatches[0].RequestID != plan.Dispatches[0].RequestID {
				t.Fatal("duplicate confirmation", err)
			}
			if _, err = s.Respond(ctx, worker, plan.Dispatches[0].RequestID, Response{Outcome: "answered", Answer: "Seven pages complete", Version: 1}); err != nil {
				t.Fatal(err)
			}
			// Honor the existing one-call-per-person-per-second admission limit.
			time.Sleep(time.Second)
			plan, err = s.SummarizeOverseer(ctx, person, plan.ID)
			if err != nil || plan.ReplySummary == "" {
				t.Fatal(plan, err)
			}
			if _, err = s.SummarizeOverseer(ctx, person, plan.ID); err != nil || calls.Load() != 2 {
				t.Fatal("summary cache lost", calls.Load(), err)
			}
			if _, err = s.Pool.Exec(ctx, `DELETE FROM role_bindings WHERE scope=$1 AND subject_id=$2`, owner.Scope, person.ID); err != nil {
				t.Fatal(err)
			}
			if _, err = s.OverseerPlan(ctx, person, plan.ID); ErrorStatus(err) != 404 {
				t.Fatal("derived read survived revocation", err)
			}
			if _, err = s.SummarizeOverseer(ctx, person, plan.ID); ErrorStatus(err) != 404 || calls.Load() != 2 {
				t.Fatal("revoked caller reached provider", err)
			}
		})
	}
}

func TestNativeAIRechecksAuthorityAfterModelCall(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "google", "openai-compatible"} {
		for _, scenario := range []string{"invented target", "view revoked", "ask missing"} {
			t.Run(provider+"/"+scenario, func(t *testing.T) {
				s, owner := testStore(t)
				ctx := context.Background()
				worker := testWorker(t, s, owner, "scout")
				person := testPerson(t, s, owner, "limited")
				rules := []Rule{{Action: "view", TargetKind: "agent", TargetID: worker.ID}}
				if scenario != "ask missing" {
					rules = append(rules, Rule{Action: "ask", TargetKind: "agent", TargetID: worker.ID})
				}
				grant(t, s, owner, person, rules)
				nativeAssistanceFixture(t, s, provider, func(task string, payload map[string]any) any {
					id := worker.ID
					if scenario == "invented target" {
						id = "agt_not_supplied"
					}
					if scenario == "view revoked" {
						if _, err := s.Pool.Exec(ctx, `DELETE FROM role_bindings WHERE scope=$1 AND subject_id=$2`, owner.Scope, person.ID); err != nil {
							t.Error(err)
						}
					}
					return map[string]any{"instructions": []any{}, "operation": "question", "agent_ids": []string{id}, "body": "Report progress", "summary": "A bounded proposal"}
				})
				enableAssistance(t, s, owner, true)
				if _, err := s.PlanOverseer(ctx, person, OverseerInput{Prompt: "Ask my agent"}); ErrorStatus(err) != 403 {
					t.Fatal("model bypassed authority", err)
				}
				var count int
				if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM overseer_plans WHERE scope=$1`, owner.Scope).Scan(&count); err != nil || count != 0 {
					t.Fatal("unauthorized plan persisted", count, err)
				}
			})
		}
	}
}

func TestAIConfigurationAndLegacyIsolation(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	if err := s.ConfigureAI(ctx, aimodel.Config{}); err != nil || s.OverseerPlanner != nil {
		t.Fatal("unconfigured model activated", err)
	}
	if err := s.ConfigureAI(ctx, aimodel.Config{Provider: "openai"}); err == nil || s.OverseerPlanner != nil {
		t.Fatal("missing explicit model accepted")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected private call") }))
	defer server.Close()
	config := aimodel.Config{Provider: "openai-compatible", Model: "chosen", BaseURL: server.URL + "/v1"}
	if err := s.ConfigureAI(ctx, config); err == nil {
		t.Fatal("private model enabled without operator choice")
	}
	var nativeCalls, legacyCalls atomic.Int32
	nativeAssistanceFixture(t, s, "openai", func(task string, payload map[string]any) any {
		nativeCalls.Add(1)
		return map[string]any{"summary": "native summary"}
	})
	if s.AllowLocal {
		t.Fatal("AI configuration widened channel network permissions")
	}
	if _, err := s.validURL(ctx, server.URL); err == nil {
		t.Fatal("channel private-address policy changed")
	}
	legacy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		legacyCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer legacy-only" {
			t.Error("wrong gateway key")
		}
		_ = json.NewEncoder(w).Encode(OverseerDecision{Operation: "status", AgentIDs: []string{}, Summary: "legacy result"})
	}))
	defer legacy.Close()
	// Legacy local gateway behavior is unchanged: it still requires dev mode.
	s.AllowLocal = true
	if err := s.ConfigureOverseer(ctx, legacy.URL, "legacy-only"); err != nil {
		t.Fatal(err)
	}
	// Trust only this fixture's certificate, preserving the normal TLS default.
	s.assistanceTransports[len(s.assistanceTransports)-1].TLSClientConfig.RootCAs = legacy.Client().Transport.(*http.Transport).TLSClientConfig.RootCAs
	plan, err := s.OverseerPlanner(context.WithValue(ctx, assistanceActorKey{}, owner.ID), "status", nil)
	if err != nil || plan.Summary != "legacy result" || legacyCalls.Load() != 1 || nativeCalls.Load() != 0 {
		t.Fatal("legacy override failed", err)
	}
	if _, err = s.OverseerSummarizer(context.WithValue(ctx, assistanceActorKey{}, "different-person"), "summary", nil); err != nil {
		t.Fatal(err)
	}
	// Both operations use the same explicitly selected gateway.
	if legacyCalls.Load() != 2 || nativeCalls.Load() != 0 {
		t.Fatal("gateway override routing changed")
	}
}
