package engine

import (
	"context"
	"testing"
	"time"
)

func TestIdleInboxAvoidsMutationLockButRechecksHoldAndCredentials(t *testing.T) {
	s, owner := testStore(t)
	ctx := context.Background()
	worker := testWorker(t, s, owner, "idle")
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT id FROM scopes WHERE id=$1 FOR UPDATE`, owner.Scope); e != nil {
		t.Fatal(e)
	}
	bounded, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	jobs, e := s.Inbox(bounded, worker)
	if e != nil || len(jobs) != 0 {
		t.Fatal("empty poll waited for mutation lock", jobs, e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE instances SET last_seen=now()-interval '20 seconds' WHERE id=$1`, worker.InstanceID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Inbox(ctx, worker); e != nil {
		t.Fatal(e)
	}
	var fresh bool
	if e = s.Pool.QueryRow(ctx, `SELECT last_seen>now()-interval '2 seconds' FROM instances WHERE id=$1`, worker.InstanceID).Scan(&fresh); e != nil || !fresh {
		t.Fatal("presence not refreshed", fresh, e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE scopes SET recovery_hold=true WHERE id=$1`, owner.Scope); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Inbox(ctx, worker); ErrorStatus(e) != 503 {
		t.Fatal("idle hold bypass", e)
	}
	if _, e = s.Pool.Exec(ctx, `UPDATE scopes SET recovery_hold=false WHERE id=$1;`, owner.Scope); e != nil {
		t.Fatal(e)
	}
	if e = s.Revoke(ctx, owner, worker.CredentialID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Inbox(ctx, worker); ErrorStatus(e) != 403 {
		t.Fatal("idle revoked credential", e)
	}
}
