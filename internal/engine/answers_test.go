package engine

import (
	"context"
	"testing"
	"time"
)

func TestSharedAnswersKeepWaitersScopedAndInvalidate(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	scout := testWorker(t, s, a, "scout")
	builder := testWorker(t, s, a, "builder")
	unrelated := testWorker(t, s, a, "unrelated")
	ask := AskInput{Target: Target{"person", a.ID}, Title: "Tax inclusive?", Choices: []string{"Yes", "No"}, SharedKey: "report/india/fy2026/v1"}
	source, e := s.Ask(ctx, scout, ask, "source")
	if e != nil {
		t.Fatal(e)
	}
	waiter, e := s.Ask(ctx, builder, ask, "waiter")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, a, source.ID, Response{Outcome: "answered", Answer: "Yes", Version: 1}); e != nil {
		t.Fatal(e)
	}
	answer, e := s.PublishAnswer(ctx, a, PublishInput{RequestID: source.ID, Audience: []string{scout.ID, builder.ID}})
	if e != nil {
		t.Fatal(e)
	}
	resolved, e := s.Request(ctx, builder, waiter.ID)
	if e != nil || resolved.State != "resolved" || resolved.Response.SourceAnswerID != answer.ID {
		t.Fatal(resolved, e)
	}
	q, e := s.Ask(ctx, builder, ask, "future")
	if e != nil || q.State != "resolved" || q.Response.Answer != "Yes" {
		t.Fatal(q, e)
	}
	q, e = s.Ask(ctx, unrelated, ask, "other-audience")
	if e != nil || q.State != "open" {
		t.Fatal("answer crossed audience boundary", q, e)
	}
	changed := ask
	changed.Body = "A different resource"
	q, e = s.Ask(ctx, builder, changed, "different-context")
	if e != nil || q.State != "open" {
		t.Fatal("answer crossed context boundary", q, e)
	}
	if e = s.InvalidateAnswer(ctx, a, answer.ID); e != nil {
		t.Fatal(e)
	}
	q, e = s.Ask(ctx, builder, ask, "after-invalidation")
	if e != nil || q.State != "open" {
		t.Fatal("invalidated answer reused", q, e)
	}
}
func TestSemanticSuggestionsCannotInventAuthority(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "worker")
	source, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Tax inclusive?", SharedKey: "policy/1"}, "source")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, a, source.ID, Response{Outcome: "answered", Answer: "Yes", Version: 1}); e != nil {
		t.Fatal(e)
	}
	answer, e := s.PublishAnswer(ctx, a, PublishInput{RequestID: source.ID, Audience: []string{w.ID}, ValidUntil: time.Now().Add(time.Hour)})
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Should tax be included?", SharedKey: "policy/1"}, "paraphrase")
	if e != nil {
		t.Fatal(e)
	}
	s.Matcher = func(_ context.Context, r Request, candidates []SharedAnswer) ([]MatchSuggestion, error) {
		if len(candidates) != 1 {
			t.Error("unexpected candidate scope")
		}
		return []MatchSuggestion{{"invented-private-answer", "Trust me"}, {answer.ID, "Same policy; review scope before accepting."}}, nil
	}
	proposals, e := s.SuggestAnswers(ctx, a, q.ID)
	if e != nil || len(proposals) != 1 {
		t.Fatal(proposals, e)
	}
	if q, e = s.Request(ctx, w, q.ID); e != nil || q.State != "open" {
		t.Fatal("model suggestion resolved a request", e)
	}
	if _, e = s.DecideProposal(ctx, a, proposals[0].ID, true); e != nil {
		t.Fatal(e)
	}
	q, e = s.Request(ctx, w, q.ID)
	if e != nil || q.State != "resolved" {
		t.Fatal(q, e)
	}
	approval, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Kind: "approval", Title: "Deploy?", Action: &Action{Name: "deploy", ExecutorID: w.ID, Resource: "app", ResourceVersion: "v1", OperationKey: "deploy-v1"}}, "approval")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SuggestAnswers(ctx, a, approval.ID); ErrorStatus(e) != 400 {
		t.Fatal("approval given semantic answer suggestions", e)
	}
}

func TestInvalidationDuringSemanticReviewIsAtomic(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "review-worker")
	source, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Which policy?", SharedKey: "policy/2"}, "src")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, a, source.ID, Response{Outcome: "answered", Answer: "v2", Version: 1}); e != nil {
		t.Fatal(e)
	}
	answer, e := s.PublishAnswer(ctx, a, PublishInput{RequestID: source.ID, Audience: []string{w.ID}})
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "What policy applies?"}, "q")
	if e != nil {
		t.Fatal(e)
	}
	s.Matcher = func(_ context.Context, _ Request, _ []SharedAnswer) ([]MatchSuggestion, error) {
		return []MatchSuggestion{{answer.ID, "Same policy, requires review."}}, nil
	}
	proposals, e := s.SuggestAnswers(ctx, a, q.ID)
	if e != nil || len(proposals) != 1 {
		t.Fatal(proposals, e)
	}
	if e = s.InvalidateAnswer(ctx, a, answer.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DecideProposal(ctx, a, proposals[0].ID, true); ErrorStatus(e) != 403 {
		t.Fatal("invalidated answer accepted", e)
	}
	current, e := s.Request(ctx, w, q.ID)
	if e != nil || current.State != "open" {
		t.Fatal(current, e)
	}
}
