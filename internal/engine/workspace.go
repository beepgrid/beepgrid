package engine

import (
	"context"
	"time"
)

// ApplyWorkspaceState fences delayed operator requests independently of customer
// credentials. It must only be exposed behind authenticated management access.
func (s *Store) ApplyWorkspaceState(ctx context.Context, scope string, version int64, suspended bool) error {
	return s.applyWorkspaceControl(ctx, scope, version, suspended, nil)
}

// ApplyWorkspaceAccess atomically fences security state and an optional access
// deadline. Zero means unlimited. Expiry is enforced by the database even while
// the management service is offline; it never deletes accepted work.
func (s *Store) ApplyWorkspaceAccess(ctx context.Context, scope string, version int64, suspended bool, until time.Time) error {
	return s.applyWorkspaceControl(ctx, scope, version, suspended, &until)
}

func (s *Store) applyWorkspaceControl(ctx context.Context, scope string, version int64, suspended bool, deadline *time.Time) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var current int64
	var old bool
	var oldDeadline *time.Time
	if err = tx.QueryRow(ctx, `SELECT control_version,suspended,access_until FROM scopes WHERE id=$1 FOR UPDATE`, scope).Scan(&current, &old, &oldDeadline); err != nil {
		return normalize(err)
	}
	nextDeadline := oldDeadline
	if deadline != nil {
		nextDeadline = nil
		if !deadline.IsZero() {
			normalized := deadline.UTC().Truncate(time.Microsecond)
			nextDeadline = &normalized
		}
	}
	sameDeadline := oldDeadline == nil && nextDeadline == nil || oldDeadline != nil && nextDeadline != nil && oldDeadline.Equal(*nextDeadline)
	if version == 0 {
		version = current + 1
	}
	if version < current || (version == current && (suspended != old || !sameDeadline)) {
		return Conflict("A newer workspace state is already applied.")
	}
	if version == current {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE scopes SET suspended=$2,control_version=$3,access_until=$4 WHERE id=$1`, scope, suspended, version, nextDeadline); err != nil {
		return err
	}
	if err = event(ctx, tx, Actor{Scope: scope}, "workspace.state", "", "", map[string]any{"suspended": suspended, "version": version, "access_until": nextDeadline}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// BootstrapCredentialMatches permits idempotent operator replay while a
// workspace is suspended. It never makes that credential usable by a customer.
func (s *Store) BootstrapCredentialMatches(ctx context.Context, scope, company, token string) bool {
	var exists bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials c JOIN scopes s ON s.id=c.scope JOIN principals p ON p.id=c.principal_id AND p.scope=c.scope WHERE c.scope=$1 AND c.digest=$2 AND c.kind='admin' AND NOT c.revoked AND NOT p.disabled AND s.company_name=$3)`, scope, Digest(token), company).Scan(&exists)
	return err == nil && exists
}
