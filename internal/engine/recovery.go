package engine

import (
	"context"
	"time"
)

type RestoredWorkspace struct {
	Scope   string `json:"scope"`
	Version int64  `json:"version"`
}

// HoldRestoredCell is an offline operator primitive. Commit the hold and revoke
// every restored credential before consulting an external registry. Unknown
// workspaces must remain inaccessible even when reconciliation cannot proceed.
func (s *Store) HoldRestoredCell(ctx context.Context) ([]RestoredWorkspace, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id,control_version FROM scopes ORDER BY id FOR UPDATE`)
	if err != nil {
		return nil, err
	}
	out := []RestoredWorkspace{}
	for rows.Next() {
		var w RestoredWorkspace
		if err = rows.Scan(&w.Scope, &w.Version); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, w)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, q := range []string{
		`UPDATE scopes SET recovery_hold=true,automatic_answers=false,overseer_enabled=false,assistance_version=assistance_version+1`,
		`UPDATE credentials SET revoked=true WHERE NOT revoked`,
		`UPDATE answer_match_jobs SET state='cancelled' WHERE state IN ('queued','leased')`,
		`UPDATE deliveries SET state='unknown',last_error='Database restored; reconcile externally before replay' WHERE state IN ('queued','leased','waiting')`,
		`UPDATE execution_intents SET state='unknown',updated_at=now() WHERE state IN ('authorized','running')`,
		`UPDATE requests SET state='superseded',resolved_at=now() WHERE kind IN ('approval','task') AND state IN ('open','resolved')`,
	} {
		if _, err = tx.Exec(ctx, q); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

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
		`UPDATE scopes SET recovery_hold=true,automatic_answers=false,overseer_enabled=false,assistance_version=assistance_version+1 WHERE id=$1`,
		`UPDATE answer_match_jobs SET state='cancelled' WHERE scope=$1 AND state IN ('queued','leased')`,
		`UPDATE credentials SET revoked=true WHERE scope=$1`,
		`UPDATE deliveries SET state='unknown',last_error='Database restored; reconcile externally before replay' WHERE scope=$1 AND state IN ('queued','leased','waiting')`,
		`UPDATE execution_intents SET state='unknown',updated_at=now() WHERE scope=$1 AND state IN ('authorized','running')`,
		`UPDATE requests SET state='superseded',resolved_at=now() WHERE scope=$1 AND kind IN ('approval','task') AND state IN ('open','resolved')`,
		`UPDATE principals SET disabled=false WHERE scope=$1 AND kind='person' AND external_id='owner'`,
	}
	for _, q := range statements {
		if _, e = tx.Exec(ctx, q, scope); e != nil {
			return e
		}
	}
	// A lost acknowledgement must be replayable with the same privately saved
	// recovery key. Never revive a matching digest owned by a different identity.
	tag, e := tx.Exec(ctx, `INSERT INTO credentials(id,scope,principal_id,digest,kind,prefix) VALUES($1,$2,$3,$4,'admin','recovery') ON CONFLICT(digest) DO UPDATE SET revoked=false WHERE credentials.scope=excluded.scope AND credentials.principal_id=excluded.principal_id AND credentials.kind='admin' AND credentials.prefix='recovery'`, ID("key"), scope, owner, Digest(newOwnerToken))
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return Conflict("Recovery credential belongs to another identity.")
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
