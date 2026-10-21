package engine

import (
	"context"
)

// ApplyWorkspaceState fences delayed operator requests independently of customer
// credentials. It must only be exposed behind authenticated management access.
func (s *Store) ApplyWorkspaceState(ctx context.Context, scope string, version int64, suspended bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var current int64
	var old bool
	if err = tx.QueryRow(ctx, `SELECT control_version,suspended FROM scopes WHERE id=$1 FOR UPDATE`, scope).Scan(&current, &old); err != nil {
		return normalize(err)
	}
	if version == 0 {
		version = current + 1
	}
	if version < current || (version == current && suspended != old) {
		return Conflict("A newer workspace state is already applied.")
	}
	if version == current {
		return nil
	}
	if _, err = tx.Exec(ctx, `UPDATE scopes SET suspended=$2,control_version=$3 WHERE id=$1`, scope, suspended, version); err != nil {
		return err
	}
	if err = event(ctx, tx, Actor{Scope: scope}, "workspace.state", "", "", map[string]any{"suspended": suspended, "version": version}); err != nil {
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
