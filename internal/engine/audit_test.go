package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAuditExpiredOriginCannotDeliverQueuedContent(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	sender := testWorker(t, s, owner, "expired-origin")
	receiver := testWorker(t, s, owner, "receiver")
	if _, e := s.Send(ctx, sender, SendInput{Target: Target{"agent", receiver.ID}, Body: "queued before expiry"}, "expiry"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Pool.Exec(ctx, `UPDATE credentials SET expires_at=now()-interval '1 second' WHERE id=$1`, sender.CredentialID); e != nil {
		t.Fatal(e)
	}
	jobs, e := s.Inbox(ctx, receiver)
	if e != nil || len(jobs) != 0 {
		t.Fatal("expired origin exposed queued content", jobs, e)
	}
}
func TestAuditExpiredAdminCannotAuthorizeRedemption(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "executor")
	action := Action{Name: "fixture", ExecutorID: worker.ID, Resource: "fixture", ResourceVersion: "1", OperationKey: "once"}
	q, e := s.Ask(ctx, owner, AskInput{Kind: "approval", Target: Target{"person", owner.ID}, Title: "Fixture?", Action: &action}, "approval")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, owner, q.ID, Response{Outcome: "approved", Version: 1}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE credentials SET expires_at=now()-interval '1 second' WHERE id=$1`, owner.CredentialID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Redeem(ctx, worker, q.ID, action); ErrorStatus(e) != 403 {
		t.Fatal("expired admin retained authority", e)
	}
}
func TestAuditRemovedApproverCannotRedeemOldGroupApproval(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "executor")
	person := testPerson(t, s, owner, "approver")
	grant(t, s, owner, person, []Rule{{Action: "view", TargetKind: "agent", TargetID: worker.ID}, {Action: "decide", TargetKind: "agent", TargetID: worker.ID}})
	group, e := s.SaveGroup(ctx, owner, Group{Name: "Approvers", Members: []string{person.ID}})
	if e != nil {
		t.Fatal(e)
	}
	action := Action{Name: "fixture", ExecutorID: worker.ID, Resource: "fixture", ResourceVersion: "1", OperationKey: "group-once"}
	q, e := s.Ask(ctx, worker, AskInput{Kind: "approval", Target: Target{"group", group.ID}, Title: "Fixture?", Action: &action}, "group")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, person, q.ID, Response{Outcome: "approved", Version: 1}); e != nil {
		t.Fatal(e)
	}
	group.Members = []string{}
	if _, e = s.SaveGroup(ctx, owner, group); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Redeem(ctx, worker, q.ID, action); ErrorStatus(e) != 403 {
		t.Fatal("removed audience member authorized execution", e)
	}
}
func TestAuditHeartbeatRespectsRecoveryHoldAndRevocation(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "heartbeat")
	if _, e := s.Pool.Exec(ctx, `UPDATE scopes SET recovery_hold=true WHERE id=$1`, owner.Scope); e != nil {
		t.Fatal(e)
	}
	if e := s.Heartbeat(ctx, worker, map[string]any{"step": "blocked"}); ErrorStatus(e) != 503 {
		t.Fatal("recovery hold bypassed", e)
	}
	if _, e := s.Pool.Exec(ctx, `UPDATE scopes SET recovery_hold=false WHERE id=$1`, owner.Scope); e != nil {
		t.Fatal(e)
	}
	if e := s.Revoke(ctx, owner, worker.CredentialID); e != nil {
		t.Fatal(e)
	}
	if e := s.Heartbeat(ctx, worker, nil); ErrorStatus(e) != 403 {
		t.Fatal("revoked actor mutated progress", e)
	}
}
func TestAuditBootstrapRequiresOwnerOfRequestedScope(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	token := Secret()
	other, e := s.Bootstrap(ctx, "other/dev", "Other", "dev", token)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Bootstrap(ctx, owner.Scope, "Acme", "dev", token); ErrorStatus(e) != 403 {
		t.Fatal("cross-workspace bootstrap accepted", other, e)
	}
	service, e := s.CreateService(ctx, owner, "app", "App")
	if e != nil {
		t.Fatal(e)
	}
	credential, e := s.NewCredential(ctx, owner, service.ID, "api", "")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Bootstrap(ctx, owner.Scope, "Acme", "dev", credential["token"].(string)); ErrorStatus(e) != 403 {
		t.Fatal("non-owner startup credential accepted", e)
	}
}

func TestAuditProposerContactRevocationBlocksRedemption(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	session, e := s.Enroll(ctx, owner, EnrollInput{Agent: "contact-limited", Name: "Contact limited"})
	if e != nil {
		t.Fatal(e)
	}
	worker, e := s.Authenticate(ctx, session.Token)
	if e != nil {
		t.Fatal(e)
	}
	role := grant(t, s, owner, worker, []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}, {Action: "ask", TargetKind: "person", TargetID: owner.ID}, {Action: "ask", TargetKind: "agent", TargetID: "$self"}})
	action := Action{Name: "fixture", ExecutorID: worker.ID, Resource: "fixture", ResourceVersion: "1", OperationKey: "contact-once"}
	q, e := s.Ask(ctx, worker, AskInput{Kind: "approval", Target: Target{"person", owner.ID}, Title: "Fixture?", Action: &action}, "contact")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Respond(ctx, owner, q.ID, Response{Outcome: "approved", Version: 1}); e != nil {
		t.Fatal(e)
	}
	role.Rules = []Rule{{Action: "view", TargetKind: "person", TargetID: owner.ID}, {Action: "ask", TargetKind: "agent", TargetID: "$self"}}
	if _, e = s.SaveRole(ctx, owner, role); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Redeem(ctx, worker, q.ID, action); ErrorStatus(e) != 403 {
		t.Fatal("revoked contact grant still authorized execution", e)
	}
}

// Dispatch must acquire the workspace lock before delivery rows, matching every
// cancellation/decision transaction. Otherwise those two paths can deadlock.
func TestAuditDispatchSkipsBusyWorkspaceWithoutHoldingDelivery(t *testing.T) {
	s, owner := testStore(t)
	s.AllowLocal = true
	ctx := context.Background()
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202) }))
	defer receiver.Close()
	c, e := s.SaveConnection(ctx, owner, Connection{Name: "Lock order", Kind: "webhook", Config: map[string]string{"url": receiver.URL}, Secret: "test-signing-secret", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.SaveDestination(ctx, owner, Destination{ConnectionID: c.ID, PrincipalID: owner.ID, Address: "audit"}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Send(ctx, owner, SendInput{Target: Target{"person", owner.ID}, Body: "lock order"}, "lock-order"); e != nil {
		t.Fatal(e)
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT id FROM scopes WHERE id=$1 FOR UPDATE`, owner.Scope); e != nil {
		t.Fatal(e)
	}
	bounded, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if e = s.DispatchOnce(bounded, "http://localhost"); e != nil {
		t.Fatal("dispatcher blocked behind workspace mutation", e)
	}
	// A concurrent cancelling transaction can still lock the delivery rows.
	if _, e = tx.Exec(ctx, `SELECT id FROM deliveries WHERE scope=$1 FOR UPDATE NOWAIT`, owner.Scope); e != nil {
		t.Fatal("delivery lock inversion", e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.DispatchOnce(ctx, "http://localhost"); e != nil {
		t.Fatal(e)
	}
	var state string
	if e = s.Pool.QueryRow(ctx, `SELECT state FROM deliveries WHERE connection_id=$1`, c.ID).Scan(&state); e != nil || state != "provider_accepted" {
		t.Fatal(state, e)
	}
}
