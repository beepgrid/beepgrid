package engine

import (
	"context"
	"github.com/jackc/pgx/v5"
	"time"
)

type WorkspaceInfo struct {
	Scope          string `json:"scope"`
	Company        string `json:"company"`
	Environment    string `json:"environment"`
	CanReadAccount bool   `json:"can_read_account"`
}
type AccountSnapshot struct {
	WorkspaceInfo
	Suspended      bool             `json:"suspended"`
	RecoveryHold   bool             `json:"recovery_hold"`
	AccessUntil    *time.Time       `json:"access_until,omitempty"`
	ControlVersion int64            `json:"control_version"`
	Budget         *WorkspaceBudget `json:"budget"`
	Usage          []UsageUnit      `json:"usage"`
	Metered        bool             `json:"metered"`
	Source         string           `json:"source"`
}

func (s *Store) Workspace(ctx context.Context, a Actor) (WorkspaceInfo, error) {
	out := WorkspaceInfo{Scope: a.Scope, CanReadAccount: s.accountReader(ctx, a)}
	e := s.Pool.QueryRow(ctx, `SELECT company_name,environment FROM scopes WHERE id=$1`, a.Scope).Scan(&out.Company, &out.Environment)
	return out, normalize(e)
}
func (s *Store) accountReader(ctx context.Context, a Actor) bool {
	return a.Kind == "person" && s.allowed(ctx, s.Pool, a, "view", Target{"account", "workspace"}, "", "")
}
func (s *Store) Account(ctx context.Context, a Actor) (AccountSnapshot, error) {
	out := AccountSnapshot{Usage: []UsageUnit{}, Source: "applied_runtime"}

	if a.CredentialID == "" {
		return out, Denied()
	}
	var kind string
	if e := s.Pool.QueryRow(ctx, `SELECT c.kind FROM credentials c JOIN principals p ON p.scope=c.scope AND p.id=c.principal_id WHERE c.scope=$1 AND c.id=$2 AND c.principal_id=$3 AND NOT c.revoked AND NOT p.disabled AND (c.expires_at IS NULL OR c.expires_at>now())`, a.Scope, a.CredentialID, a.ID).Scan(&kind); e != nil {
		if e == pgx.ErrNoRows {
			return out, Denied()
		}
		return out, authorizationUnavailable()
	}
	a.Admin = kind == "admin"
	if kind == "browser" {
		if e := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked AND (expires_at IS NULL OR expires_at>now()))`, a.Scope, a.ID).Scan(&a.Admin); e != nil {
			return out, authorizationUnavailable()
		}
	}
	if !s.accountReader(ctx, a) {
		return out, Denied()
	}
	var e error
	out.WorkspaceInfo, e = s.Workspace(ctx, a)
	if e != nil {
		return out, e
	}
	e = s.Pool.QueryRow(ctx, `SELECT suspended,recovery_hold,access_until,control_version FROM scopes WHERE id=$1`, a.Scope).Scan(&out.Suspended, &out.RecoveryHold, &out.AccessUntil, &out.ControlVersion)
	if e != nil {
		return out, normalize(e)
	}
	var b WorkspaceBudget
	e = s.Pool.QueryRow(ctx, `SELECT version,messages,requests,upload_bytes FROM workspace_budgets WHERE scope=$1`, a.Scope).Scan(&b.Version, &b.Messages, &b.Requests, &b.UploadBytes)
	if e != nil && e != pgx.ErrNoRows {
		return out, e
	}
	if e == nil {
		out.Budget = &b
		out.Metered = true
	}
	out.Usage, e = s.WorkspaceUsage(ctx, a.Scope)
	return out, e
}
