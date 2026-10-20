package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

func deliveryScan(row pgx.Row) (Delivery, error) {
	var d Delivery
	var raw []byte
	e := row.Scan(&d.ID, &d.RecipientID, &d.OperationID, &raw, &d.State, &d.Generation, &d.Attempts, &d.LastError, &d.ConnectionID, &d.Address, &d.ExpiresAt)
	_ = json.Unmarshal(raw, &d.Payload)
	return d, normalize(e)
}

const deliveryFields = `id,recipient_id,operation_id,payload,state,generation,attempts,last_error,connection_id,address,expires_at`

// currentDeliveryPolicy rechecks the originating credential, group membership and route grants before exposing content.
func (s *Store) currentDeliveryPolicy(ctx context.Context, db DB, scope string, d Delivery) bool {
	var internal struct {
		RequestID string `json:"engine_result"`
	}
	_ = json.Unmarshal(JSON(d.Payload["_policy"]), &internal)
	if internal.RequestID != "" {
		var valid bool
		_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM requests r JOIN principals p ON p.id=r.requester_id AND p.scope=r.scope WHERE r.scope=$1 AND r.id=$2 AND r.requester_id=$3 AND NOT p.disabled)`, scope, internal.RequestID, d.RecipientID).Scan(&valid)
		return valid
	}
	var policy struct {
		CredentialID       string `json:"credential_id"`
		ProviderConnection string `json:"provider_connection"`
		SenderID           string `json:"sender_id"`
		Target             Target `json:"target"`
		Action             string `json:"action"`
		ActionName         string `json:"action_name"`
	}
	if json.Unmarshal(JSON(d.Payload["_policy"]), &policy) != nil || policy.SenderID == "" {
		return false
	}
	p, e := principal(ctx, db, scope, policy.SenderID)
	if e != nil || p.Disabled {
		return false
	}
	sender := Actor{Principal: p, Scope: scope}
	var valid bool
	if policy.ProviderConnection != "" {
		if e = db.QueryRow(ctx, `SELECT enabled FROM connections WHERE scope=$1 AND id=$2`, scope, policy.ProviderConnection).Scan(&valid); e != nil || !valid {
			return false
		}
		sender.CredentialKind = "provider"
	} else {
		if e = db.QueryRow(ctx, `SELECT NOT revoked,kind FROM credentials WHERE scope=$1 AND id=$2 AND principal_id=$3`, scope, policy.CredentialID, p.ID).Scan(&valid, &sender.CredentialKind); e != nil || !valid {
			return false
		}
	}
	sender.Admin = sender.CredentialKind == "admin"
	if sender.CredentialKind == "browser" {
		_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM credentials WHERE scope=$1 AND principal_id=$2 AND kind='admin' AND NOT revoked)`, scope, p.ID).Scan(&sender.Admin)
	}
	recipient, e := principal(ctx, db, scope, d.RecipientID)
	if e != nil || recipient.Disabled {
		return false
	}
	if policy.Target.Kind == "group" {
		var member bool
		_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE scope=$1 AND group_id=$2 AND principal_id=$3)`, scope, policy.Target.ID, d.RecipientID).Scan(&member)
		if !member {
			return false
		}
	}
	if request, ok := d.Payload["request"].(map[string]any); ok && d.Payload["type"] == "request" {
		var open bool
		_ = db.QueryRow(ctx, `SELECT state='open' AND expires_at>now() FROM requests WHERE scope=$1 AND id=$2`, scope, request["id"]).Scan(&open)
		if !open {
			return false
		}
	}
	var detail struct {
		Request struct {
			Action *Action `json:"action"`
		} `json:"request"`
	}
	_ = json.Unmarshal(JSON(d.Payload), &detail)
	return s.allowed(ctx, db, sender, policy.Action, policy.Target, d.ConnectionID, policy.ActionName, detail.Request.Action)
}
func (s *Store) Inbox(ctx context.Context, a Actor) ([]Delivery, error) {
	if a.InstanceID == "" {
		return nil, Denied()
	}
	out := []Delivery{}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `UPDATE instances SET last_seen=now() WHERE scope=$1 AND id=$2 AND last_seen<now()-interval '10 seconds'`, a.Scope, a.InstanceID); e != nil {
			return e
		}
		rows, e := tx.Query(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE scope=$1 AND recipient_id=$2 AND connection_id='' AND expires_at>now() AND (exact_instance='' OR exact_instance=$3) AND ((state='queued' AND next_at<=now()) OR (state='leased' AND (lease_until<now() OR owner_instance=$3))) ORDER BY created_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`, a.Scope, a.ID, a.InstanceID)
		if e != nil {
			return e
		}
		jobs := []Delivery{}
		for rows.Next() {
			d, e := deliveryScan(rows)
			if e != nil {
				rows.Close()
				return e
			}
			jobs = append(jobs, d)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		for _, d := range jobs {
			if !s.currentDeliveryPolicy(ctx, tx, a.Scope, d) {
				if _, e = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled',last_error='Current policy or request validity denied delivery' WHERE scope=$1 AND id=$2`, a.Scope, d.ID); e != nil {
					return e
				}
				continue
			}
			if e = tx.QueryRow(ctx, `UPDATE deliveries SET state='leased',owner_instance=$3,generation=generation+1,lease_until=now()+interval '30 seconds',attempts=attempts+1 WHERE scope=$1 AND id=$2 RETURNING generation,attempts`, a.Scope, d.ID, a.InstanceID).Scan(&d.Generation, &d.Attempts); e != nil {
				return e
			}
			delete(d.Payload, "_policy")
			d.State = "leased"
			out = append(out, d)
		}
		return nil
	})
	return out, err
}
func (s *Store) Ack(ctx context.Context, a Actor, id string, generation int64) error {
	if a.InstanceID == "" {
		return Denied()
	}
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		var state, owner string
		var gen int64
		e := tx.QueryRow(ctx, `SELECT state,owner_instance,generation FROM deliveries WHERE scope=$1 AND id=$2 AND recipient_id=$3 AND connection_id=''`, a.Scope, id, a.ID).Scan(&state, &owner, &gen)
		if e != nil {
			return e
		}
		if owner != a.InstanceID || gen != generation {
			return Conflict("This delivery lease is no longer owned by this instance.")
		}
		if state == "received" {
			return nil
		}
		var valid bool
		_ = tx.QueryRow(ctx, `SELECT lease_until>now() FROM deliveries WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&valid)
		if state != "leased" || !valid {
			return Conflict("The delivery lease expired.")
		}
		if _, e = tx.Exec(ctx, `UPDATE deliveries SET state='received' WHERE scope=$1 AND id=$2`, a.Scope, id); e != nil {
			return e
		}
		return event(ctx, tx, a, "worker.received", "", a.ID, map[string]any{"delivery_id": id, "instance_id": a.InstanceID})
	})
}
func (s *Store) Deliveries(ctx context.Context, a Actor) ([]Delivery, error) {
	if !a.Admin {
		return nil, Denied()
	}
	rows, e := s.Pool.Query(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE scope=$1 ORDER BY created_at DESC LIMIT 200`, a.Scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		d, e := deliveryScan(rows)
		if e != nil {
			return nil, e
		}
		delete(d.Payload, "_policy")
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) RetryDelivery(ctx context.Context, a Actor, id string) error {
	return s.tx(ctx, a, func(tx pgx.Tx) error {
		if !a.Admin {
			return Denied()
		}
		d, e := deliveryScan(tx.QueryRow(ctx, `SELECT `+deliveryFields+` FROM deliveries WHERE scope=$1 AND id=$2`, a.Scope, id))
		if e != nil {
			return e
		}
		if d.State != "failed" && d.State != "unknown" {
			return Conflict("Only failed or unknown deliveries can be manually retried.")
		}
		if d.ExpiresAt.Before(time.Now()) || !s.currentDeliveryPolicy(ctx, tx, a.Scope, d) {
			return Denied()
		}
		if _, e = tx.Exec(ctx, `UPDATE deliveries SET state='queued',next_at=now(),last_error='' WHERE scope=$1 AND id=$2`, a.Scope, id); e != nil {
			return e
		}
		return event(ctx, tx, a, "delivery.retried", "", a.ID, map[string]any{"id": id, "duplicate_risk": d.State == "unknown"})
	})
}
