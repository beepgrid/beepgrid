package engine

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5"
)

// New request conversations have no incoming provider-thread override. Shared
// informational keys retain enqueue's exact-key coalescing path. Every batch
// remains inside the original fenced transaction, including all projections,
// audience policy, events, configured provider routes and native inboxes.
func (s *Store) newRequestAudience(ctx context.Context, tx pgx.Tx, a Actor, request Request, recipients []recipient) error {
	ctx = withPolicySnapshot(ctx)
	actionName := ""
	if request.Action != nil {
		actionName = request.Action.Name
	}
	for _, r := range recipients {
		if s.recipientBlocked(ctx, tx, a, "ask", r.ID) {
			return Denied()
		}
	}
	routes, err := s.resolveProviderRoutes(ctx, tx, a, recipients, "", "ask", actionName, request.Action)
	if err != nil {
		return err
	}
	type record struct {
		Recipient    string          `json:"recipient_id"`
		Conversation string          `json:"conversation_id"`
		Exact        string          `json:"exact_instance"`
		Delivery     string          `json:"delivery_id"`
		Target       Target          `json:"policy_target"`
		Native       bool            `json:"native"`
		Routes       []providerRoute `json:"providers"`
		Request      Request         `json:"request"`
	}
	items := []record{}
	for _, r := range recipients {
		view := request
		view.Attachments = append([]Attachment(nil), request.Attachments...)
		if len(recipients) > 1 {
			view.ConversationID = ID("con")
		}
		for i := range view.Attachments {
			view.Attachments[i].ConversationID = view.ConversationID
		}
		x := record{Recipient: r.ID, Conversation: view.ConversationID, Exact: r.ExactInstance, Delivery: ID("del"), Target: r.PolicyTarget, Native: s.allowed(ctx, tx, a, "ask", r.PolicyTarget, "", actionName, request.Action), Request: view}
		x.Routes = routes[r.ID]
		if !x.Native && len(x.Routes) == 0 {
			return &Error{"NO_ELIGIBLE_ROUTE", "No permitted destination is configured for this recipient.", 403}
		}
		items = append(items, x)
	}
	batchSize := max(1, (2<<20)/(len(request.Body)+len(request.Title)+2048))
	for start := 0; start < len(items); start += batchSize {
		if _, err = tx.Exec(ctx, requestAudienceSQL, a.Scope, JSON(items[start:min(start+batchSize, len(items))]), a.ID, a.CredentialID, a.ProviderConnection, request.ExpiresAt, request.ID, request.Title, len(recipients) > 1, actionName, JSON(deliveryIdentity(a))); err != nil {
			return err
		}
	}
	for _, item := range items {
		if item.Conversation != request.ConversationID {
			if err = linkAttachments(ctx, tx, a.Scope, item.Conversation, "", request.ID, request.Attachments); err != nil {
				return err
			}
		}
	}
	return nil
}

//go:embed request_fanout.sql
var requestAudienceSQL string
