package engine

import (
	"context"
	"testing"
)

func TestExactAnswerReuseRequiresCurrentIntendedResponder(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	first := testWorker(t, s, owner, "first")
	second := testWorker(t, s, owner, "second")
	alice := testPerson(t, s, owner, "alice")
	bob := testPerson(t, s, owner, "bob")
	grant(t, s, owner, bob, []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}})
	for _, worker := range []Actor{first, second} {
		grant(t, s, owner, worker, []Rule{{Action: "ask", TargetKind: "person", TargetID: alice.ID}, {Action: "ask", TargetKind: "person", TargetID: bob.ID}})
	}
	authorRole := grant(t, s, owner, alice, []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}, {Action: "message", TargetKind: "agent", TargetID: "*"}})
	ask := AskInput{Target: Target{"person", alice.ID}, Title: "Include GST?", SharedKey: "qa/india/2026/v1"}
	source, err := s.Ask(ctx, first, ask, "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Respond(ctx, alice, source.ID, Response{Outcome: "answered", Version: 1, Answer: "Yes"}); err != nil {
		t.Fatal(err)
	}
	other := ask
	other.Target.ID = bob.ID
	pendingBob, err := s.Ask(ctx, second, other, "pending-bob")
	if err != nil {
		t.Fatal(err)
	}
	pendingAlice, err := s.Ask(ctx, second, ask, "pending-alice")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PublishAnswer(ctx, owner, PublishInput{RequestID: source.ID, Audience: []string{first.ID, second.ID}}); err != nil {
		t.Fatal(err)
	}
	if current, e := s.Request(ctx, owner, pendingBob.ID); e != nil || current.State != "open" {
		t.Fatal("publication crossed responder boundary", current, e)
	}
	if current, e := s.Request(ctx, owner, pendingAlice.ID); e != nil || current.State != "resolved" {
		t.Fatal("authorized waiter not reused", current, e)
	}
	if current, e := s.Ask(ctx, second, other, "future-bob"); e != nil || current.State != "open" {
		t.Fatal("exact reuse crossed responder boundary", current, e)
	}
	if current, e := s.Ask(ctx, second, ask, "future-alice"); e != nil || current.State != "resolved" {
		t.Fatal("authorized exact reuse failed", current, e)
	}
	authorRole.Rules = []Rule{{Action: "view", TargetKind: "agent", TargetID: "*"}, {Action: "message", TargetKind: "agent", TargetID: second.ID, Effect: "deny"}}
	if _, err = s.SaveRole(ctx, owner, authorRole); err != nil {
		t.Fatal(err)
	}
	if current, e := s.Ask(ctx, second, ask, "revoked-author"); ErrorStatus(e) != 403 {
		t.Fatal("blocked author accepted a new request", current, e)
	}
}

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
