package engine

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"time"
)

type recipient struct {
	Principal
	ExactInstance string
	PolicyTarget  Target
}

func (s *Store) resolve(ctx context.Context, db DB, a Actor, target Target, action, actionName string) ([]recipient, error) {
	if err := target.Validate(); err != nil {
		return nil, err
	}
	out := []recipient{}
	if target.Kind == "group" {
		allowedGroup := s.allowed(ctx, db, a, action, target, "", actionName)
		rows, e := db.Query(ctx, `SELECT p.id,p.kind,p.external_id,p.name,p.metadata,p.disabled,p.created_at FROM memberships m JOIN principals p ON p.id=m.principal_id AND p.scope=m.scope WHERE m.scope=$1 AND m.group_id=$2 AND NOT p.disabled ORDER BY p.id LIMIT 1001`, a.Scope, target.ID)
		if e != nil {
			return nil, e
		}
		people := []Principal{}
		for rows.Next() {
			p, e := scanPrincipal(rows)
			if e != nil {
				rows.Close()
				return nil, e
			}
			people = append(people, p)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if len(people) > 1000 {
			return nil, Bad("Audience exceeds the 1000 recipient limit.")
		}
		for _, p := range people {
			pt := Target{p.Kind, p.ID}
			if allowedGroup || s.allowed(ctx, db, a, action, pt, "", actionName) {
				if allowedGroup {
					pt = target
				}
				out = append(out, recipient{p, "", pt})
			}
		}
	} else if target.Kind == "instance" {
		var pid string
		if e := db.QueryRow(ctx, `SELECT agent_id FROM instances WHERE scope=$1 AND id=$2`, a.Scope, target.ID).Scan(&pid); e != nil {
			return nil, Missing()
		}
		p, e := principal(ctx, db, a.Scope, pid)
		if e != nil {
			return nil, e
		}
		if p.Disabled || !s.allowed(ctx, db, a, action, Target{"agent", p.ID}, "", actionName) {
			return nil, Denied()
		}
		out = append(out, recipient{p, target.ID, Target{"agent", p.ID}})
	} else {
		p, e := scanPrincipal(db.QueryRow(ctx, `SELECT `+principalFields+` FROM principals WHERE scope=$1 AND kind=$2 AND (id=$3 OR external_id=$3) AND NOT disabled`, a.Scope, target.Kind, target.ID))
		if e != nil {
			return nil, Missing()
		}
		if !s.allowed(ctx, db, a, action, Target{p.Kind, p.ID}, "", actionName) {
			return nil, Denied()
		}
		out = append(out, recipient{p, "", Target{p.Kind, p.ID}})
	}
	if len(out) == 0 {
		return nil, &Error{"NO_ELIGIBLE_RECIPIENT", "No permitted recipient is available.", 403}
	}
	return out, nil
}
func canReadConversation(ctx context.Context, db DB, a Actor, id string) bool {
	if a.Admin {
		var exists bool
		_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conversations WHERE scope=$1 AND id=$2)`, a.Scope, id).Scan(&exists)
		return exists
	}
	var ok bool
	_ = db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM participants WHERE scope=$1 AND conversation_id=$2 AND principal_id=$3)`, a.Scope, id, a.ID).Scan(&ok)
	return ok && a.CredentialKind != "enrollment"
}
func newConversation(ctx context.Context, db DB, a Actor, title string, ids ...string) (string, error) {
	id := ID("con")
	if title == "" {
		title = "Conversation"
	}
	if _, e := db.Exec(ctx, `INSERT INTO conversations(id,scope,title,created_by) VALUES($1,$2,$3,$4)`, id, a.Scope, title, a.ID); e != nil {
		return "", e
	}
	ids = append(ids, a.ID)
	for _, pid := range ids {
		if _, e := db.Exec(ctx, `INSERT INTO participants(scope,conversation_id,principal_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, a.Scope, id, pid); e != nil {
			return "", e
		}
	}
	return id, nil
}
func (s *Store) Conversations(ctx context.Context, a Actor) ([]Conversation, error) {
	rows, e := s.Pool.Query(ctx, `SELECT c.id,c.title,c.created_by,c.created_at FROM conversations c WHERE c.scope=$1 AND ($2 OR EXISTS(SELECT 1 FROM participants p WHERE p.scope=c.scope AND p.conversation_id=c.id AND p.principal_id=$3)) ORDER BY c.created_at DESC LIMIT 200`, a.Scope, a.Admin, a.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Conversation{}
	for rows.Next() {
		var c Conversation
		if e = rows.Scan(&c.ID, &c.Title, &c.CreatedBy, &c.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	visible := []Conversation{}
	for _, c := range out {
		if s.canRead(ctx, s.Pool, a, c.ID) {
			visible = append(visible, c)
		}
	}
	return visible, nil
}
func (s *Store) Conversation(ctx context.Context, a Actor, id string) (Conversation, error) {
	var c Conversation
	if !s.canRead(ctx, s.Pool, a, id) {
		return c, Missing()
	}
	if e := s.Pool.QueryRow(ctx, `SELECT id,title,created_by,created_at FROM conversations WHERE scope=$1 AND id=$2`, a.Scope, id).Scan(&c.ID, &c.Title, &c.CreatedBy, &c.CreatedAt); e != nil {
		return c, normalize(e)
	}
	rows, e := s.Pool.Query(ctx, `SELECT p.id,p.kind,p.external_id,p.name,p.metadata,p.disabled,p.created_at FROM participants cp JOIN principals p ON p.id=cp.principal_id AND p.scope=cp.scope WHERE cp.scope=$1 AND cp.conversation_id=$2 ORDER BY p.id`, a.Scope, id)
	if e != nil {
		return c, e
	}
	c.Participants = []Principal{}
	for rows.Next() {
		p, e := scanPrincipal(rows)
		if e != nil {
			rows.Close()
			return c, e
		}
		c.Participants = append(c.Participants, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return c, e
	}
	rows, e = s.Pool.Query(ctx, `SELECT id,conversation_id,sender_id,COALESCE(instance_id,''),body,created_at FROM (SELECT * FROM messages WHERE scope=$1 AND conversation_id=$2 ORDER BY created_at DESC,id DESC LIMIT 500) m ORDER BY created_at,id`, a.Scope, id)
	if e != nil {
		return c, e
	}
	defer rows.Close()
	c.Messages = []Message{}
	for rows.Next() {
		var m Message
		if e = rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.InstanceID, &m.Body, &m.CreatedAt); e != nil {
			return c, e
		}
		c.Messages = append(c.Messages, m)
	}
	return c, rows.Err()
}
func (s *Store) conversationRecipients(ctx context.Context, db DB, a Actor, id, action string) ([]recipient, error) {
	if !s.canRead(ctx, db, a, id) {
		return nil, Missing()
	}
	rows, e := db.Query(ctx, `SELECT p.id,p.kind,p.external_id,p.name,p.metadata,p.disabled,p.created_at FROM participants cp JOIN principals p ON p.id=cp.principal_id AND p.scope=cp.scope WHERE cp.scope=$1 AND cp.conversation_id=$2 AND p.id<>$3 AND NOT p.disabled`, a.Scope, id, a.ID)
	if e != nil {
		return nil, e
	}
	all := []Principal{}
	for rows.Next() {
		p, e := scanPrincipal(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		all = append(all, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []recipient{}
	for _, p := range all {
		if !s.allowed(ctx, db, a, action, Target{p.Kind, p.ID}, "", "") {
			return nil, Denied()
		}
		out = append(out, recipient{p, "", Target{p.Kind, p.ID}})
	}
	if len(out) == 0 {
		return nil, Bad("Conversation has no active recipient.")
	}
	return out, nil
}
func insertMessage(ctx context.Context, db DB, a Actor, conv, body string) (Message, error) {
	m := Message{ID: ID("msg"), ConversationID: conv, SenderID: a.ID, InstanceID: a.InstanceID, Body: body, CreatedAt: time.Now()}
	_, e := db.Exec(ctx, `INSERT INTO messages(id,scope,conversation_id,sender_id,instance_id,body,created_at) VALUES($1,$2,$3,$4,NULLIF($5,''),$6,$7)`, m.ID, a.Scope, conv, a.ID, a.InstanceID, body, m.CreatedAt)
	if e != nil {
		return m, e
	}
	return m, event(ctx, db, a, "message.created", conv, "", m)
}
func (s *Store) enqueue(ctx context.Context, db DB, a Actor, r recipient, op, conv, action string, payload map[string]any, expiry time.Time) error {
	payload["_policy"] = map[string]any{"credential_id": a.CredentialID, "provider_connection": a.ProviderConnection, "sender_id": a.ID, "target": r.PolicyTarget, "action": action}
	payload["conversation_id"] = conv
	if _, e := db.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,exact_instance,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT DO NOTHING`, ID("del"), a.Scope, op, r.ID, r.ExactInstance, JSON(payload), expiry); e != nil {
		return e
	}
	// Destination dispatch is additional to the durable native inbox. One provider does not replace canonical history.
	rows, e := db.Query(ctx, `SELECT d.connection_id,d.address FROM destinations d JOIN connections c ON c.id=d.connection_id AND c.scope=d.scope WHERE d.scope=$1 AND d.principal_id=$2 AND c.enabled`, a.Scope, r.ID)
	if e != nil {
		return e
	}
	type dest struct{ id, address string }
	dests := []dest{}
	for rows.Next() {
		var d dest
		if e = rows.Scan(&d.id, &d.address); e != nil {
			rows.Close()
			return e
		}
		dests = append(dests, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, d := range dests {
		if !s.allowed(ctx, db, a, action, r.PolicyTarget, d.id, "") {
			continue
		}
		if _, e = db.Exec(ctx, `INSERT INTO deliveries(id,scope,operation_id,recipient_id,connection_id,address,payload,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT DO NOTHING`, ID("del"), a.Scope, op, r.ID, d.id, d.address, JSON(payload), expiry); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) Send(ctx context.Context, a Actor, in SendInput, key string) (SendResult, error) {
	var out SendResult
	if !ValidText(in.Body, 32768) || len(in.Title) > 200 {
		return out, Bad("Message body is required and must be at most 32768 bytes.")
	}
	err := s.tx(ctx, a, func(tx pgx.Tx) error {
		return idempotent(ctx, tx, a, "send", key, in, &out, func() error {
			var rs []recipient
			var e error
			if in.ConversationID != "" {
				rs, e = s.conversationRecipients(ctx, tx, a, in.ConversationID, "message")
			} else {
				rs, e = s.resolve(ctx, tx, a, in.Target, "message", "")
			}
			if e != nil {
				return e
			}
			out.Messages = []Message{}
			if in.ConversationID != "" {
				m, e := insertMessage(ctx, tx, a, in.ConversationID, in.Body)
				if e != nil {
					return e
				}
				out.Messages = append(out.Messages, m)
				out.ConversationID = in.ConversationID
				for _, r := range rs {
					if e = s.enqueue(ctx, tx, a, r, m.ID, m.ConversationID, "message", map[string]any{"type": "message", "message": m}, time.Now().Add(7*24*time.Hour)); e != nil {
						return e
					}
				}
				return nil
			}
			for _, r := range rs {
				conv, e := newConversation(ctx, tx, a, in.Title, r.ID)
				if e != nil {
					return e
				}
				m, e := insertMessage(ctx, tx, a, conv, in.Body)
				if e != nil {
					return e
				}
				if out.ConversationID == "" {
					out.ConversationID = conv
				}
				out.Messages = append(out.Messages, m)
				if e = s.enqueue(ctx, tx, a, r, m.ID, conv, "message", map[string]any{"type": "message", "message": m}, time.Now().Add(7*24*time.Hour)); e != nil {
					return e
				}
			}
			return nil
		})
	})
	return out, err
}
func (s *Store) Events(ctx context.Context, a Actor, after int64) ([]Event, error) {
	rows, e := s.Pool.Query(ctx, `SELECT e.sequence,e.kind,e.actor_id,e.conversation_id,e.recipient_id,e.data,e.created_at FROM events e WHERE e.scope=$1 AND e.sequence>$2 AND ($3 OR (e.conversation_id<>'' AND EXISTS(SELECT 1 FROM participants p WHERE p.scope=e.scope AND p.conversation_id=e.conversation_id AND p.principal_id=$4)) OR e.recipient_id=$4) ORDER BY e.sequence LIMIT 200`, a.Scope, after, a.Admin, a.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var x Event
		var raw []byte
		if e = rows.Scan(&x.Sequence, &x.Kind, &x.ActorID, &x.ConversationID, &x.RecipientID, &raw, &x.CreatedAt); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(raw, &x.Data)
		out = append(out, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	visible := []Event{}
	for _, x := range out {
		if x.ConversationID == "" || s.canRead(ctx, s.Pool, a, x.ConversationID) {
			visible = append(visible, x)
		}
	}
	return visible, nil
}
func (s *Store) Preview(ctx context.Context, a Actor, target Target, action string) (map[string]any, error) {
	rs, e := s.resolve(ctx, s.Pool, a, target, action, "")
	if e != nil {
		return nil, e
	}
	list := []Principal{}
	for _, r := range rs {
		list = append(list, r.Principal)
	}
	return map[string]any{"recipients": list, "count": len(list), "mode": "private_per_recipient", "sends": false}, nil
}
