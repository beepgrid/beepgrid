package engine

import "context"

func (s *Store) mayReadDelivery(ctx context.Context, db DB, a Actor, d Delivery) bool {
	if !s.deliveryChatVisible(ctx, db, a.Scope, d, a.ID, a.Admin) {
		return false
	}
	if a.Admin {
		return true
	}
	var requester string
	if e := db.QueryRow(ctx, `SELECT requester_id FROM requests WHERE scope=$1 AND id=split_part($2,'/',1)`, a.Scope, d.OperationID).Scan(&requester); e == nil && requester == a.ID {
		return true
	}
	if a.ID == d.RecipientID {
		if _, e := s.request(ctx, db, a, d.OperationID); e == nil {
			return true
		}
		if conv, ok := d.Payload["conversation_id"].(string); ok && !s.canRead(ctx, db, a, conv) {
			return false
		}
		return s.currentDeliveryPolicy(ctx, db, a.Scope, d)
	}
	p, e := principal(ctx, db, a.Scope, d.RecipientID)
	return e == nil && s.allowed(ctx, db, a, "view", Target{p.Kind, p.ID}, "", "") && s.allowed(ctx, db, a, "view", Target{"delivery", d.ID}, "", "")
}
