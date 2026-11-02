package engine

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Permissions are an upper bound, never grants. nil preserves existing identity permissions.
type CredentialOptions struct {
	Name        string     `json:"name,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Permissions []string   `json:"permissions,omitempty"`
}
type CredentialEdit struct {
	ClearPermissions bool       `json:"clear_permissions,omitempty"`
	Version          int64      `json:"version"`
	Name             *string    `json:"name,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Permissions      *[]string  `json:"permissions,omitempty"`
}
type CredentialRotation struct {
	Version        int64 `json:"version"`
	OverlapSeconds int   `json:"overlap_seconds"`
}

const credentialFields = `id,principal_id,kind,prefix,expires_at,revoked,name,token_hint,created_at,last_used_at,permissions,version,renewable_until,previous_until,COALESCE(parent_credential_id,''),(SELECT name FROM principals p WHERE p.scope=credentials.scope AND p.id=credentials.principal_id)`

func scanCredential(row pgx.Row) (Credential, error) {
	var c Credential
	e := row.Scan(&c.ID, &c.PrincipalID, &c.Kind, &c.Prefix, &c.ExpiresAt, &c.Revoked, &c.Name, &c.TokenHint, &c.CreatedAt, &c.LastUsedAt, &c.Permissions, &c.Version, &c.RenewableUntil, &c.PreviousUntil, &c.ParentCredentialID, &c.OwnerName)
	return c, normalize(e)
}
func credential(ctx context.Context, db DB, scope, id string) (Credential, error) {
	return scanCredential(db.QueryRow(ctx, `SELECT `+credentialFields+` FROM credentials WHERE scope=$1 AND id=$2`, scope, id))
}
func credentialExpiry(value *time.Time, fallback time.Duration) (time.Time, error) {
	now := time.Now()
	until := now.Add(fallback)
	if value != nil {
		until = *value
	}
	if until.Before(now.Add(time.Minute)) || until.After(now.Add(366*24*time.Hour)) {
		return time.Time{}, Bad("Expiry must be between one minute and one year from now.")
	}
	return until, nil
}
func credentialPermissions(value []string) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	if len(value) == 0 || len(value) > 6 {
		return nil, Bad("Choose at least one token permission, or omit permissions to use the identity's access.")
	}
	result := slices.Clone(value)
	for _, v := range result {
		if !slices.Contains([]string{"read", "message", "request", "respond", "execute", "manage"}, v) {
			return nil, Bad("Unknown token permission.")
		}
	}
	slices.Sort(result)
	return slices.Compact(result), nil
}
func (a Actor) CredentialPermits(permission string) bool {
	return a.TokenPermissions == nil || slices.Contains(a.TokenPermissions, permission)
}

type credentialPermissionKey struct{}

func WithCredentialPermission(ctx context.Context, permission string) context.Context {
	return context.WithValue(ctx, credentialPermissionKey{}, permission)
}

