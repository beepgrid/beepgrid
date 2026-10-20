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
	role := grant(t, s, a, p, []Rule{{"view", "agent", w.ID, "", ""}, {"decide", "agent", w.ID, c.ID, "deploy"}})
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
