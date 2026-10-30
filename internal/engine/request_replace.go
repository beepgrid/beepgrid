package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

type ReplaceRequestInput struct {
	Version  int       `json:"version"`
	Body     string    `json:"body"`
	Mentions []Mention `json:"mentions,omitempty"`
}

type ReplacedRequest struct {
	Previous    Request `json:"previous"`
	Replacement Request `json:"replacement"`
}

// ReplaceRequest atomically retires one open request and creates a fresh decision.
// Original recipients, private chat projections, files and bound action remain
// unchanged. New group members cannot silently enter the original audience.
func (s *Store) ReplaceRequest(ctx context.Context, a Actor, id string, in ReplaceRequestInput, key string) (ReplacedRequest, error) {
	var out ReplacedRequest
	if !ValidText(in.Body, 32768) {
		return out, Bad("Write a replacement message.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		ctx = withPolicySnapshot(ctx)
		return idempotent(ctx, tx, a, "replace_request", key, struct {
			ID    string
			Input ReplaceRequestInput
		}{id, in}, &out, func() error {
			r, err := s.request(ctx, tx, a, id)
			if err != nil {
				return err
			}
			if a.ID != r.RequesterID {
				return Denied()
			}
			if r.State != "open" || r.Version != in.Version {
				return Conflict("This message already changed or received a response. Refresh before replacing it.")
			}
			continuation, err := s.replacementAudience(ctx, tx, a, r)
			if err != nil {
				return err
			}
			for _, file := range r.Attachments {
				if !s.canReadAttachment(ctx, tx, a, file) {
					return Missing()
				}
			}
			continuation.files = r.Attachments
			next := AskInput{ConversationID: r.ConversationID, Kind: r.Kind, Body: in.Body, Choices: r.Choices, Action: r.Action, Blocking: r.Blocking, Mentions: in.Mentions}
			// Unlimited communication expiry is represented by year 9999.
			// Finite deadlines stay exact, rather than restarting on replacement.
			if r.ExpiresAt.Before(time.Now().Add(90 * 24 * time.Hour)) {
				next.ExpiresAt = r.ExpiresAt
			}
			if err = s.askContinuedTx(ctx, tx, a, next, "", &out.Replacement, continuation); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE requests SET state='superseded',resolved_at=now() WHERE scope=$1 AND id=$2`, a.Scope, id); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE deliveries SET state='cancelled' WHERE scope=$1 AND operation_id=$2 AND state IN ('queued','leased','waiting')`, a.Scope, id); err != nil {
				return err
			}
			out.Previous, err = s.request(ctx, tx, a, id)
			if err != nil {
				return err
			}
			return s.requestEvent(ctx, tx, a, out.Previous, "request.superseded")
		})
	})
	if err != nil {
		return ReplacedRequest{}, err
	}
	// Return current authorized state on a replay, never a stale stored approval.
	out.Previous, err = s.Request(ctx, a, out.Previous.ID)
	if err == nil {
		out.Replacement, err = s.Request(ctx, a, out.Replacement.ID)
	}
	return out, err
}

func (s *Store) replacementAudience(ctx context.Context, tx pgx.Tx, a Actor, request Request) (*requestContinuation, error) {
	out := &requestContinuation{projections: map[string]string{}}
	type audience struct {
		id, projection, exact string
		target                Target
	}
	all := []audience{}
	rows, err := tx.Query(ctx, `SELECT ra.principal_id,ra.conversation_id,ra.policy_target,COALESCE((SELECT NULLIF(d.exact_instance,'') FROM deliveries d WHERE d.scope=ra.scope AND d.operation_id=ra.request_id AND d.recipient_id=ra.principal_id AND d.connection_id='' LIMIT 1),'') FROM request_audience ra WHERE ra.scope=$1 AND ra.request_id=$2 ORDER BY ra.principal_id`, a.Scope, request.ID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item audience
		var raw []byte
		if err = rows.Scan(&item.id, &item.projection, &raw, &item.exact); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(raw, &item.target); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	resolved := map[Target][]recipient{}
	action := ""
	if request.Action != nil {
		action = request.Action.Name
	}
	for _, item := range all {
		// Removal or lost visibility must fail the whole replacement, never
		// partially send or silently change its intended recipients.
		if !s.canRead(ctx, tx, a, item.projection) {
			return nil, Missing()
		}
		if request.shared {
			var active bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM participants WHERE scope=$1 AND conversation_id=$2 AND principal_id=$3 AND left_at IS NULL)`, a.Scope, item.projection, item.id).Scan(&active); err != nil {
				return nil, err
			}
			if !active {
				return nil, Conflict("A recipient left this chat. Send a new message to its current members instead.")
			}
		}
		rs, ok := resolved[item.target]
		if !ok {
			rs, err = s.resolve(ctx, tx, a, item.target, "ask", action, request.Action)
			if err != nil {
				return nil, err
			}
			resolved[item.target] = rs
		}
		found := false
		for _, peer := range rs {
			if peer.ID != item.id {
				continue
			}
			peer.ExactInstance = item.exact
			out.recipients = append(out.recipients, peer)
			out.projections[item.id] = item.projection
			found = true
			break
		}
		if !found {
			return nil, Denied()
		}
	}
	if len(out.recipients) == 0 {
		return nil, Bad("This message has no available recipients.")
	}
	return out, nil
}
