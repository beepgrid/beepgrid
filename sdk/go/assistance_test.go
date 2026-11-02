package beepgrid

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type assistanceTransport func(*http.Request) (*http.Response, error)

func (f assistanceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAssistanceDeadlineIsScopedAndCancellationPreserved(t *testing.T) {
	c := New("http://localhost", "person-token")
	original := c.HTTP.Timeout
	c.HTTP.Transport = assistanceTransport(func(r *http.Request) (*http.Response, error) {
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) < 30*time.Second {
			t.Error("model call cut short by ordinary timeout")
		}
		if r.Header.Get("Authorization") != "Bearer person-token" {
			t.Error("caller identity changed")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"ovr_1"}`))}, nil
	})
	if plan, err := c.PlanOverseer(context.Background(), OverseerInput{Prompt: "Status?"}); err != nil || plan.ID != "ovr_1" {
		t.Fatal(plan, err)
	}
	if c.HTTP.Timeout != original {
		t.Fatal("ordinary client mutated")
	}
	c.AssistanceTimeout = 10 * time.Millisecond
	c.HTTP.Transport = assistanceTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })
	start := time.Now()
	if _, err := c.SummarizeOverseer(context.Background(), "ovr_1"); err == nil || time.Since(start) > time.Second {
		t.Fatal("configured assistance deadline ignored", err)
	}
}

func TestOverseerFollowupAndRetryIdentity(t *testing.T) {
	c := New("http://localhost", "person-token")
	c.HTTP.Transport = assistanceTransport(func(r *http.Request) (*http.Response, error) {
		var in OverseerInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Fatal(err)
		}
		if in.PreviousPlanID != "ovr_previous" || r.Header.Get("Idempotency-Key") != "turn-2" {
			t.Error("follow-up context or retry identity lost")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"id":"ovr_2","previous_plan_id":"ovr_previous"}`))}, nil
	})
	p, err := c.PlanOverseer(context.Background(), OverseerInput{Prompt: "Follow up", PreviousPlanID: "ovr_previous"}, "turn-2")
	if err != nil || p.PreviousPlanID != "ovr_previous" {
		t.Fatal(p, err)
	}
}
