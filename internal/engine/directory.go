package engine

import (
	"context"
	"encoding/json"
)

// Callers pass only IDs whose visibility they already verified in this operation.
// The per-agent bound matches the individual instances endpoint.
func (s *Store) instancesForAgents(ctx context.Context, scope string, ids []string) (map[string][]Instance, error) {
	return s.instancesForAgentsDB(ctx, s.Pool, scope, ids)
}
func (s *Store) instancesForAgentsDB(ctx context.Context, db DB, scope string, ids []string) (map[string][]Instance, error) {
	out := map[string][]Instance{}
	for _, id := range ids {
		out[id] = []Instance{}
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, e := db.Query(ctx, agentInstancesSQL, scope, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var x Instance
		var caps, progress []byte
		if e = rows.Scan(&x.ID, &x.AgentID, &caps, &progress, &x.LastSeen, &x.Presence); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(caps, &x.Capabilities)
		_ = json.Unmarshal(progress, &x.Progress)
		out[x.AgentID] = append(out[x.AgentID], x)
	}
	return out, rows.Err()
}

const agentInstancesSQL = `SELECT i.id,i.agent_id,i.capabilities,i.progress,i.last_seen,
 CASE WHEN c.revoked OR c.expires_at<=now() OR p.disabled THEN 'offline' WHEN i.last_seen>now()-interval '45 seconds' THEN 'connected' WHEN i.last_seen>now()-interval '2 minutes' THEN 'stale' ELSE 'offline' END AS presence
 FROM (SELECT DISTINCT unnest($2::text[]) AS agent_id) wanted
 CROSS JOIN LATERAL (
 SELECT id,agent_id,credential_id,capabilities,progress,last_seen,created_at
 FROM instances WHERE scope=$1 AND agent_id=wanted.agent_id
 ORDER BY created_at DESC,id DESC LIMIT 1000
 ) i
 CROSS JOIN LATERAL (SELECT revoked,expires_at FROM credentials WHERE id=i.credential_id AND scope=$1 LIMIT 1) c
 JOIN principals p ON p.id=i.agent_id AND p.scope=$1
 ORDER BY i.agent_id,i.created_at DESC,i.id DESC`