// Use recording is coalesced in PostgreSQL. Active renewable workers extend a
// 24-hour lease only near expiry, never beyond their operator-chosen deadline.
// The credential and instance IDs stay stable, including pending delivery fences.
func (s *Store) recordCredentialUse(ctx context.Context, a Actor, lastUsed, expires, until *time.Time, previous bool) error {
	now := time.Now()
	canExtend := !previous && until != nil && expires != nil && expires.Before(now.Add(6*time.Hour)) && expires.Before(*until)
	if lastUsed != nil && now.Sub(*lastUsed) < 5*time.Minute && !canExtend {
		return nil
	}
	_, e := s.Pool.Exec(ctx, `UPDATE credentials SET last_used_at=now(),expires_at=CASE WHEN NOT $3 AND (digest=$4 OR $4='') AND kind='worker' AND expires_at<renewable_until AND expires_at<now()+interval '6 hours' THEN LEAST(now()+interval '24 hours',renewable_until) ELSE expires_at END WHERE scope=$1 AND id=$2 AND NOT revoked AND (expires_at IS NULL OR expires_at>now()) AND (last_used_at IS NULL OR last_used_at<now()-interval '5 minutes' OR (NOT $3 AND (digest=$4 OR $4='') AND kind='worker' AND expires_at<renewable_until AND expires_at<now()+interval '6 hours'))`, a.Scope, a.CredentialID, previous, a.TokenDigest)
	return e
}
func (s *Store) RenewCredential(ctx context.Context, a Actor) (Credential, error) {
	var out Credential
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		if a.CredentialPrevious {
			return Denied()
		}
		tag, e := tx.Exec(ctx, `UPDATE credentials SET expires_at=LEAST(now()+interval '24 hours',renewable_until) WHERE scope=$1 AND id=$2 AND (digest=$3 OR $3='') AND kind='worker' AND NOT revoked AND expires_at>now() AND renewable_until>now()`, a.Scope, a.CredentialID, a.TokenDigest)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return Conflict("This token is not renewable or its renewal deadline has passed.")
		}
		out, e = credential(ctx, tx, a.Scope, a.CredentialID)
		return e
	})
	return out, e
}
func (s *Store) manageCredential(ctx context.Context, db DB, a Actor, id string) (Credential, error) {
	c, e := credential(ctx, db, a.Scope, id)
	if e != nil {
		return c, e
	}
	p, e := principal(ctx, db, a.Scope, c.PrincipalID)
	if e != nil {
		return c, e
	}
	if c.Kind == "admin" || c.Kind == "browser" || !s.mayIssueCredential(ctx, db, a, p) {
		return c, Denied()
	}
	return c, nil
}
func (s *Store) EditCredential(ctx context.Context, a Actor, id string, in CredentialEdit) (Credential, error) {
	var out Credential
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		c, e := s.manageCredential(ctx, tx, a, id)
		if e != nil {
			return e
		}
		if c.Revoked || (c.ExpiresAt != nil && !c.ExpiresAt.After(time.Now())) || in.Version != c.Version {
			return Conflict("This token changed. Refresh it before saving.")
		}
		if in.Name != nil {
			if len(strings.TrimSpace(*in.Name)) > 120 {
				return Bad("Token name must be at most 120 characters.")
			}
			c.Name = strings.TrimSpace(*in.Name)
		}
		if in.ClearPermissions {
			if c.Kind != "api" || in.Permissions != nil {
				return Bad("Choose either identity access or limited permissions.")
			}
			c.Permissions = nil
		}
		if in.Permissions != nil {
			if c.Kind != "api" {
				return Bad("Permission limits are available for API tokens.")
			}
			c.Permissions, e = credentialPermissions(*in.Permissions)
			if e != nil {
				return e
			}
		}
		if in.ExpiresAt != nil {
			until, e := credentialExpiry(in.ExpiresAt, 0)
			if e != nil {
				return e
			}
			if c.RenewableUntil != nil {
				c.RenewableUntil = &until
				lease := time.Now().Add(24 * time.Hour)
				if lease.After(until) {
					lease = until
				}
				c.ExpiresAt = &lease
			} else {
				c.ExpiresAt = &until
			}
		}
		// Editing metadata never revives an expired token.
		if _, e = tx.Exec(ctx, `UPDATE credentials SET name=$3,expires_at=$4,permissions=$5,renewable_until=$6,version=version+1 WHERE scope=$1 AND id=$2`, a.Scope, id, c.Name, c.ExpiresAt, c.Permissions, c.RenewableUntil); e != nil {
			return e
		}
		if e = event(ctx, tx, a, "credential.updated", "", c.PrincipalID, map[string]any{"id": id}); e != nil {
			return e
		}
		out, e = credential(ctx, tx, a.Scope, id)
		return e
	})
	return out, e
}
func (s *Store) RotateCredential(ctx context.Context, a Actor, id string, in CredentialRotation) (map[string]any, error) {
	if in.OverlapSeconds < 0 || in.OverlapSeconds > 86400 {
		return nil, Bad("Rotation overlap must be between zero and 24 hours.")
	}
	var out Credential
	token := Secret()
	e := s.tx(ctx, a, func(tx pgx.Tx) error {
		c, e := s.manageCredential(ctx, tx, a, id)
		if e != nil {
			return e
		}
		if c.Revoked || c.ExpiresAt == nil || !c.ExpiresAt.After(time.Now()) || in.Version != c.Version {
			return Conflict("This token changed, expired or was revoked. Refresh it before rotating.")
		}
		if _, e = tx.Exec(ctx, `UPDATE credentials SET previous_digest=CASE WHEN $4>0 THEN digest ELSE NULL END,previous_until=CASE WHEN $4>0 THEN LEAST(expires_at,now()+make_interval(secs=>$4)) ELSE NULL END,digest=$3,token_hint=$5,version=version+1 WHERE scope=$1 AND id=$2`, a.Scope, id, Digest(token), in.OverlapSeconds, tokenHint(token)); e != nil {
			return e
		}
		if e = event(ctx, tx, a, "credential.rotated", "", c.PrincipalID, map[string]any{"id": id, "overlap_seconds": in.OverlapSeconds}); e != nil {
			return e
		}
		out, e = credential(ctx, tx, a.Scope, id)
		return e
	})
	return map[string]any{"token": token, "credential": out}, e
}
func tokenHint(token string) string { return token[:8] + "…" + token[len(token)-4:] }
