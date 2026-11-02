package engine

import (
	"context"
	"time"
)

// WorkspaceBudget is operator policy. Units count canonical recipient messages,
// requests and accepted upload bytes per UTC month, from metering activation.
// They are not provider attempts, API calls, invoice lines or storage gauges.
type WorkspaceBudget struct {
	Version     int64 `json:"version"`
	Messages    int64 `json:"messages"`
	Requests    int64 `json:"requests"`
	UploadBytes int64 `json:"upload_bytes"`
}
type UsageUnit struct {
	Period time.Time `json:"period"`
	Metric string    `json:"metric"`
	Units  int64     `json:"units"`
}

func (s *Store) ApplyWorkspaceBudget(ctx context.Context, scope string, b WorkspaceBudget) error {
	if b.Version < 1 || b.Messages < -1 || b.Requests < -1 || b.UploadBytes < -1 {
		return Bad("Invalid workspace budget.")
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var erased string
	if err = tx.QueryRow(ctx, `SELECT erasure_operation FROM scopes WHERE id=$1 FOR UPDATE`, scope).Scan(&erased); err != nil {
		return normalize(err)
	}
	if erased != "" {
		return Conflict("Erased workspaces cannot receive a new budget.")
	}
	tag, err := tx.Exec(ctx, `INSERT INTO workspace_budgets VALUES($1,$2,$3,$4,$5)
 ON CONFLICT(scope) DO UPDATE SET version=excluded.version,messages=excluded.messages,requests=excluded.requests,upload_bytes=excluded.upload_bytes
 WHERE workspace_budgets.version<excluded.version OR (workspace_budgets.version=excluded.version AND workspace_budgets.messages=excluded.messages AND workspace_budgets.requests=excluded.requests AND workspace_budgets.upload_bytes=excluded.upload_bytes)`, scope, b.Version, b.Messages, b.Requests, b.UploadBytes)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Conflict("A newer or conflicting budget is already applied.")
	}
	return tx.Commit(ctx)
}
func (s *Store) WorkspaceUsage(ctx context.Context, scope string) ([]UsageUnit, error) {
	rows, err := s.Pool.Query(ctx, `SELECT period,metric,units FROM workspace_usage WHERE scope=$1 ORDER BY period,metric`, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []UsageUnit{}
	for rows.Next() {
		var u UsageUnit
		if err = rows.Scan(&u.Period, &u.Metric, &u.Units); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
