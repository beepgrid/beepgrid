package engine

import (
	"context"
	_ "embed"
	"time"

	"github.com/jackc/pgx/v5"
)

// Persist a normal conversation message, its event and every permitted native
// and provider delivery together. The caller still holds the workspace fence,
// revalidates current credentials and commits the idempotency receipt before ack.
// Requests retain their separate approval/coalescing enqueue path.
func (s *Store) appendConversationMessage(ctx context.Context, tx pgx.Tx, a Actor, conv, body string, rs []recipient, addressed ...[]Mention) (Message, error) {
	var mentions []Mention
	if len(addressed) > 0 {
		mentions = addressed[0]
	}
	return s.appendMessageFiles(ctx, tx, a, conv, body, rs, mentions, nil)
}
func (s *Store) appendMessageFiles(ctx context.Context, tx pgx.Tx, a Actor, conv, body string, rs []recipient, mentions []Mention, fileIDs []string) (Message, error) {
	m := Message{ID: ID("msg"), ConversationID: conv, SenderID: a.ID, InstanceID: a.InstanceID, Body: body, CreatedAt: time.Now()}
	m.Mentions = mentions
	files, err := s.claimAttachments(ctx, tx, a, fileIDs)
	if err != nil {
		return m, err
	}
	m.Attachments = messageFiles(files, conv, m.ID)
	if len(m.Mentions) > 0 {
		if err := s.validateMentions(ctx, tx, a, conv, body, m.Mentions); err != nil {
			return m, err
		}
	}
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
	rows, err := tx.Query(ctx, `SELECT d.principal_id,d.connection_id,d.address FROM destinations d
 JOIN connections c ON c.scope=d.scope AND c.id=d.connection_id
 WHERE d.scope=$1 AND d.principal_id=ANY($2::text[]) AND c.enabled AND d.mode IN ('primary','copy')
 UNION ALL SELECT t.person_id,t.connection_id,t.channel_id FROM provider_threads t
 JOIN connections c ON c.scope=t.scope AND c.id=t.connection_id
 WHERE t.scope=$1 AND t.conversation_id=$3 AND t.person_id=ANY($2::text[]) AND c.enabled`, a.Scope, ids, conv)
	if err != nil {
		return m, err
	}
	for rows.Next() {
		var pid string
		var d route
		if err = rows.Scan(&pid, &d.ConnectionID, &d.Address); err != nil {
			rows.Close()
			return m, err
		}
		routes[pid] = append(routes[pid], d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return m, err
	}
	type record struct {
		RecipientID   string  `json:"recipient_id"`
		ExactInstance string  `json:"exact_instance"`
		DeliveryID    string  `json:"delivery_id"`
		PolicyTarget  Target  `json:"policy_target"`
		Native        bool    `json:"native"`
		Providers     []route `json:"providers"`
	}
	items := make([]record, 0, len(rs))
	for _, r := range rs {
		item := record{RecipientID: r.ID, ExactInstance: r.ExactInstance, DeliveryID: ID("del"), PolicyTarget: r.PolicyTarget,
			Native: s.allowed(ctx, tx, a, "message", r.PolicyTarget, "", "")}
		for _, d := range routes[r.ID] {
			if s.allowed(ctx, tx, a, "message", r.PolicyTarget, d.ConnectionID, "") {
				d.DeliveryID = ID("del")
				item.Providers = append(item.Providers, d)
			}
		}
		if !item.Native && len(item.Providers) == 0 {
			return m, &Error{"NO_ELIGIBLE_ROUTE", "No permitted destination is configured for this recipient.", 403}
		}
		items = append(items, item)
	}
	_, err = tx.Exec(ctx, appendMessageSQL, a.Scope, JSON(m), JSON(items), a.CredentialID, a.ProviderConnection, time.Now().Add(7*24*time.Hour))
	if err == nil {
		err = linkAttachments(ctx, tx, a.Scope, conv, m.ID, "", m.Attachments)
	}
	return m, err
}

//go:embed message_write.sql
var appendMessageSQL string
