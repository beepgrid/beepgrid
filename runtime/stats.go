package runtime

import "context"

type WorkloadStats struct {
	Queued              int64   `json:"queued"`
	QueueAge            float64 `json:"queue_age"`
	DatabaseConnections int32   `json:"database_connections"`
}

// Workload returns operational totals without tenant identifiers or payloads.
// Offline agent inboxes are not evidence of overdue provider dispatch work.
func (s *Service) Workload(ctx context.Context) (WorkloadStats, error) {
	out := WorkloadStats{DatabaseConnections: s.store.Pool.Stat().TotalConns()}
	err := s.store.Pool.QueryRow(ctx, `SELECT count(*),COALESCE(EXTRACT(EPOCH FROM now()-min(d.next_at)),0)::float8 FROM deliveries d JOIN scopes s ON s.id=d.scope WHERE NOT s.suspended AND NOT s.recovery_hold AND d.connection_id<>'' AND d.state='queued' AND d.next_at<=now() AND d.expires_at>now()`).Scan(&out.Queued, &out.QueueAge)
	return out, err
}
