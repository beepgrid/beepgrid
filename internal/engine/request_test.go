package engine

import (
	"context"
	"sync"
	"testing"
	"time"
)

func testWorker(t testing.TB, s *Store, a Actor, name string) Actor {
	t.Helper()
	ctx := context.Background()
	session, e := s.Enroll(ctx, a, EnrollInput{Agent: name, Name: name, Capabilities: []string{"message", "status", "task"}})
	if e != nil {
		t.Fatal(e)
	}
	role, e := s.SaveRole(ctx, a, Role{Name: name + " role", Rules: []Rule{{Action: "*", TargetKind: "*", TargetID: "*"}}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(ctx, a, Binding{role.ID, "principal", session.Agent.ID}, false); e != nil {
		t.Fatal(e)
	}
	actor, e := s.Authenticate(ctx, session.Token)
	if e != nil {
		t.Fatal(e)
	}
	return actor
}
func testPerson(t testing.TB, s *Store, a Actor, name string) Actor {
	t.Helper()
	ctx := context.Background()
	p, e := s.CreatePerson(ctx, a, CreatePerson{ExternalID: name, Name: name})
	if e != nil {
		t.Fatal(e)
	}
	key, e := s.NewCredential(ctx, a, p.ID, "api", "")
	if e != nil {
		t.Fatal(e)
	}
	out, e := s.Authenticate(ctx, key["token"].(string))
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func grant(t testing.TB, s *Store, a Actor, target Actor, rules []Rule) Role {
	t.Helper()
	r, e := s.SaveRole(context.Background(), a, Role{Name: ID("role"), Rules: rules})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindRole(context.Background(), a, Binding{r.ID, "principal", target.ID}, false); e != nil {
		t.Fatal(e)
	}
	return r
}
func TestRequestsSurviveClarificationAndEnforceOneDecision(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "scout")
	q, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Which tax convention?", Choices: []string{"Inclusive", "Exclusive"}}, "question-1")
	if e != nil {
		t.Fatal(e)
	}
	q2, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Which locale?"}, "question-2")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, a, SendInput{ConversationID: q.ConversationID, Body: "Which report is this for?"}, "clarify"); e != nil {
		t.Fatal(e)
	}
	pending, e := s.Request(ctx, w, q2.ID)
	if e != nil || pending.State != "open" {
		t.Fatal(pending, e)
	}
	var wg sync.WaitGroup
	success := make(chan string, 2)
	for _, answer := range []string{"Inclusive", "Exclusive"} {
		wg.Add(1)
		go func(answer string) {
			defer wg.Done()
			_, err := s.Respond(ctx, a, q.ID, Response{Outcome: "answered", Answer: answer, Version: 1})
			if err == nil {
				success <- answer
			} else if ErrorStatus(err) != 409 {
				t.Errorf("unexpected error: %v", err)
			}
		}(answer)
	}
	wg.Wait()
	close(success)
	if len(success) != 1 {
		t.Fatalf("expected one decision, got %d", len(success))
	}
	final, e := s.Request(ctx, w, q.ID)
	if e != nil || final.State != "resolved" || final.Response == nil {
		t.Fatal(final, e)
	}
	again, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Which tax convention?", Choices: []string{"Inclusive", "Exclusive"}}, "question-1")
	if e != nil || again.ID != q.ID {
		t.Fatal("retry did not return original durable request", e)
	}
	if _, e = s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Different?"}, "question-1"); ErrorStatus(e) != 409 {
		t.Fatal("idempotency conflict missing", e)
	}
}
func TestPrivateGroupApprovalAndCurrentPermissions(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "billing")
	alice := testPerson(t, s, a, "alice")
	bob := testPerson(t, s, a, "bob")
	for _, p := range []Actor{alice, bob} {
		grant(t, s, a, p, []Rule{{Action: "view", TargetKind: "*", TargetID: "*", ConnectionID: "", ActionName: ""}, {Action: "message", TargetKind: "agent", TargetID: w.ID, ConnectionID: "", ActionName: ""}, {Action: "decide", TargetKind: "agent", TargetID: w.ID, ConnectionID: "", ActionName: "refund"}})
	}
	g, e := s.SaveGroup(ctx, a, Group{Name: "Finance", Members: []string{alice.ID, bob.ID}})
	if e != nil {
		t.Fatal(e)
	}
	action := Action{Name: "refund", ExecutorID: w.ID, Resource: "invoice/123", ResourceVersion: "2", Arguments: map[string]any{"amount": 123}, OperationKey: "refund-123"}
	q, e := s.Ask(ctx, w, AskInput{Target: Target{"group", g.ID}, Kind: "approval", Title: "Refund invoice?", Action: &action}, "approval")
	if e != nil {
		t.Fatal(e)
	}
	aq, e := s.Request(ctx, alice, q.ID)
	if e != nil {
		t.Fatal(e)
	}
	bq, e := s.Request(ctx, bob, q.ID)
	if e != nil {
		t.Fatal(e)
	}
	if aq.ConversationID == bq.ConversationID {
		t.Fatal("private recipients share a conversation")
	}
	if _, e = s.Send(ctx, alice, SendInput{ConversationID: aq.ConversationID, Body: "Private details"}, "private"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Conversation(ctx, bob, aq.ConversationID); ErrorStatus(e) != 404 {
		t.Fatal("private conversation leaked", e)
	}
	if _, e = s.Respond(ctx, alice, q.ID, Response{Outcome: "approved", Version: 1}); e != nil {
		t.Fatal(e)
	}
	x, e := s.Redeem(ctx, w, q.ID, action)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.Redeem(ctx, w, q.ID, action)
	if e != nil || again.ID != x.ID {
		t.Fatal("redemption not recoverable", e)
	}
	action.ResourceVersion = "3"
	if _, e = s.Redeem(ctx, w, q.ID, action); ErrorStatus(e) != 403 {
		t.Fatal("changed operation accepted", e)
	}
	if e = s.SetDisabled(ctx, a, alice.ID, true); e != nil {
		t.Fatal(e)
	}
	action.ResourceVersion = "2"
	if _, e = s.Redeem(ctx, w, q.ID, action); ErrorStatus(e) != 403 {
		t.Fatal("revoked approver still authorizes execution", e)
	}
}
func TestWorkerLeasesAndReplay(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "research")
	session, e := s.Enroll(ctx, a, EnrollInput{Agent: "research", Name: "research"})
	if e != nil {
		t.Fatal(e)
	}
	w2, e := s.Authenticate(ctx, session.Token)
	if e != nil {
		t.Fatal(e)
	}
	sent, e := s.Send(ctx, a, SendInput{Target: Target{"agent", w.ID}, Body: "Status?"}, "send")
	if e != nil {
		t.Fatal(e)
	}
	jobs, e := s.Inbox(ctx, w)
	if e != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%+v err=%v", jobs, e)
	}
	jobs2, e := s.Inbox(ctx, w2)
	if e != nil || len(jobs2) != 0 {
		t.Fatal("replicas both received one logical item", e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE deliveries SET lease_until=now()-interval '1 second' WHERE id=$1`, jobs[0].ID); e != nil {
		t.Fatal(e)
	}
	jobs2, e = s.Inbox(ctx, w2)
	if e != nil || len(jobs2) != 1 {
		t.Fatal(jobs2, e)
	}
	if e = s.Ack(ctx, w, jobs[0].ID, jobs[0].Generation); ErrorStatus(e) != 409 {
		t.Fatal("stale worker ack accepted", e)
	}
	if e = s.Ack(ctx, w2, jobs2[0].ID, jobs2[0].Generation); e != nil {
		t.Fatal(e)
	}
	events, e := s.Events(ctx, w2, 0)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == "message.created" {
			found = true
		}
	}
	if !found {
		t.Fatal("message absent from durable replay", sent)
	}
	if len(jobs2[0].Payload) == 0 || jobs2[0].Payload["_policy"] != nil {
		t.Fatal("invalid public delivery payload")
	}
}
func TestSupersessionExpiryAndAuthorization(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "worker")
	outsider := testPerson(t, s, a, "outsider")
	q, e := s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Approve direction?"}, "one")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, outsider, q.ID, Response{Outcome: "answered", Answer: "yes", Version: 1}); ErrorStatus(e) != 404 {
		t.Fatal(e)
	}
	if _, e = s.EndRequest(ctx, w, q.ID, "superseded", 1); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, a, q.ID, Response{Outcome: "answered", Answer: "yes", Version: 1}); ErrorStatus(e) != 409 {
		t.Fatal(e)
	}
	q, e = s.Ask(ctx, w, AskInput{Target: Target{"person", a.ID}, Title: "Expire me", ExpiresAt: time.Now().Add(time.Minute)}, "two")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE requests SET expires_at=now()-interval '1 second' WHERE id=$1`, q.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, a, q.ID, Response{Outcome: "answered", Answer: "yes", Version: 1}); ErrorStatus(e) != 409 {
		t.Fatal(e)
	}
}
