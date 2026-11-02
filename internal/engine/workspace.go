package engine

import (
	"context"
	"time"
)

// ApplyWorkspaceDeadline projects a versioned access grant without changing
// operator suspension, recovery holds or their independent control revision.
func (s *Store) ApplyWorkspaceDeadline(ctx context.Context, scope string, version int64, until time.Time) error {
	if version < 1 {
		return Bad("An explicit access version is required.")
	}
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var current int64
	var previous *time.Time
	var erasure string
	if e = tx.QueryRow(ctx, `SELECT access_version,access_until,erasure_operation FROM scopes WHERE id=$1 FOR UPDATE`, scope).Scan(&current, &previous, &erasure); e != nil {
		return normalize(e)
	}
	if erasure != "" {
		return Conflict("This workspace is reserved for erasure and cannot receive an access grant.")
	}
	var next *time.Time
	if !until.IsZero() {
		value := until.UTC().Truncate(time.Microsecond)
		next = &value
	}
	same := previous == nil && next == nil || previous != nil && next != nil && previous.Equal(*next)
	if version < current || version == current && !same {
		return Conflict("A newer or conflicting access grant is already applied.")
	}
	if version == current {
		return nil
	}
	if _, e = tx.Exec(ctx, `UPDATE scopes SET access_until=$2,access_version=$3 WHERE id=$1`, scope, next, version); e != nil {
		return e
	}
	if e = event(ctx, tx, Actor{Scope: scope}, "workspace.access", "", "", map[string]any{"version": version, "access_until": next}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

// ApplyWorkspaceState fences delayed operator requests independently of customer
// credentials. It must only be exposed behind authenticated management access.
func (s *Store) ApplyWorkspaceState(ctx context.Context, scope string, version int64, suspended bool) error {
	return s.applyWorkspaceControl(ctx, scope, version, suspended, nil, false)
}

// ApplyWorkspaceAccess atomically fences security state and an optional access
// deadline. Zero means unlimited. Expiry is enforced by the database even while
// the management service is offline; it never deletes accepted work.
func (s *Store) ApplyWorkspaceAccess(ctx context.Context, scope string, version int64, suspended bool, until time.Time) error {
	return s.applyWorkspaceControl(ctx, scope, version, suspended, &until, false)
}

// ApplyRestoredWorkspaceAccess reconciles an authoritative registry snapshot
// while the restored workspace is held. Equal versions may repair a snapshot's
// access projection; lower versions and erasure tombstones remain forbidden.
func (s *Store) ApplyRestoredWorkspaceAccess(ctx context.Context, scope string, version int64, suspended bool, until time.Time) error {
	if version < 1 {
		return Bad("An explicit registry version is required.")
	}
	return s.applyWorkspaceControl(ctx, scope, version, suspended, &until, true)
}

func (s *Store) applyWorkspaceControl(ctx context.Context, scope string, version int64, suspended bool, deadline *time.Time, restore bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var current int64
	var old, held bool
	var oldDeadline *time.Time
	var erasure string
	if err = tx.QueryRow(ctx, `SELECT control_version,suspended,access_until,erasure_operation,recovery_hold FROM scopes WHERE id=$1 FOR UPDATE`, scope).Scan(&current, &old, &oldDeadline, &erasure, &held); err != nil {
		return normalize(err)
	}
	if restore && !held {
		return Conflict("Restore reconciliation requires an active recovery hold.")
	}
	if erasure != "" {
		return Conflict("This workspace is reserved for erasure and cannot be reactivated.")
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
	if version < current || (version == current && !restore && (suspended != old || !sameDeadline)) {
		return Conflict("A newer workspace state is already applied.")
	}
	if version == current && (suspended == old && sameDeadline) {
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
