package engine

import (
	"context"
	_ "embed"
	"time"

	"github.com/jackc/pgx/v5"
)

// A new private conversation cannot have an incoming provider-thread route.
// Resolve configured destinations once and batch canonical and provider writes
// together. Existing conversations and requests retain the full enqueue path.
func (s *Store) nativeFanout(ctx context.Context, tx pgx.Tx, a Actor, in SendInput, rs []recipient, out *SendResult) (bool, error) {
	ids := make([]string, 0, len(rs))
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	type route struct {
		ConnectionID string `json:"connection_id"`
		Address      string `json:"address"`
		DeliveryID   string `json:"delivery_id"`
	}
	routes := map[string][]route{}
	rows, err := tx.Query(ctx, `SELECT d.principal_id,d.connection_id,d.address FROM destinations d JOIN connections c ON c.scope=d.scope AND c.id=d.connection_id WHERE d.scope=$1 AND d.principal_id=ANY($2::text[]) AND c.enabled AND d.mode IN ('primary','copy') ORDER BY d.priority,d.id`, a.Scope, ids)
	if err != nil {
		return true, err
	}
	for rows.Next() {
		var pid string
		var d route
		if err = rows.Scan(&pid, &d.ConnectionID, &d.Address); err != nil {
			rows.Close()
			return true, err
		}
		routes[pid] = append(routes[pid], d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return true, err
	}
	ctx = withPolicySnapshot(ctx)
	title := in.Title
	if title == "" {
		title = "Conversation"
	}
	type record struct {
		ConversationID string    `json:"conversation_id"`
		RecipientID    string    `json:"recipient_id"`
		DeliveryID     string    `json:"delivery_id"`
		MessageID      string    `json:"message_id"`
		CreatedAt      time.Time `json:"created_at"`
		PolicyTarget   Target    `json:"policy_target"`
		Native         bool      `json:"native"`
		Providers      []route   `json:"providers"`
	}
	items := make([]record, 0, len(rs))
	for _, r := range rs {
		m := Message{ID: ID("msg"), ConversationID: ID("con"), SenderID: a.ID, InstanceID: a.InstanceID, Body: in.Body, CreatedAt: time.Now()}
		item := record{ConversationID: m.ConversationID, RecipientID: r.ID, DeliveryID: ID("del"), MessageID: m.ID, CreatedAt: m.CreatedAt, PolicyTarget: r.PolicyTarget,
			Native: s.allowed(ctx, tx, a, "message", r.PolicyTarget, "", "")}
		for _, d := range routes[r.ID] {
			if s.allowed(ctx, tx, a, "message", r.PolicyTarget, d.ConnectionID, "") {
				d.DeliveryID = ID("del")
				item.Providers = append(item.Providers, d)
			}
		}
		if !item.Native && len(item.Providers) == 0 {
			return true, &Error{"NO_ELIGIBLE_ROUTE", "No permitted destination is configured for this recipient.", 403}
		}
		items = append(items, item)
		out.Messages = append(out.Messages, m)
	}
	// Bound materialized JSON per statement. Large bodies otherwise spill the
	// input and delivery projections to temporary files or retain large backend
	// contexts. Ordinary small messages still use a single round trip. Every
	// chunk shares this workspace transaction: no partial history or delivery
	// is visible or acknowledged if any later statement fails.
	batchSize := max(1, (2<<20)/(len(in.Body)+512))
	expiry := time.Now().Add(7 * 24 * time.Hour)
	for start := 0; start < len(items); start += batchSize {
		raw := JSON(items[start:min(start+batchSize, len(items))])
		if _, e := tx.Exec(ctx, nativeFanoutSQL, a.Scope, raw, title, a.ID, a.InstanceID, a.CredentialID, a.ProviderConnection, expiry, in.Body); e != nil {
			return true, e
		}
	}
	out.ConversationID = out.Messages[0].ConversationID
	return true, nil
}

//go:embed fanout.sql
var nativeFanoutSQL string
