package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestExactQuestionsCoalesceExternalAttentionAndKeepIndependentRequests(t *testing.T) {
	s, a := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	first := testWorker(t, s, a, "first")
	second := testWorker(t, s, a, "second")
	var sends atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1); w.WriteHeader(202) }))
	defer provider.Close()
	c, e := s.SaveConnection(ctx, a, Connection{Name: "Attention", Kind: "webhook", Enabled: true, Config: map[string]string{"url": provider.URL}, Secret: "test-signing-secret"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, a, Destination{ConnectionID: c.ID, PrincipalID: a.ID, Address: "owner"}); e != nil {
		t.Fatal(e)
	}
	input := AskInput{Target: Target{"person", a.ID}, Title: "Tax inclusive?", Choices: []string{"Yes", "No"}, SharedKey: "report/india/v1"}
	q1, e := s.Ask(ctx, first, input, "first")
	if e != nil {
		t.Fatal(e)
	}
	q2, e := s.Ask(ctx, second, input, "second")
	if e != nil {
		t.Fatal(e)
	}
	if q1.ID == q2.ID {
		t.Fatal("independent handles lost")
	}
	for i := 0; i < 3; i++ {
		if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
			t.Fatal(e)
		}
	}
	if sends.Load() != 1 {
		t.Fatal("duplicate interruption", sends.Load())
	}
	if _, e = s.Respond(ctx, a, q1.ID, Response{Outcome: "answered", Answer: "Yes", Version: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PublishAnswer(ctx, a, PublishInput{RequestID: q1.ID, Audience: []string{first.ID, second.ID}}); e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	if sends.Load() != 1 {
		t.Fatal("resolved waiter sent redundant notification", sends.Load())
	}
	done, e := s.Request(ctx, second, q2.ID)
	if e != nil || done.State != "resolved" {
		t.Fatal(done, e)
	}
}

func TestAnswerAndShareRollsBackAnInvalidAudience(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "atomic-shared")
	q, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Reuse?", SharedKey: "policy/v1"}, "atomic")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RespondAndShare(ctx, a, q.ID, Response{Outcome: "answered", Answer: "Yes", Version: 1}, []string{"missing-person"}); ErrorStatus(e) != 400 {
		t.Fatal(e)
	}
	current, e := s.Request(ctx, w, q.ID)
	if e != nil || current.State != "open" {
		t.Fatal("partial grouped answer committed", current, e)
	}
	if _, e = s.RespondAndShare(ctx, a, q.ID, Response{Outcome: "answered", Answer: "Yes", Version: 1}, []string{w.ID}); e != nil {
		t.Fatal(e)
	}
}

func TestCancelledAttentionLeaderPromotesOnlyOneWaitingQuestion(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	var sends atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1); w.WriteHeader(202) }))
	defer provider.Close()
	c, e := s.SaveConnection(ctx, owner, Connection{Name: "Attention", Kind: "webhook", Enabled: true, Config: map[string]string{"url": provider.URL}, Secret: "test-signing-secret"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: owner.ID, Address: "owner"}); e != nil {
		t.Fatal(e)
	}
	questions := []Request{}
	for _, name := range []string{"alpha", "beta", "gamma"} {
		worker := testWorker(t, s, owner, name)
		q, e := s.Ask(ctx, worker, AskInput{Target: Target{"person", owner.ID}, Title: "One shared question", SharedKey: "same/scope/v1"}, name)
		if e != nil {
			t.Fatal(e)
		}
		questions = append(questions, q)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	if sends.Load() != 1 {
		t.Fatal(sends.Load())
	}
	if _, e = s.EndRequest(ctx, owner, questions[0].ID, "cancelled", 1); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 4; i++ {
		if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
			t.Fatal(e)
		}
	}
	if sends.Load() != 2 {
		t.Fatal("more than one successor notified", sends.Load())
	}
	for _, q := range questions[1:] {
		current, e := s.Request(ctx, owner, q.ID)
		if e != nil || current.State != "open" {
			t.Fatal("coalescing changed independent request", current, e)
		}
	}
	if _, e = s.EndRequest(ctx, owner, questions[1].ID, "cancelled", 1); e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	if sends.Load() != 3 {
		t.Fatal("last independent question lost", sends.Load())
	}
}
