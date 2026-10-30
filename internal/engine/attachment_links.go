package engine

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type attachmentBindingKey struct{}
type attachmentQueryConversationKey struct{}

func withAttachmentBinding(ctx context.Context, operation, key string) context.Context {
	if ctx.Value(attachmentBindingKey{}) != nil {
		return ctx
	}
	return context.WithValue(ctx, attachmentBindingKey{}, Digest(operation+"/"+key))
}

const attachmentListSQL = `SELECT a.id,COALESCE(l.conversation_id,a.conversation_id,''),a.file_name,a.media_type,a.size,a.sha256,a.created_at,COALESCE(l.message_id,''),COALESCE(l.request_id,'') FROM attachments a LEFT JOIN attachment_links l ON l.scope=a.scope AND l.attachment_id=a.id`

const attachmentHistorySQL = `chat_history_allows(a.scope,COALESCE(l.conversation_id,a.conversation_id),$3,$4,COALESCE((SELECT created_sequence FROM messages WHERE scope=a.scope AND id=l.message_id),(SELECT created_sequence FROM requests WHERE scope=a.scope AND id=l.request_id),a.created_sequence))`

func scanAttachmentRow(row pgx.Row, x *Attachment) error {
	return row.Scan(&x.ID, &x.ConversationID, &x.FileName, &x.MediaType, &x.Size, &x.SHA256, &x.CreatedAt, &x.MessageID, &x.RequestID)
}

// Claim under the enclosing communication transaction. A batch may place the
// same draft in each private copy, but another operation cannot reuse it.
func (s *Store) claimAttachments(ctx context.Context, db DB, a Actor, ids []string) ([]Attachment, error) {
	out := []Attachment{}
	if len(ids) == 0 {
		return out, nil
	}
	if len(ids) > 5 {
		return nil, Bad("Attach up to five files per message.")
	}
	binding, _ := ctx.Value(attachmentBindingKey{}).(string)
	if binding == "" {
		return nil, Bad("Files require a communication operation.")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, Bad("A file can appear only once in a message.")
		}
		seen[id] = true
		var x Attachment
		err := db.QueryRow(ctx, `UPDATE attachments SET bound_operation=$4,draft_until=NULL WHERE scope=$1 AND id=$2 AND owner_id=$3 AND conversation_id IS NULL AND ((bound_operation='' AND draft_until>now()) OR bound_operation=$4) RETURNING id,file_name,media_type,size,sha256,created_at`, a.Scope, id, a.ID, binding).Scan(&x.ID, &x.FileName, &x.MediaType, &x.Size, &x.SHA256, &x.CreatedAt)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, Bad("A file is unavailable, expired or already sent. Attach it again.")
			}
			return nil, err
		}
		if _, err = db.Exec(ctx, `DELETE FROM attachment_uploads WHERE scope=$1 AND id=$2`, a.Scope, id); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}
func linkAttachments(ctx context.Context, db DB, scope, conv, message, request string, files []Attachment) error {
	for _, file := range files {
		if _, err := db.Exec(ctx, `INSERT INTO attachment_links(scope,attachment_id,conversation_id,message_id,request_id) VALUES($1,$2,$3,NULLIF($4,''),NULLIF($5,'')) ON CONFLICT DO NOTHING`, scope, file.ID, conv, message, request); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) canReadAttachment(ctx context.Context, db DB, a Actor, x Attachment) bool {
	var own bool
	if db.QueryRow(ctx, `SELECT owner_id=$3 AND bound_operation='' AND draft_until>now() FROM attachments WHERE scope=$1 AND id=$2`, a.Scope, x.ID, a.ID).Scan(&own) == nil && own {
		return true
	}
	rows, err := db.Query(ctx, `SELECT COALESCE(l.conversation_id,a.conversation_id) FROM attachments a LEFT JOIN attachment_links l ON l.scope=a.scope AND l.attachment_id=a.id WHERE a.scope=$1 AND a.id=$2 AND COALESCE(l.conversation_id,a.conversation_id) IS NOT NULL AND `+attachmentHistorySQL, a.Scope, x.ID, a.ID, a.Admin)
	if err != nil {
		return false
	}
	convs := []string{}
	for rows.Next() {
		var conv string
		if rows.Scan(&conv) != nil {
			rows.Close()
			return false
		}
		convs = append(convs, conv)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false
	}
	for _, conv := range convs {
		if s.canRead(ctx, db, a, conv) {
			return true
		}
	}
	return false
}
func messageFiles(files []Attachment, conv, id string) []Attachment {
	for i := range files {
		files[i].ConversationID = conv
		files[i].MessageID = id
	}
	return files
}
func (s *Store) loadMessageFiles(ctx context.Context, db DB, scope string, messages []Message) error {
	ids := []string{}
	index := map[string]int{}
	for i, m := range messages {
		ids = append(ids, m.ID)
		index[m.ID] = i
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := db.Query(ctx, attachmentListSQL+` WHERE a.scope=$1 AND l.message_id=ANY($2::text[]) ORDER BY a.created_at,a.id`, scope, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x Attachment
		if err = scanAttachmentRow(rows, &x); err != nil {
			return err
		}
		i := index[x.MessageID]
		messages[i].Attachments = append(messages[i].Attachments, x)
	}
	return rows.Err()
}
func (s *Store) loadRequestFiles(ctx context.Context, db DB, scope string, requests []Request) error {
	ids := []string{}
	index := map[string]int{}
	for i, r := range requests {
		if !r.hasFiles {
			continue
		}
		ids = append(ids, r.ID)
		index[r.ID] = i
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := db.Query(ctx, attachmentListSQL+` WHERE a.scope=$1 AND l.request_id=ANY($2::text[]) ORDER BY a.created_at,a.id`, scope, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x Attachment
		if err = scanAttachmentRow(rows, &x); err != nil {
			return err
		}
		i := index[x.RequestID]
		if x.ConversationID == requests[i].ConversationID {
			requests[i].Attachments = append(requests[i].Attachments, x)
		}
	}
	return rows.Err()
}
