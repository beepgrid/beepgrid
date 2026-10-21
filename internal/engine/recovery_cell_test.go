package engine

import (
	"context"
	"testing"
	"time"
)

func TestRestoredAccessRequiresHoldAndCannotLowerVersionsOrEraseMarkers(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	if e := s.ApplyWorkspaceAccess(ctx, a.Scope, 3, false, time.Time{}); e != nil {
		t.Fatal(e)
	}
	if e := s.ApplyRestoredWorkspaceAccess(ctx, a.Scope, 3, true, time.Unix(1, 0)); ErrorStatus(e) != 409 {
		t.Fatal("restore bypassed missing hold", e)
	}
	if _, e := s.HoldRestoredCell(ctx); e != nil {
		t.Fatal(e)
	}
	// Ordinary control commands keep their existing equal-version conflict rule.
	if e := s.ApplyWorkspaceAccess(ctx, a.Scope, 3, true, time.Unix(1, 0)); ErrorStatus(e) != 409 {
		t.Fatal("ordinary fencing weakened", e)
	}
	if e := s.ApplyRestoredWorkspaceAccess(ctx, a.Scope, 3, true, time.Unix(1, 0)); e != nil {
		t.Fatal("held projection repair", e)
	}
	if e := s.ApplyRestoredWorkspaceAccess(ctx, a.Scope, 2, false, time.Time{}); ErrorStatus(e) != 409 {
		t.Fatal("stale registry repaired newer state", e)
	}
	if e := s.ReserveWorkspaceErasure(ctx, a.Scope, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 4, time.Unix(1, 0)); e != nil {
		t.Fatal(e)
	}
	if e := s.ApplyRestoredWorkspaceAccess(ctx, a.Scope, 10, false, time.Time{}); ErrorStatus(e) != 409 {
		t.Fatal("restore revived tombstone", e)
	}
}
func TestRecoveryKeyReplayCannotTakeAnotherIdentityDigest(t *testing.T) {
	s, a := testStore(t)
	ctx := context.Background()
	oldToken := Secret()
	other, e := s.Bootstrap(ctx, "other/recovery", "Other", "recovery", oldToken)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.HoldRestoredCell(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.ReconcileRestored(ctx, other.Scope, oldToken); ErrorStatus(e) != 409 {
		t.Fatal("bootstrap credential revived as a recovery key", e)
	}
	key := Secret()
	if e = s.ReconcileRestored(ctx, other.Scope, key); e != nil {
		t.Fatal(e)
	}
	if e = s.ReconcileRestored(ctx, a.Scope, key); ErrorStatus(e) != 409 {
		t.Fatal("recovery key crossed identities", e)
	}
	actor, e := s.Authenticate(ctx, key)
	if e != nil || actor.Scope != other.Scope {
		t.Fatal("conflict revoked another recovery owner", e)
	}
	if e = s.ReconcileRestored(ctx, other.Scope, key); e != nil {
		t.Fatal("matching lost-ack replay rejected", e)
	}
	var n int
	if e = s.Pool.QueryRow(ctx, "SELECT count(*) FROM credentials WHERE scope=$1 AND NOT revoked", other.Scope).Scan(&n); e != nil || n != 1 {
		t.Fatal("replay duplicated authority", n, e)
	}
}
