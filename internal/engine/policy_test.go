package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
	"testing"
)

func TestDirectionalConnectionGrantsAndGroupRemoval(t *testing.T) {
	s, a := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	w := testWorker(t, s, a, "permission-worker")
	p := testPerson(t, s, a, "approver")
	c, e := s.SaveConnection(ctx, a, Connection{Name: "Restricted", Kind: "webhook", Config: map[string]string{"url": "http://localhost:4123"}, Secret: "test-long-signing-secret", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	role := grant(t, s, a, p, []Rule{{Action: "view", TargetKind: "agent", TargetID: w.ID, ConnectionID: "", ActionName: ""}, {Action: "decide", TargetKind: "agent", TargetID: w.ID, ConnectionID: c.ID, ActionName: "deploy"}})
	explain, e := s.ExplainSubject(ctx, a, p.ID, "decide", Target{"agent", w.ID}, c.ID, "deploy")
	if e != nil || explain["allowed"] != true {
		t.Fatal(explain, e)
	}
	explain, e = s.ExplainSubject(ctx, a, p.ID, "message", Target{"agent", w.ID}, "", "")
	if e != nil || explain["allowed"] != false {
		t.Fatal("view implied contact", explain, e)
	}
	g, e := s.SaveGroup(ctx, a, Group{Name: "Approvers", Members: []string{p.ID}})
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.Ask(ctx, w, AskInput{Target: Target{"group", g.ID}, Kind: "approval", Title: "Deploy?", Action: &Action{Name: "deploy", ExecutorID: w.ID, Resource: "app", ResourceVersion: "v1", OperationKey: "deploy"}}, "group-approval")
	if e != nil {
		t.Fatal(e)
	}
	provider := p
	provider.ProviderConnection = c.ID
	if _, e = s.Respond(ctx, p, q.ID, Response{Outcome: "approved", Version: 1}); ErrorStatus(e) != 403 {
		t.Fatal("connection restriction bypassed", e)
	}
	if _, e = s.SaveGroup(ctx, a, Group{ID: g.ID, Name: g.Name, Members: []string{}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, provider, q.ID, Response{Outcome: "approved", Version: 1}); ErrorStatus(e) != 403 {
		t.Fatal("removed group member decided", e)
	}
	_ = role
}
func TestWorkspaceAndExpiredCredentialIsolation(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "isolated")
	other, e := s.Bootstrap(ctx, "other/production", "Other", "production", Secret())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, other, SendInput{Target: Target{"agent", w.ID}, Body: "Cross tenant"}, "cross"); ErrorStatus(e) != 404 {
		t.Fatal("cross tenant send", e)
	}
	if _, e = s.ExplainSubject(ctx, a, other.ID, "message", Target{"agent", w.ID}, "", ""); ErrorStatus(e) != 404 {
		t.Fatal("cross tenant subject", e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET expires_at=now()-interval '1 second' WHERE id=$1`, w.CredentialID); e != nil {
		t.Fatal(e)
	}
	if e = s.tx(ctx, w, func(_ pgx.Tx) error { return nil }); ErrorStatus(e) != 403 {
		t.Fatal("expired worker wrote", e)
	}
}

func TestAmountAndResourceLimitsAreRecheckedAtExecution(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "billing-agent")
	p := testPerson(t, s, a, "finance-manager")
	ceiling := int64(10000)
	role := grant(t, s, a, p, []Rule{{Action: "view", TargetKind: "agent", TargetID: w.ID}, {Action: "decide", TargetKind: "agent", TargetID: w.ID, ActionName: "refund", Conditions: &RuleConditions{Resource: "invoice/1", MaxAmountMinor: &ceiling, Currency: "USD"}}})
	ask := func(amount int64, key string) Request {
		t.Helper()
		q, e := s.Ask(ctx, w, AskInput{Target: Target{"person", p.ID}, Kind: "approval", Title: "Refund?", Action: &Action{Name: "refund", ExecutorID: w.ID, Resource: "invoice/1", ResourceVersion: "1", OperationKey: key, Arguments: map[string]any{"amount_minor": amount, "currency": "USD"}}}, key)
		if e != nil {
			t.Fatal(e)
		}
		return q
	}
	if _, err := s.Ask(ctx, w, AskInput{Target: Target{"person", p.ID}, Kind: "approval", Title: "Refund?", Action: &Action{Name: "refund", ExecutorID: w.ID, Resource: "invoice/1", ResourceVersion: "1", OperationKey: "over-limit", Arguments: map[string]any{"amount_minor": int64(10001), "currency": "USD"}}}, "over-limit"); ErrorStatus(err) != 403 {
		t.Fatal("unusable approval accepted", err)
	}
	q := ask(9900, "within-limit")
	if _, e := s.Respond(ctx, p, q.ID, Response{Outcome: "approved", Version: 1}); e != nil {
		t.Fatal(e)
	}
	current, e := s.ExplainSubject(ctx, a, p.ID, "decide", Target{"agent", w.ID}, "", "refund", q.Action)
	if e != nil || current["allowed"] != true {
		t.Fatal(current, e)
	}
	lower := int64(5000)
	role.Rules[1].Conditions.MaxAmountMinor = &lower
	if _, e = s.SaveRole(ctx, a, role); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Redeem(ctx, w, q.ID, *q.Action); ErrorStatus(e) != 403 {
		t.Fatal("old limit survived policy update", e)
	}
}

func TestPreviewDoesNotRevealHiddenRecipientMetadata(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	w := testWorker(t, s, a, "preview-source")
	p := testPerson(t, s, a, "hidden-person")
	g, e := s.SaveGroup(ctx, a, Group{Name: "Private team", Members: []string{p.ID}})
	if e != nil {
		t.Fatal(e)
	}
	r, e := s.Roles(ctx, a)
	if e != nil {
		t.Fatal(e)
	}
	for _, role := range r {
		if role.Name == "preview-source role" {
			role.Rules = []Rule{{Action: "message", TargetKind: "group", TargetID: g.ID}}
			if _, e = s.SaveRole(ctx, a, role); e != nil {
				t.Fatal(e)
			}
		}
	}
	preview, e := s.Preview(ctx, w, Target{"group", g.ID}, "message")
	if e != nil || preview["count"] != 1 || len(preview["recipients"].([]Principal)) != 0 {
		t.Fatal(preview, e)
	}
}

func TestGroupTargetGrantAllowsIndividualMembersAndRevokesPendingContact(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	sender := testPerson(t, s, a, "group-sender")
	receiver := testPerson(t, s, a, "group-receiver")
	g, e := s.SaveGroup(ctx, a, Group{Name: "Finance", Members: []string{receiver.ID}})
	if e != nil {
		t.Fatal(e)
	}
	grant(t, s, a, sender, []Rule{{Action: "view", TargetKind: "group", TargetID: g.ID}, {Action: "message", TargetKind: "group", TargetID: g.ID}, {Action: "ask", TargetKind: "group", TargetID: g.ID}})
	grant(t, s, a, receiver, []Rule{{Action: "view", TargetKind: "person", TargetID: sender.ID}, {Action: "message", TargetKind: "person", TargetID: sender.ID}})
	if _, e = s.Send(ctx, sender, SendInput{Target: Target{"person", receiver.ID}, Body: "Private contact through group grant"}, "group-individual"); e != nil {
		t.Fatal(e)
	}
	q, e := s.Ask(ctx, sender, AskInput{Target: Target{"person", receiver.ID}, Title: "Can you review?"}, "pending-contact")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveGroup(ctx, a, Group{ID: g.ID, Name: g.Name, Members: []string{}}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, sender, SendInput{Target: Target{"person", receiver.ID}, Body: "No longer permitted"}, "after-removal"); ErrorStatus(e) != 403 {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, receiver, q.ID, Response{Outcome: "answered", Answer: "Yes", Version: 1}); ErrorStatus(e) != 403 {
		t.Fatal("pending request outlived contact grant", e)
	}
}
