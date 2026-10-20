package engine

import (
	"context"
	"time"
)

// ReconcileRestored is an offline operator action before accepting traffic on a restored database.
// It freezes uncertain work and rotates every credential, including admin credentials rolled back by a backup.
func (s *Store) ReconcileRestored(ctx context.Context, scope, newOwnerToken string) error {
	if len(newOwnerToken) < 32 {
		return Bad("A new recovery owner token is required.")
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var owner string
	if e = tx.QueryRow(ctx, `SELECT p.id FROM principals p JOIN scopes s ON s.id=p.scope WHERE p.scope=$1 AND p.kind='person' AND p.external_id='owner' FOR UPDATE OF s`, scope).Scan(&owner); e != nil {
		return normalize(e)
	}
	statements := []string{
		`UPDATE scopes SET recovery_hold=true WHERE id=$1`,
		`UPDATE credentials SET revoked=true WHERE scope=$1`,
		`UPDATE deliveries SET state='unknown',last_error='Database restored; reconcile externally before replay' WHERE scope=$1 AND state IN ('queued','leased')`,
		`UPDATE execution_intents SET state='unknown',updated_at=now() WHERE scope=$1 AND state IN ('authorized','running')`,
		`UPDATE requests SET state='superseded',resolved_at=now() WHERE scope=$1 AND kind IN ('approval','task') AND state IN ('open','resolved')`,
		`UPDATE principals SET disabled=false WHERE scope=$1 AND kind='person' AND external_id='owner'`,
	}
	for _, q := range statements {
		if _, e = tx.Exec(ctx, q, scope); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO credentials(id,scope,principal_id,digest,kind) VALUES($1,$2,$3,$4,'admin')`, ID("key"), scope, owner, Digest(newOwnerToken)); e != nil {
		return e
	}
	if e = event(ctx, tx, Actor{Principal: Principal{ID: owner}, Scope: scope}, "recovery.hold", "", owner, map[string]any{"at": time.Now(), "credentials_rotated": true, "effects_require_reconciliation": true}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s *Store) ResumeAfterReconciliation(ctx context.Context, scope string) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT id FROM scopes WHERE id=$1 FOR UPDATE`, scope); e != nil {
		return e
	}
	tag, e := tx.Exec(ctx, `UPDATE scopes SET recovery_hold=false WHERE id=$1`, scope)
	if e != nil {
		return e
	}
	if tag.RowsAffected() == 0 {
		return Missing()
	}
	if e = event(ctx, tx, Actor{Scope: scope}, "recovery.resumed", "", "", map[string]any{"at": time.Now(), "unknown_work_not_replayed": true}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
