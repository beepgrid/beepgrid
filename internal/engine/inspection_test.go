package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestTraceKeepsPrivateAudienceAndExecutionSeparate(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	p := testPerson(t, s, owner, "first")
	q := testPerson(t, s, owner, "second")
	agent := testWorker(t, s, owner, "worker")
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Audience", Members: []string{p.ID, q.ID}})
	if e != nil {
		t.Fatal(e)
	}
	for _, x := range []Actor{p, q} {
		grant(t, s, owner, x, []Rule{{Action: "view", TargetKind: "agent", TargetID: agent.ID}, {Action: "message", TargetKind: "agent", TargetID: agent.ID}})
	}
	r, e := s.Ask(ctx, agent, AskInput{Target: Target{"group", group.ID}, Title: "Input?"}, "trace")
	if e != nil {
		t.Fatal(e)
	}
	trace, e := s.RequestTrace(ctx, p, r.ID, "")
	if e != nil || len(trace.Deliveries.Items) != 1 || len(trace.Executions.Items) != 0 {
		t.Fatal(trace, e)
	}
	delivery := trace.Deliveries.Items[0].(DeliveryEvidence)
	if delivery.RecipientID != p.ID || delivery.CreatedAt.IsZero() || delivery.Payload["_policy"] != nil {
		t.Fatal(delivery)
	}
	requester, e := s.RequestTrace(ctx, agent, r.ID, "")
	if e != nil || len(requester.Deliveries.Items) != 2 {
		t.Fatal(requester, e)
	}
	if trace.Request.ConversationID == requester.Request.ConversationID {
		t.Fatal("private audience projection was lost")
	}
	if _, e = s.Respond(ctx, p, r.ID, Response{Version: r.Version, Outcome: "answered", Answer: "42"}); e != nil {
		t.Fatal(e)
	}
	trace, e = s.RequestTrace(ctx, p, r.ID, "")
	if e != nil || len(trace.Deliveries.Items) != 1 || trace.Request.State != "resolved" {
		t.Fatal(trace, e)
	}
	outsider := testPerson(t, s, owner, "outside")
	if _, e = s.RequestTrace(ctx, outsider, r.ID, ""); ErrorStatus(e) != 404 {
		t.Fatal(e)
	}
}
func TestConnectionDiagnosticsAreNonSendingAndTestRequiresContact(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	p := testPerson(t, s, owner, "person")
	lead := testPerson(t, s, owner, "lead")
	conn, e := s.SaveConnection(ctx, owner, Connection{Name: "SMTP", Kind: "email", Enabled: true, Config: map[string]string{"smtp_addr": "localhost:1025", "from": "from@example.com"}})
	if e != nil {
		t.Fatal(e)
	}
	dest, e := s.SaveDestination(ctx, owner, Destination{ConnectionID: conn.ID, PrincipalID: p.ID, Address: "to@example.com"})
	if e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "connection", TargetID: conn.ID}, {Action: "manage", TargetKind: "destination", TargetID: p.ID}, {Action: "manage", TargetKind: "person", TargetID: p.ID}})
	diag, e := s.DiagnoseConnection(ctx, lead, conn.ID)
	if e != nil || !diag.ConfigurationValid || diag.Sends || diag.ProviderAcceptance != "not_tested" {
		t.Fatal(diag, e)
	}
	var n int
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE scope=$1`, owner.Scope).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if _, e = s.TestConnection(ctx, lead, conn.ID, dest.ID, "test-denied"); ErrorStatus(e) != 403 {
		t.Fatal("management granted contact", e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "message", TargetKind: "person", TargetID: p.ID, ConnectionID: conn.ID}})
	delivery, e := s.TestConnection(ctx, lead, conn.ID, dest.ID, "test-allowed")
	if e != nil || delivery.State != "queued" || delivery.Payload["_policy"] != nil {
		t.Fatal(delivery, e)
	}
	again, e := s.TestConnection(ctx, lead, conn.ID, dest.ID, "test-allowed")
	if e != nil || again.ID != delivery.ID {
		t.Fatal(again, e)
	}
	if e = s.Pool.QueryRow(ctx, `SELECT count(*) FROM deliveries WHERE scope=$1`, owner.Scope).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if _, e = s.DiagnoseConnection(ctx, p, conn.ID); ErrorStatus(e) != 404 {
		t.Fatal("recipient got manager diagnostic", e)
	}
	conn.Enabled = false
	if _, e = s.SaveConnection(ctx, owner, conn); e != nil {
		t.Fatal(e)
	}
	if _, e = s.TestConnection(ctx, lead, conn.ID, dest.ID, "test-disabled"); ErrorStatus(e) != 409 {
		t.Fatal(e)
	}
}
func TestDelegatedDeliveryRetryRequiresReadManageAndCurrentPolicy(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	p := testPerson(t, s, owner, "recipient")
	lead := testPerson(t, s, owner, "lead")
	r, e := s.Ask(ctx, owner, AskInput{Target: Target{"person", p.ID}, Title: "Request"}, "retry")
	if e != nil {
		t.Fatal(e)
	}
	var id string
	if e = s.Pool.QueryRow(ctx, `SELECT id FROM deliveries WHERE scope=$1 AND operation_id=$2`, owner.Scope, r.ID).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE deliveries SET state='failed' WHERE scope=$1 AND id=$2`, owner.Scope, id); e != nil {
		t.Fatal(e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "manage", TargetKind: "delivery", TargetID: id}, {Action: "view", TargetKind: "person", TargetID: p.ID}})
	if e = s.RetryDelivery(ctx, lead, id); ErrorStatus(e) != 403 {
		t.Fatal(e)
	}
	grant(t, s, owner, lead, []Rule{{Action: "view", TargetKind: "delivery", TargetID: id}})
	if e = s.RetryDelivery(ctx, lead, id); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE deliveries SET state='failed',expires_at=$3 WHERE scope=$1 AND id=$2`, owner.Scope, id, time.Now().Add(-time.Hour)); e != nil {
		t.Fatal(e)
	}
	if e = s.RetryDelivery(ctx, lead, id); ErrorStatus(e) != 403 {
		t.Fatal(e)
	}
}

func TestExplicitConnectionTestUsesRealDispatcherWithoutFallback(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	var first, other atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { first.Add(1); w.WriteHeader(400) }))
	defer primary.Close()
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { other.Add(1); w.WriteHeader(202) }))
	defer fallback.Close()
	c, e := s.SaveConnection(ctx, owner, Connection{Name: "Primary", Kind: "webhook", Enabled: true, Config: map[string]string{"url": primary.URL}, Secret: "long-fixture-signing-secret"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: owner.ID, Address: "one"})
	if e != nil {
		t.Fatal(e)
	}
	f, e := s.SaveConnection(ctx, owner, Connection{Name: "Fallback", Kind: "webhook", Enabled: true, Config: map[string]string{"url": fallback.URL}, Secret: "long-fixture-signing-secret"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: f.ID, PrincipalID: owner.ID, Address: "two", Mode: "fallback"}); e != nil {
		t.Fatal(e)
	}
	test, e := s.TestConnection(ctx, owner, c.ID, d.ID, "diagnostic-only")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	if first.Load() != 1 || other.Load() != 0 {
		t.Fatal(first.Load(), other.Load())
	}
	item, e := s.queryRecord(ctx, owner, "deliveries", test.ID)
	if e != nil {
		t.Fatal(e)
	}
	evidence := item.(DeliveryEvidence)
	if evidence.State != "failed" || len(evidence.Attempts.Items) != 1 || !evidence.ContentVisible {
		t.Fatal(evidence)
	}
}

func TestSMTPRelayCredentialReadinessIsTruthful(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	conn, e := s.SaveConnection(ctx, owner, Connection{Name: "Relay", Kind: "email", Config: map[string]string{"from": "sender@example.com", "smtp_addr": "localhost:1025"}})
	if e != nil || conn.SecretConfigured {
		t.Fatal(conn, e)
	}
	list, e := s.Connections(ctx, owner)
	if e != nil || len(list) != 1 || list[0].SecretConfigured {
		t.Fatal(list, e)
	}
	conn.Config["username"] = "sender"
	conn.Secret = "fixture-password"
	conn, e = s.SaveConnection(ctx, owner, conn)
	if e != nil || !conn.SecretConfigured || conn.Secret != "" {
		t.Fatal(conn, e)
	}
	list, e = s.Connections(ctx, owner)
	if e != nil || !list[0].SecretConfigured {
		t.Fatal(list, e)
	}
}
