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
	routes, err := s.resolveProviderRoutes(ctx, tx, a, rs, conv, "message", "", nil)
	if err != nil {
		return m, err
	}
	type record struct {
		RecipientID   string          `json:"recipient_id"`
		ExactInstance string          `json:"exact_instance"`
		DeliveryID    string          `json:"delivery_id"`
		PolicyTarget  Target          `json:"policy_target"`
		Native        bool            `json:"native"`
		Providers     []providerRoute `json:"providers"`
	}
	items := make([]record, 0, len(rs))
	for _, r := range rs {
		item := record{RecipientID: r.ID, ExactInstance: r.ExactInstance, DeliveryID: ID("del"), PolicyTarget: r.PolicyTarget,
			Native: s.allowed(ctx, tx, a, "message", r.PolicyTarget, "", "")}
		item.Providers = routes[r.ID]
		if !item.Native && len(item.Providers) == 0 {
			return m, &Error{"NO_ELIGIBLE_ROUTE", "No permitted destination is configured for this recipient.", 403}
		}
		items = append(items, item)
	}
	_, err = tx.Exec(ctx, appendMessageSQL, a.Scope, JSON(m), JSON(items), a.CredentialID, a.ProviderConnection, time.Now().Add(7*24*time.Hour), JSON(deliveryIdentity(a)))
	if err == nil {
		err = linkAttachments(ctx, tx, a.Scope, conv, m.ID, "", m.Attachments)
	}
	return m, err
}

//go:embed message_write.sql
var appendMessageSQL string
