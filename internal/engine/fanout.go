package engine

import (
	"context"
	_ "embed"
	"time"

	"github.com/jackc/pgx/v5"
)

// New private native conversations have no provider-thread routes. Batch their
// writes only when none of the audience has an enabled provider destination.
// External routing and existing conversations keep the full enqueue path.
func (s *Store) nativeFanout(ctx context.Context, tx pgx.Tx, a Actor, in SendInput, rs []recipient, out *SendResult) (bool, error) {
	ids := make([]string, 0, len(rs))
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	var external bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM destinations d JOIN connections c ON c.scope=d.scope AND c.id=d.connection_id WHERE d.scope=$1 AND d.principal_id=ANY($2::text[]) AND c.enabled)`, a.Scope, ids).Scan(&external); e != nil {
		return false, e
	}
	if external {
		return false, nil
	}
	ctx = withPolicySnapshot(ctx)
	for _, r := range rs {
		if !s.allowed(ctx, tx, a, "message", r.PolicyTarget, "", "") {
			return true, &Error{"NO_ELIGIBLE_ROUTE", "No permitted destination is configured for this recipient.", 403}
		}
	}
	title := in.Title
	if title == "" {
		title = "Conversation"
	}
	type record struct {
		ConversationID string  `json:"conversation_id"`
		RecipientID    string  `json:"recipient_id"`
		DeliveryID     string  `json:"delivery_id"`
		Message        Message `json:"message"`
		PolicyTarget   Target  `json:"policy_target"`
	}
	items := make([]record, 0, len(rs))
	for _, r := range rs {
		m := Message{ID: ID("msg"), ConversationID: ID("con"), SenderID: a.ID, InstanceID: a.InstanceID, Body: in.Body, CreatedAt: time.Now()}
		items = append(items, record{m.ConversationID, r.ID, ID("del"), m, r.PolicyTarget})
		out.Messages = append(out.Messages, m)
	}
	raw := JSON(items)
	// All inserts and the event range remain one atomic workspace transaction.
	// PostgreSQL parses the batch once, rather than receiving seven copies.
	if _, e := tx.Exec(ctx, nativeFanoutSQL, a.Scope, raw, title, a.ID, a.InstanceID, a.CredentialID, a.ProviderConnection, time.Now().Add(7*24*time.Hour)); e != nil {
		return true, e
	}
	out.ConversationID = out.Messages[0].ConversationID
	return true, nil
}

//go:embed fanout.sql
var nativeFanoutSQL string
