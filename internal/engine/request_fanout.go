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
	type route struct {
		Connection string `json:"connection_id"`
		Address    string `json:"address"`
		ID         string `json:"delivery_id"`
	}
	ids := []string{}
	for _, r := range recipients {
		ids = append(ids, r.ID)
	}
	rows, err := tx.Query(ctx, `SELECT d.principal_id,d.connection_id,d.address FROM destinations d JOIN connections c ON c.scope=d.scope AND c.id=d.connection_id WHERE d.scope=$1 AND d.principal_id=ANY($2::text[]) AND c.enabled AND d.mode IN ('primary','copy') ORDER BY d.priority,d.id`, a.Scope, ids)
	if err != nil {
		return err
	}
	routes := map[string][]route{}
	for rows.Next() {
		var id string
		var r route
		if err = rows.Scan(&id, &r.Connection, &r.Address); err != nil {
			rows.Close()
			return err
		}
		routes[id] = append(routes[id], r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	type record struct {
		Recipient    string  `json:"recipient_id"`
		Conversation string  `json:"conversation_id"`
		Exact        string  `json:"exact_instance"`
		Delivery     string  `json:"delivery_id"`
		Target       Target  `json:"policy_target"`
		Native       bool    `json:"native"`
		Routes       []route `json:"providers"`
		Request      Request `json:"request"`
	}
	items := []record{}
	actionName := ""
	if request.Action != nil {
		actionName = request.Action.Name
	}
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
		for _, d := range routes[r.ID] {
			if s.allowed(ctx, tx, a, "ask", r.PolicyTarget, d.Connection, actionName, request.Action) {
				d.ID = ID("del")
				x.Routes = append(x.Routes, d)
			}
		}
		if !x.Native && len(x.Routes) == 0 {
			return &Error{"NO_ELIGIBLE_ROUTE", "No permitted destination is configured for this recipient.", 403}
		}
		items = append(items, x)
	}
	batchSize := max(1, (2<<20)/(len(request.Body)+len(request.Title)+2048))
	for start := 0; start < len(items); start += batchSize {
		if _, err = tx.Exec(ctx, requestAudienceSQL, a.Scope, JSON(items[start:min(start+batchSize, len(items))]), a.ID, a.CredentialID, a.ProviderConnection, request.ExpiresAt, request.ID, request.Title, len(recipients) > 1, actionName); err != nil {
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
