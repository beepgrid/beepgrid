package engine

import (
	"context"
	"encoding/json"
)

// Only exact, explicitly keyed informational questions coalesce. Every request retains its own durable handle.
func (s *Store) promoteQuestions(ctx context.Context) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT `+deliveryFields+`,scope FROM deliveries WHERE state='waiting' AND connection_id<>'' AND (NOT EXISTS(SELECT 1 FROM requests r WHERE r.scope=deliveries.scope AND r.id=deliveries.operation_id AND r.state='open' AND r.expires_at>now()) OR NOT EXISTS(SELECT 1 FROM requests r WHERE r.scope=deliveries.scope AND r.id=deliveries.payload->>'_cluster' AND r.state='open' AND r.expires_at>now())) AND scope IN(SELECT id FROM scopes WHERE NOT recovery_hold) ORDER BY created_at,id LIMIT 100 FOR UPDATE SKIP LOCKED`)
	if e != nil {
		return e
	}
	type item struct {
		d     Delivery
		scope string
	}
	items := []item{}
	for rows.Next() {
		var d Delivery
		var raw []byte
		var scope string
		if e = rows.Scan(&d.ID, &d.RecipientID, &d.OperationID, &raw, &d.State, &d.Generation, &d.Attempts, &d.LastError, &d.ConnectionID, &d.Address, &d.ExpiresAt, &scope); e != nil {
			rows.Close()
			return e
		}
		_ = json.Unmarshal(raw, &d.Payload)
		items = append(items, item{d, scope})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, x := range items {
		d := x.d
		state := "waiting"
		detail := "Waiting for a matching open informational question"
		var ownOpen, leaderOpen bool
		_ = tx.QueryRow(ctx, `SELECT state='open' AND expires_at>now() FROM requests WHERE scope=$1 AND id=$2`, x.scope, d.OperationID).Scan(&ownOpen)
		leader, _ := d.Payload["_cluster"].(string)
		_ = tx.QueryRow(ctx, `SELECT state='open' AND expires_at>now() FROM requests WHERE scope=$1 AND id=$2`, x.scope, leader).Scan(&leaderOpen)
		if !ownOpen {
			state = "cancelled"
			detail = "Matching question already resolved or ended"
		} else if !leaderOpen {
			var q Request
			_ = json.Unmarshal(JSON(d.Payload["request"]), &q)
			var successor string
			e = tx.QueryRow(ctx, `SELECT r.id FROM requests r JOIN request_audience ra ON ra.scope=r.scope AND ra.request_id=r.id WHERE r.scope=$1 AND r.kind='question' AND r.state='open' AND r.expires_at>now() AND r.shared_key=$2 AND r.title=$3 AND r.body=$4 AND r.choices=$5::jsonb AND ra.principal_id=$6 ORDER BY r.created_at,r.id LIMIT 1`, x.scope, q.SharedKey, q.Title, q.Body, JSON(q.Choices), d.RecipientID).Scan(&successor)
			if e != nil {
				return e
			}
			if successor == d.OperationID {
				state = "queued"
				detail = "Matching question ended; next independent request requires attention"
			} else {
				d.Payload["_cluster"] = successor
				if _, e = tx.Exec(ctx, `UPDATE deliveries SET payload=$3 WHERE scope=$1 AND id=$2`, x.scope, d.ID, JSON(d.Payload)); e != nil {
					return e
				}
			}
		}
		if _, e = tx.Exec(ctx, `UPDATE deliveries SET state=$3,last_error=$4,next_at=now() WHERE scope=$1 AND id=$2`, x.scope, d.ID, state, detail); e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}
