package engine

import (
	"context"
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
	// The workspace lock is held, so the reserved range is committed in the same
	// order as all other events and is rolled back together with these writes.
	var end int64
	if e := tx.QueryRow(ctx, `UPDATE scopes SET sequence=sequence+$2 WHERE id=$1 RETURNING sequence`, a.Scope, len(items)).Scan(&end); e != nil {
		return true, e
	}
	if _, e := tx.Exec(ctx, `INSERT INTO conversations(id,scope,title,created_by) SELECT x.conversation_id,$1,$2,$3 FROM jsonb_to_recordset($4::jsonb) AS x(conversation_id text)`, a.Scope, title, a.ID, raw); e != nil {
		return true, e
	}
	if _, e := tx.Exec(ctx, `INSERT INTO participants(scope,conversation_id,principal_id) SELECT $1,x.conversation_id,x.recipient_id FROM jsonb_to_recordset($3::jsonb) AS x(conversation_id text,recipient_id text) UNION SELECT $1,x.conversation_id,$2 FROM jsonb_to_recordset($3::jsonb) AS x(conversation_id text)`, a.Scope, a.ID, raw); e != nil {
		return true, e
	}
	if _, e := tx.Exec(ctx, `INSERT INTO messages(id,scope,conversation_id,sender_id,instance_id,body,created_at) SELECT x.message->>'id',$1,x.conversation_id,$2,NULLIF($3,''),x.message->>'body',(x.message->>'created_at')::timestamptz FROM jsonb_to_recordset($4::jsonb) AS x(conversation_id text,message jsonb)`, a.Scope, a.ID, a.InstanceID, raw); e != nil {
		return true, e
	}
	if _, e := tx.Exec(ctx, `INSERT INTO events(scope,sequence,kind,actor_id,conversation_id,recipient_id,data) SELECT $1,$2+x.ordinality,'message.created',$3,x.value->>'conversation_id','',x.value->'message' FROM jsonb_array_elements($4::jsonb) WITH ORDINALITY x`, a.Scope, end-int64(len(items)), a.ID, raw); e != nil {
		return true, e
	}
	if _, e := tx.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,payload,expires_at) SELECT x.delivery_id,$1,x.message->>'id',x.recipient_id,jsonb_build_object('type','message','message',x.message,'conversation_id',x.conversation_id,'_policy',jsonb_build_object('action_name','','credential_id',$4::text,'provider_connection',$5::text,'sender_id',$6::text,'target',x.policy_target,'action','message')),$3 FROM jsonb_to_recordset($2::jsonb) AS x(delivery_id text,recipient_id text,conversation_id text,message jsonb,policy_target jsonb)`, a.Scope, raw, time.Now().Add(7*24*time.Hour), a.CredentialID, a.ProviderConnection, a.ID); e != nil {
		return true, e
	}
	if _, e := tx.Exec(ctx, `SELECT pg_notify('notif_work',json_build_array($1::text,x)::text) FROM unnest($2::text[]) x`, a.Scope, ids); e != nil {
		return true, e
	}
	out.ConversationID = out.Messages[0].ConversationID
	return true, nil
}
